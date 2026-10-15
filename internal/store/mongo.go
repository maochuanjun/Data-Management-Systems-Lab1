// Package store owns MongoDB persistence, schema validation, and aggregation.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"questionnaire/internal/domain"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var (
	ErrNotFound  = errors.New("record not found")
	ErrConflict  = errors.New("record changed or operation not allowed")
	ErrDuplicate = errors.New("record already exists")
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

func New(ctx context.Context, uri, database string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("connect MongoDB: %w", err)
	}
	s := &Store{client: client, db: client.Database(database)}
	if err = client.Ping(ctx, readpref.Primary()); err == nil {
		err = s.Init(ctx)
	}
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(closeCtx)
		return nil, fmt.Errorf("initialize MongoDB: %w", err)
	}
	return s, nil
}

func (s *Store) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

// Init applies validators to both new and existing collections. Schema versions
// make future migrations explicit rather than silently changing old documents.
func (s *Store) Init(ctx context.Context) error {
	for name, schema := range collectionSchemas() {
		validator := bson.M{"$jsonSchema": schema}
		err := s.db.CreateCollection(ctx, name, options.CreateCollection().SetValidator(validator))
		if err != nil {
			var commandError mongo.CommandError
			if !errors.As(err, &commandError) || commandError.Code != 48 {
				return fmt.Errorf("create %s: %w", name, err)
			}
			if err := s.db.RunCommand(ctx, bson.D{{Key: "collMod", Value: name}, {Key: "validator", Value: validator}, {Key: "validationLevel", Value: "strict"}, {Key: "validationAction", Value: "error"}}).Err(); err != nil {
				return fmt.Errorf("validate %s: %w", name, err)
			}
		}
	}
	indexes := map[string][]mongo.IndexModel{
		"users":    {{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)}},
		"surveys":  {{Keys: bson.D{{Key: "ownerId", Value: 1}, {Key: "createdAt", Value: -1}}}},
		"sessions": {{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}},
		"responses": {
			{Keys: bson.D{{Key: "surveyId", Value: 1}, {Key: "dedupeKey", Value: 1}}, Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"dedupeKey": bson.M{"$exists": true}})},
			{Keys: bson.D{{Key: "surveyId", Value: 1}, {Key: "submittedAt", Value: 1}}},
		},
	}
	for name, models := range indexes {
		if _, err := s.db.Collection(name).Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("index %s: %w", name, err)
		}
	}
	return nil
}

func collectionSchemas() map[string]bson.M {
	str := bson.M{"bsonType": "string"}
	date := bson.M{"bsonType": "date"}
	boolean := bson.M{"bsonType": "bool"}
	version := bson.M{"bsonType": bson.A{"int", "long"}, "minimum": 1}
	option := bson.M{"bsonType": "object", "required": bson.A{"id", "label"}, "properties": bson.M{"id": str, "label": str}}
	rule := bson.M{"bsonType": "object", "required": bson.A{"operator", "value", "target"}, "properties": bson.M{"operator": str, "target": str}}
	question := bson.M{"bsonType": "object", "required": bson.A{"id", "title", "type", "required", "validation"}, "properties": bson.M{
		"id": bson.M{"bsonType": "string", "pattern": "^[a-zA-Z0-9_-]{1,64}$"}, "title": str,
		"type": bson.M{"enum": bson.A{"single", "multiple", "text", "number"}}, "required": boolean,
		"options": bson.M{"bsonType": bson.A{"array", "null"}, "items": option},
		"rules":   bson.M{"bsonType": bson.A{"array", "null"}, "items": rule}, "validation": bson.M{"bsonType": "object"},
	}}
	return map[string]bson.M{
		"users": {"bsonType": "object", "required": bson.A{"_id", "username", "passwordHash", "createdAt"}, "properties": bson.M{
			"_id": str, "username": str, "passwordHash": str, "createdAt": date,
		}},
		"sessions": {"bsonType": "object", "required": bson.A{"_id", "userId", "expiresAt"}, "properties": bson.M{
			"_id": str, "userId": str, "expiresAt": date,
		}},
		"surveys": {"bsonType": "object", "required": bson.A{"_id", "ownerId", "schemaVersion", "title", "status", "questions", "revision", "createdAt", "allowAnonymous", "allowRepeat"}, "properties": bson.M{
			"_id": str, "ownerId": str, "schemaVersion": version, "title": str, "description": str, "createdAt": date,
			"status": bson.M{"enum": bson.A{"draft", "published", "closed"}}, "allowAnonymous": boolean, "allowRepeat": boolean,
			"deadline": bson.M{"bsonType": bson.A{"date", "null"}}, "revision": bson.M{"bsonType": bson.A{"int", "long"}, "minimum": 0},
			"questions": bson.M{"bsonType": bson.A{"array", "null"}, "items": question},
		}},
		"responses": {"bsonType": "object", "required": bson.A{"_id", "surveyId", "schemaVersion", "anonymous", "answers", "path", "submittedAt"}, "properties": bson.M{
			"_id": str, "surveyId": str, "schemaVersion": version, "anonymous": boolean, "userId": str, "dedupeKey": str,
			"answers": bson.M{"bsonType": "object"}, "path": bson.M{"bsonType": "array", "items": str}, "submittedAt": date,
		}},
	}
}

