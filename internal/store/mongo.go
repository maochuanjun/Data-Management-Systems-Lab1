// Package store owns MongoDB persistence, schema validation, and aggregation.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// The unique index must exist before any legacy keys are added. It also
	// arbitrates concurrent initializers and preserves already-keyed responses.
	return s.backfillLegacyDedupeKeys(ctx)
}

// backfillLegacyDedupeKeys restores the no-repeat policy for historical answers
// that retained their respondent ID but predate dedupeKey. It adds only the
// auxiliary key: answers, paths, identities, and all duplicate records survive.
// An anonymous historical answer without a userId cannot be linked back to an
// account safely, so it is retained without inventing a deduplication identity.
func (s *Store) backfillLegacyDedupeKeys(ctx context.Context) error {
	cursor, err := s.db.Collection("surveys").Find(ctx, bson.M{"allowRepeat": false},
		options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return fmt.Errorf("find surveys for legacy deduplication: %w", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var survey struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&survey); err != nil {
			return fmt.Errorf("decode legacy deduplication survey: %w", err)
		}
		if err := s.backfillSurveyDedupeKeys(ctx, survey.ID); err != nil {
			return fmt.Errorf("backfill legacy deduplication for survey %s: %w", survey.ID, err)
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("read surveys for legacy deduplication: %w", err)
	}
	return nil
}

func (s *Store) backfillSurveyDedupeKeys(ctx context.Context, surveyID string) error {
	responses := s.db.Collection("responses")
	cursor, err := responses.Find(ctx, bson.M{
		"surveyId": surveyID, "dedupeKey": bson.M{"$exists": false},
		"userId": bson.M{"$type": "string", "$ne": ""},
	}, options.Find().
		SetProjection(bson.M{"_id": 1, "userId": 1}).
		SetSort(bson.D{{Key: "submittedAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var response struct {
			ID     string `bson:"_id"`
			UserID string `bson:"userId"`
		}
		if err := cursor.Decode(&response); err != nil {
			return fmt.Errorf("decode legacy response: %w", err)
		}
		hash := sha256.Sum256([]byte(surveyID + ":" + response.UserID))
		key := hex.EncodeToString(hash[:])
		// The first historical record in submittedAt/_id order holds the key,
		// unless an existing keyed record already represents this account. A
		// competing Init may have set this record's key after our cursor read it;
		// the conditional update never overwrites that successful migration.
		_, err := responses.UpdateOne(ctx, bson.M{
			"_id": response.ID, "surveyId": surveyID, "userId": response.UserID,
			"dedupeKey": bson.M{"$exists": false},
		}, bson.M{"$set": bson.M{"dedupeKey": key}})
		if err != nil {
			if mongo.IsDuplicateKeyError(err) {
				// Existing historical duplicates remain intact. Only suppress the
				// conflict if the intended account guard really exists; unrelated
				// uniqueness errors or failed verification must abort startup.
				existingErr := responses.FindOne(ctx, bson.M{"surveyId": surveyID, "dedupeKey": key},
					options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
				if existingErr == nil {
					continue
				}
				if !errors.Is(existingErr, mongo.ErrNoDocuments) {
					return fmt.Errorf("verify legacy guard after response %s conflict: %w", response.ID, existingErr)
				}
			}
			return fmt.Errorf("set legacy response %s dedupeKey: %w", response.ID, err)
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("read legacy responses: %w", err)
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
		// The editor saves fill + fillType; the API also accepts legacy text/number types.
		"type": bson.M{"enum": bson.A{"single", "multiple", "fill", "text", "number"}}, "fillType": bson.M{"enum": bson.A{"text", "number"}}, "required": boolean,
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
			"status": bson.M{"enum": bson.A{"draft", "published", "closed"}}, "allowAnonymous": boolean, "allowRepeat": boolean, "archived": boolean,
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
		bson.M{"_id": survey.ID, "ownerId": survey.OwnerID, "status": "draft", "archived": bson.M{"$ne": true}, "revision": expectedRevision},
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
		bson.M{"_id": id, "ownerId": owner, "status": from, "archived": bson.M{"$ne": true}, "revision": revision},
		bson.M{"$set": bson.M{"status": to}, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return translate(err)
	}
	if result.MatchedCount == 0 {
		return ErrConflict
	}
	return nil
}

// Archive changes dashboard visibility without deleting historical responses.
// Archiving a published survey also closes it in the same atomic update.
func (s *Store) SetArchiveState(ctx context.Context, survey *domain.Survey, archived bool) error {
	filter := bson.M{"_id": survey.ID, "ownerId": survey.OwnerID, "revision": survey.Revision, "status": survey.Status}
	update := bson.M{"archived": archived}
	if archived {
		filter["archived"] = bson.M{"$ne": true} // old v1 surveys omit this field
		if survey.Status == "published" {
			update["status"] = "closed"
		}
	} else {
		filter["archived"] = true
	}
	result, err := s.db.Collection("surveys").UpdateOne(ctx, filter, bson.M{"$set": update, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return translate(err)
	}
	if result.MatchedCount == 0 {
		return ErrConflict
	}
	return nil
}

// DeleteDraft permanently deletes only drafts; published content and their
// responses can be archived but must not be erased by this endpoint.
func (s *Store) DeleteDraft(ctx context.Context, survey *domain.Survey) error {
	count, err := s.db.Collection("responses").CountDocuments(ctx, bson.M{"surveyId": survey.ID})
	if err != nil {
		return translate(err)
	}
	if count != 0 {
		return ErrConflict
	}
	result, err := s.db.Collection("surveys").DeleteOne(ctx, bson.M{
		"_id": survey.ID, "ownerId": survey.OwnerID,
		"status": "draft", "revision": survey.Revision,
	})
	if err != nil {
		return translate(err)
	}
	if result.DeletedCount == 0 {
		return ErrConflict
	}
	return nil
}

// Reopen preserves all existing questions, options and response documents.
// A closed survey can be reopened with the original or a new valid deadline.
func (s *Store) Reopen(ctx context.Context, survey *domain.Survey, deadline *time.Time) error {
	result, err := s.db.Collection("surveys").UpdateOne(ctx,
		bson.M{"_id": survey.ID, "ownerId": survey.OwnerID, "status": "closed",
			"archived": bson.M{"$ne": true}, "revision": survey.Revision},
		bson.M{"$set": bson.M{"status": "published", "deadline": deadline}, "$inc": bson.M{"revision": 1}})
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
	if offset < 0 || limit < 0 || limit > 50 {
		return nil, errors.New("statistics offset must be nonnegative and limit between 0 and 50")
	}
	facets := bson.M{"total": mongo.Pipeline{{{Key: "$count", Value: "count"}}}}
	for i, q := range survey.Questions {
		field := "answers." + q.ID
		group := bson.M{"_id": nil, "answered": bson.M{"$sum": 1}}
		if q.Type == "number" || (q.Type == "fill" && q.FillType == "number") {
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
		// limit=0 is an internal summary-only mode; the public API still
		// requires a positive page size when raw values are requested.
		values := make([]any, 0)
		hasMore := row.Answered > 0
		if limit > 0 {
			var err error
			values, hasMore, err = s.answerValues(ctx, survey.ID, q.ID, offset, limit)
			if err != nil {
				return nil, err
			}
		}
		optionStats := make([]map[string]any, 0, len(q.Options))
		for j, option := range q.Options {
			count := int64(0)
			if j < len(row.OptionCounts) {
				count = row.OptionCounts[j]
			}
			optionStats = append(optionStats, map[string]any{"id": option.ID, "label": option.Label, "count": count})
		}
		questionStats := map[string]any{
			"id": q.ID, "title": q.Title, "type": q.Type, "answered": row.Answered,
			"options": optionStats, "values": values, "average": average,
			"valuesOffset": offset, "valuesLimit": limit, "hasMore": hasMore,
		}
		if q.Type == "fill" {
			questionStats["fillType"] = q.FillType
		}
		questions = append(questions, questionStats)
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
