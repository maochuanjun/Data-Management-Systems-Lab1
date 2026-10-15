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
			{Keys: bson.D{{Key: "surveyId", Value: 1}, {Key: "submittedAt", Value: 1}, {Key: "_id", Value: 1}}},
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

// Stats aggregates bounded summaries from one matching response stream. Raw
// answers are fetched separately in stable, bounded pages; concurrent submissions
// may appear between the summary and page reads. Participant identities are never
// returned.
func (s *Store) Stats(ctx context.Context, survey *domain.Survey, offset, limit int) (map[string]any, error) {
	if offset < 0 || limit < 1 || limit > 50 {
		return nil, errors.New("statistics offset must be nonnegative and limit between 1 and 50")
	}
	facets := bson.M{"total": mongo.Pipeline{{{Key: "$count", Value: "count"}}}}
	for i, q := range survey.Questions {
		field := "answers." + q.ID
		group := bson.M{"_id": nil, "answered": bson.M{"$sum": 1}}
		if q.Type == "number" {
			group["average"] = bson.M{"$avg": "$" + field}
		}
		optionCounts := make(bson.A, 0, len(q.Options))
		for j, option := range q.Options {
			key := fmt.Sprintf("o%d", j)
			condition := bson.M{"$eq": bson.A{"$" + field, bson.M{"$literal": option.ID}}}
			if q.Type == "multiple" {
				condition = bson.M{"$in": bson.A{bson.M{"$literal": option.ID}, "$" + field}}
			}
			group[key] = bson.M{"$sum": bson.M{"$cond": bson.A{condition, 1, 0}}}
			optionCounts = append(optionCounts, "$"+key)
		}
		facets[fmt.Sprintf("q%d", i)] = mongo.Pipeline{
			{{Key: "$match", Value: bson.M{field: bson.M{"$exists": true, "$ne": nil}}}},
			{{Key: "$group", Value: group}},
			{{Key: "$project", Value: bson.M{"answered": 1, "average": 1, "optionCounts": optionCounts}}},
		}
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"surveyId": survey.ID}}},
		{{Key: "$facet", Value: facets}},
	}
	cursor, err := s.db.Collection("responses").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	type aggregateRow struct {
		Count        int64    `bson:"count"`
		Answered     int64    `bson:"answered"`
		OptionCounts []int64  `bson:"optionCounts"`
		Average      *float64 `bson:"average"`
	}
	var results []map[string][]aggregateRow
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	aggregates := make(map[string][]aggregateRow)
	if len(results) > 0 {
		aggregates = results[0]
	}
	total := int64(0)
	if rows := aggregates["total"]; len(rows) > 0 {
		total = rows[0].Count
	}
	questions := make([]map[string]any, 0, len(survey.Questions))
	for i, q := range survey.Questions {
		rows := aggregates[fmt.Sprintf("q%d", i)]
		var row aggregateRow
		var average any
		if len(rows) > 0 {
			row = rows[0]
			if row.Average != nil {
				average = *row.Average
			}
		}
		values, hasMore, err := s.answerValues(ctx, survey.ID, q.ID, offset, limit)
		if err != nil {
			return nil, err
		}
		optionStats := make([]map[string]any, 0, len(q.Options))
		for j, option := range q.Options {
			count := int64(0)
			if j < len(row.OptionCounts) {
				count = row.OptionCounts[j]
			}
			optionStats = append(optionStats, map[string]any{"id": option.ID, "label": option.Label, "count": count})
		}
		questions = append(questions, map[string]any{
			"id": q.ID, "title": q.Title, "type": q.Type, "answered": row.Answered,
			"options": optionStats, "values": values, "average": average,
			"valuesOffset": offset, "valuesLimit": limit, "hasMore": hasMore,
		})
	}
	return map[string]any{"totalResponses": total, "questions": questions}, nil
}

func (s *Store) answerValues(ctx context.Context, surveyID, questionID string, offset, limit int) ([]any, bool, error) {
	field := "answers." + questionID
	cursor, err := s.db.Collection("responses").Find(ctx,
		bson.M{"surveyId": surveyID, field: bson.M{"$exists": true, "$ne": nil}},
		options.Find().
			SetSort(bson.D{{Key: "submittedAt", Value: 1}, {Key: "_id", Value: 1}}).
			SetProjection(bson.M{"_id": 0, field: 1}).
			SetSkip(int64(offset)).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, false, err
	}
	defer cursor.Close(ctx)
	values := make([]any, 0, limit)
	for cursor.Next(ctx) {
		if len(values) == limit {
			return values, true, nil
		}
		var response struct {
			Answers map[string]any `bson:"answers"`
		}
		if err := cursor.Decode(&response); err != nil {
			return nil, false, err
		}
		values = append(values, response.Answers[questionID])
	}
	return values, false, cursor.Err()
}