func translate(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicate
	}
	return err
}

func (s *Store) CreateUser(ctx context.Context, user *domain.User) error {
	_, err := s.db.Collection("users").InsertOne(ctx, user)
	return translate(err)
}
func (s *Store) FindUserByName(ctx context.Context, name string) (*domain.User, error) {
	return s.findUser(ctx, bson.M{"username": name})
}
func (s *Store) FindUser(ctx context.Context, id string) (*domain.User, error) {
	return s.findUser(ctx, bson.M{"_id": id})
}
func (s *Store) findUser(ctx context.Context, filter bson.M) (*domain.User, error) {
	var user domain.User
	if err := s.db.Collection("users").FindOne(ctx, filter).Decode(&user); err != nil {
		return nil, translate(err)
	}
	return &user, nil
}
func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, expires time.Time) error {
	_, err := s.db.Collection("sessions").InsertOne(ctx, bson.M{"_id": tokenHash, "userId": userID, "expiresAt": expires})
	return translate(err)
}
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (*domain.User, error) {
	var session struct {
		UserID string `bson:"userId"`
	}
	// TTL cleanup is asynchronous; the query enforces immediate expiration.
	err := s.db.Collection("sessions").FindOne(ctx, bson.M{"_id": tokenHash, "expiresAt": bson.M{"$gt": time.Now()}}).Decode(&session)
	if err != nil {
		return nil, translate(err)
	}
	return s.FindUser(ctx, session.UserID)
}
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.Collection("sessions").DeleteOne(ctx, bson.M{"_id": tokenHash})
	return err
}
func (s *Store) ListSurveys(ctx context.Context, owner string) ([]domain.Survey, error) {
	cursor, err := s.db.Collection("surveys").Find(ctx, bson.M{"ownerId": owner}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	surveys := make([]domain.Survey, 0)
	if err := cursor.All(ctx, &surveys); err != nil {
		return nil, err
	}
	return surveys, nil
}
func (s *Store) Survey(ctx context.Context, id string) (*domain.Survey, error) {
	var survey domain.Survey
	if err := s.db.Collection("surveys").FindOne(ctx, bson.M{"_id": id}).Decode(&survey); err != nil {
		return nil, translate(err)
	}
	return &survey, nil
}
func (s *Store) CreateSurvey(ctx context.Context, survey *domain.Survey) error {
	_, err := s.db.Collection("surveys").InsertOne(ctx, survey)
	return translate(err)
}
func (s *Store) UpdateDraft(ctx context.Context, survey *domain.Survey, expectedRevision int) error {
	result, err := s.db.Collection("surveys").UpdateOne(ctx,
		bson.M{"_id": survey.ID, "ownerId": survey.OwnerID, "status": "draft", "revision": expectedRevision},
		bson.M{"$set": bson.M{"title": survey.Title, "description": survey.Description, "questions": survey.Questions, "allowAnonymous": survey.AllowAnonymous, "allowRepeat": survey.AllowRepeat, "deadline": survey.Deadline, "schemaVersion": survey.SchemaVersion}, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return translate(err)
	}
	if result.MatchedCount == 0 {
		return ErrConflict
	}
	survey.Revision = expectedRevision + 1
	return nil
}
func (s *Store) Transition(ctx context.Context, id, owner, from, to string, revision int) error {
	result, err := s.db.Collection("surveys").UpdateOne(ctx,
		bson.M{"_id": id, "ownerId": owner, "status": from, "revision": revision},
		bson.M{"$set": bson.M{"status": to}, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return translate(err)
	}
	if result.MatchedCount == 0 {
		return ErrConflict
	}
	return nil
}
func (s *Store) InsertResponse(ctx context.Context, response *domain.Response, dedupeKey string) error {
	data, err := bson.Marshal(response)
	if err != nil {
		return err
	}
	var document bson.M
	if err := bson.Unmarshal(data, &document); err != nil {
		return err
	}
	if dedupeKey != "" {
		document["dedupeKey"] = dedupeKey
	}
	_, err = s.db.Collection("responses").InsertOne(ctx, document)
	return translate(err)
}

// Stats uses MongoDB aggregation for answered counts, values, and numeric means.
// Only answer fields are projected, so participant identities cannot leak here.
func (s *Store) Stats(ctx context.Context, survey *domain.Survey) (map[string]any, error) {
	responses := s.db.Collection("responses")
	total, err := responses.CountDocuments(ctx, bson.M{"surveyId": survey.ID})
	if err != nil {
		return nil, err
	}
	questions := make([]map[string]any, 0, len(survey.Questions))
	for _, q := range survey.Questions {
		field := "answers." + q.ID
		group := bson.M{"_id": nil, "answered": bson.M{"$sum": 1}, "values": bson.M{"$push": "$" + field}}
		if q.Type == "number" {
			group["average"] = bson.M{"$avg": "$" + field}
		}
		pipeline := mongo.Pipeline{
			{{Key: "$match", Value: bson.M{"surveyId": survey.ID, field: bson.M{"$exists": true, "$ne": nil}}}},
			{{Key: "$group", Value: group}},
		}
		cursor, err := responses.Aggregate(ctx, pipeline)
		if err != nil {
			return nil, err
		}
		var rows []struct {
			Answered int64    `bson:"answered"`
			Values   []any    `bson:"values"`
			Average  *float64 `bson:"average"`
		}
		err = cursor.All(ctx, &rows)
		_ = cursor.Close(ctx)
		if err != nil {
			return nil, err
		}
		values := make([]any, 0)
		answered := int64(0)
		var average any
		if len(rows) > 0 {
			answered, values = rows[0].Answered, rows[0].Values
			if rows[0].Average != nil {
				average = *rows[0].Average
			}
		}
		counts := make(map[string]int64)
		for _, value := range values {
			switch v := value.(type) {
			case string:
				counts[v]++
			case bson.A:
				for _, item := range v {
					if id, ok := item.(string); ok {
						counts[id]++
					}
				}
			case []any:
				for _, item := range v {
					if id, ok := item.(string); ok {
						counts[id]++
					}
				}
			}
		}
		optionStats := make([]map[string]any, 0, len(q.Options))
		for _, option := range q.Options {
			optionStats = append(optionStats, map[string]any{"id": option.ID, "label": option.Label, "count": counts[option.ID]})
		}
		questions = append(questions, map[string]any{"id": q.ID, "title": q.Title, "type": q.Type, "answered": answered, "options": optionStats, "values": values, "average": average})
	}
	return map[string]any{"totalResponses": total, "questions": questions}, nil
}
