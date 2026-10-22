package store

import (
	"context"
	"errors"
	"fmt"

	"questionnaire/internal/domain"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *Store) initLibrary(ctx context.Context) error {
	str := bson.M{"bsonType": "string"}
	integer := bson.M{"bsonType": bson.A{"int", "long"}, "minimum": 1}
	strings := bson.M{"bsonType": bson.A{"array", "null"}, "items": str}
	question := collectionSchemas()["surveys"]["properties"].(bson.M)["questions"].(bson.M)["items"]
	schemas := map[string]bson.M{
		"question_templates": {"bsonType": "object", "required": bson.A{"_id", "ownerId", "sharedWith", "nextVersion", "ready", "createdAt"}, "properties": bson.M{
			"_id": str, "ownerId": str, "sharedWith": strings, "nextVersion": integer, "ready": bson.M{"bsonType": "bool"}, "createdAt": bson.M{"bsonType": "date"},
			"sharingRevision": bson.M{"bsonType": bson.A{"int", "long"}, "minimum": 0},
		}},
		"question_versions": {"bsonType": "object", "required": bson.A{"_id", "questionId", "version", "question", "note", "createdAt"}, "properties": bson.M{
			"_id": str, "questionId": str, "version": integer, "parentVersionId": str, "restoredFromVersionId": str, "question": question, "note": str, "createdAt": bson.M{"bsonType": "date"},
		}},
		"question_banks": {"bsonType": "object", "required": bson.A{"_id", "ownerId", "name", "description", "versionIds", "revision", "createdAt"}, "properties": bson.M{
			"_id": str, "ownerId": str, "name": str, "description": str, "versionIds": strings, "revision": bson.M{"bsonType": bson.A{"int", "long"}, "minimum": 0}, "createdAt": bson.M{"bsonType": "date"},
		}},
	}
	for name, schema := range schemas {
		validator := bson.M{"$jsonSchema": schema}
		err := s.db.CreateCollection(ctx, name, options.CreateCollection().SetValidator(validator))
		if err != nil {
			var commandError mongo.CommandError
			if !errors.As(err, &commandError) || commandError.Code != 48 {
				return fmt.Errorf("create %s: %w", name, err)
			}
			if err := s.setValidator(ctx, name, validator); err != nil {
				return fmt.Errorf("validate %s: %w", name, err)
			}
		}
	}
	indexes := map[string][]mongo.IndexModel{
		"question_templates": {
			{Keys: bson.D{{Key: "ownerId", Value: 1}, {Key: "ready", Value: 1}, {Key: "createdAt", Value: -1}}},
			{Keys: bson.D{{Key: "sharedWith", Value: 1}, {Key: "ready", Value: 1}}},
		},
		"question_versions": {{Keys: bson.D{{Key: "questionId", Value: 1}, {Key: "version", Value: 1}}, Options: options.Index().SetUnique(true)}},
		"question_banks":    {{Keys: bson.D{{Key: "ownerId", Value: 1}, {Key: "createdAt", Value: -1}}}},
		"surveys": {
			{Keys: bson.D{{Key: "ownerId", Value: 1}, {Key: "questions.sourceQuestionId", Value: 1}}, Options: options.Index().SetName("owner_question_sources")},
			{Keys: bson.D{{Key: "questions.sourceQuestionId", Value: 1}, {Key: "questions.sourceVersionId", Value: 1}}, Options: options.Index().SetName("question_sources")},
		},
	}
	for name, models := range indexes {
		if _, err := s.db.Collection(name).Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("index %s: %w", name, err)
		}
	}
	// Creation spans two documents on standalone MongoDB. Never delete either
	// after an uncertain write: a later Init publishes only complete families.
	cursor, err := s.db.Collection("question_templates").Find(ctx, bson.M{"ready": false}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var pending struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&pending); err != nil {
			return err
		}
		err := s.db.Collection("question_versions").FindOne(ctx, bson.M{"questionId": pending.ID, "version": 1}, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
		if errors.Is(err, mongo.ErrNoDocuments) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := s.db.Collection("question_templates").UpdateOne(ctx, bson.M{"_id": pending.ID, "ready": false}, bson.M{"$set": bson.M{"ready": true}}); err != nil {
			return err
		}
	}
	return cursor.Err()
}

func (s *Store) ListQuestionTemplates(ctx context.Context, userID string) ([]domain.QuestionTemplate, error) {
	cursor, err := s.db.Collection("question_templates").Find(ctx, bson.M{"ready": true, "$or": bson.A{bson.M{"ownerId": userID}, bson.M{"sharedWith": userID}}}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make([]domain.QuestionTemplate, 0)
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	for i := range result {
		var latest domain.QuestionVersion
		if err := s.db.Collection("question_versions").FindOne(ctx, bson.M{"questionId": result[i].ID}, options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})).Decode(&latest); err != nil {
			return nil, translate(err)
		}
		result[i].LatestVersion = &latest
	}
	return result, nil
}

func (s *Store) QuestionTemplate(ctx context.Context, id string) (*domain.QuestionTemplate, error) {
	var result domain.QuestionTemplate
	if err := s.db.Collection("question_templates").FindOne(ctx, bson.M{"_id": id, "ready": true}).Decode(&result); err != nil {
		return nil, translate(err)
	}
	var latest domain.QuestionVersion
	if err := s.db.Collection("question_versions").FindOne(ctx, bson.M{"questionId": id}, options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})).Decode(&latest); err != nil {
		return nil, translate(err)
	}
	result.LatestVersion = &latest
	return &result, nil
}

func (s *Store) QuestionVersion(ctx context.Context, id string) (*domain.QuestionVersion, error) {
	var result domain.QuestionVersion
	if err := s.db.Collection("question_versions").FindOne(ctx, bson.M{"_id": id}).Decode(&result); err != nil {
		return nil, translate(err)
	}
	return &result, nil
}

func (s *Store) QuestionVersions(ctx context.Context, questionID string) ([]domain.QuestionVersion, error) {
	cursor, err := s.db.Collection("question_versions").Find(ctx, bson.M{"questionId": questionID}, options.Find().SetSort(bson.D{{Key: "version", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make([]domain.QuestionVersion, 0)
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) CreateQuestionTemplate(ctx context.Context, template *domain.QuestionTemplate, version *domain.QuestionVersion) error {
	template.Ready, template.NextVersion = false, 1
	version.QuestionID, version.Version = template.ID, 1
	if version.ParentVersionID != "" || version.RestoredFromVersionID != "" {
		return ErrConflict
	}
	if _, err := s.db.Collection("question_templates").InsertOne(ctx, template); err != nil {
		return translate(err)
	}
	if _, err := s.db.Collection("question_versions").InsertOne(ctx, version); err != nil {
		return translate(err)
	}
	result, err := s.db.Collection("question_templates").UpdateOne(ctx, bson.M{"_id": template.ID, "ready": false}, bson.M{"$set": bson.M{"ready": true}})
	if err != nil {
		return translate(err)
	}
	if result.MatchedCount == 0 {
		// A concurrent Init may already have recovered this completed family.
		if _, err := s.QuestionTemplate(ctx, template.ID); err != nil {
			return err
		}
	}
	template.Ready = true
	template.LatestVersion = version
	return nil
}

func (s *Store) CreateQuestionVersion(ctx context.Context, questionID, ownerID string, version *domain.QuestionVersion) error {
	for _, id := range []string{version.ParentVersionID, version.RestoredFromVersionID} {
		if id == "" {
			continue
		}
		parent, err := s.QuestionVersion(ctx, id)
		if err != nil {
			return err
		}
		if parent.QuestionID != questionID {
			return ErrConflict
		}
	}
	if version.ParentVersionID == "" {
		return ErrConflict
	}
	var template domain.QuestionTemplate
	// The counter allocates unique numbers, not a promise that every allocated
	// number is persisted. Failed inserts leave gaps and never mutate history.
	err := s.db.Collection("question_templates").FindOneAndUpdate(ctx, bson.M{"_id": questionID, "ownerId": ownerID, "ready": true}, bson.M{"$inc": bson.M{"nextVersion": 1}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&template)
	if err != nil {
		return translate(err)
	}
	version.QuestionID, version.Version = questionID, template.NextVersion
	_, err = s.db.Collection("question_versions").InsertOne(ctx, version)
	return translate(err)
}

func (s *Store) SetQuestionSharing(ctx context.Context, id, owner string, sharedWith []string, expectedRevision int) error {
	filter := bson.M{"_id": id, "ownerId": owner, "ready": true}
	if expectedRevision == 0 {
		// Older templates have no sharingRevision. Treat absence as zero and
		// create the counter in the same atomic write that changes permissions.
		filter["$or"] = bson.A{bson.M{"sharingRevision": 0}, bson.M{"sharingRevision": bson.M{"$exists": false}}}
	} else {
		filter["sharingRevision"] = expectedRevision
	}
	result, err := s.db.Collection("question_templates").UpdateOne(ctx, filter, bson.M{"$set": bson.M{"sharedWith": sharedWith}, "$inc": bson.M{"sharingRevision": 1}})
	if err != nil {
		return translate(err)
	}
	if result.MatchedCount == 0 {
		err := s.db.Collection("question_templates").FindOne(ctx, bson.M{"_id": id, "ownerId": owner, "ready": true}, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
		if err != nil {
			return translate(err)
		}
		return ErrConflict
	}
	return nil
}

func (s *Store) ListQuestionBanks(ctx context.Context, owner string) ([]domain.QuestionBank, error) {
	cursor, err := s.db.Collection("question_banks").Find(ctx, bson.M{"ownerId": owner}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make([]domain.QuestionBank, 0)
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) QuestionBank(ctx context.Context, id string) (*domain.QuestionBank, error) {
	var result domain.QuestionBank
	if err := s.db.Collection("question_banks").FindOne(ctx, bson.M{"_id": id}).Decode(&result); err != nil {
		return nil, translate(err)
	}
	return &result, nil
}

func (s *Store) CreateQuestionBank(ctx context.Context, bank *domain.QuestionBank) error {
	_, err := s.db.Collection("question_banks").InsertOne(ctx, bank)
	return translate(err)
}

func (s *Store) UpdateQuestionBank(ctx context.Context, bank *domain.QuestionBank, expectedRevision int) error {
	result, err := s.db.Collection("question_banks").UpdateOne(ctx, bson.M{"_id": bank.ID, "ownerId": bank.OwnerID, "revision": expectedRevision}, bson.M{"$set": bson.M{"name": bank.Name, "description": bank.Description, "versionIds": bank.VersionIDs}, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return translate(err)
	}
	if result.MatchedCount == 0 {
		return ErrConflict
	}
	bank.Revision = expectedRevision + 1
	return nil
}

func (s *Store) DeleteQuestionBank(ctx context.Context, id, owner string, revision int) error {
	result, err := s.db.Collection("question_banks").DeleteOne(ctx, bson.M{"_id": id, "ownerId": owner, "revision": revision})
	if err != nil {
		return translate(err)
	}
	if result.DeletedCount == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) QuestionUsage(ctx context.Context, questionID, viewerID string) (map[string]any, error) {
	template, err := s.QuestionTemplate(ctx, questionID)
	if err != nil {
		return nil, err
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"ownerId": viewerID, "questions.sourceQuestionId": questionID}}},
		{{Key: "$unwind", Value: "$questions"}},
		{{Key: "$match", Value: bson.M{"questions.sourceQuestionId": questionID}}},
		{{Key: "$sort", Value: bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: 1}, {Key: "questions.id", Value: 1}}}},
		{{Key: "$project", Value: bson.M{"_id": 0, "surveyId": "$_id", "title": 1, "status": 1, "archived": bson.M{"$ifNull": bson.A{"$archived", false}}, "questionId": "$questions.id", "versionId": "$questions.sourceVersionId"}}},
	}
	cursor, err := s.db.Collection("surveys").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	surveys := make([]bson.M, 0)
	if err := cursor.All(ctx, &surveys); err != nil {
		return nil, err
	}
	ownSurveys := map[string]bool{}
	for _, survey := range surveys {
		ownSurveys[survey["surveyId"].(string)] = true
	}
	totalSurveys, totalOccurrences := int64(len(ownSurveys)), int64(len(surveys))
	if template.OwnerID == viewerID {
		counts, err := s.questionReferenceCounts(ctx, questionID)
		if err != nil {
			return nil, err
		}
		totalSurveys, totalOccurrences = counts.Surveys, counts.Answered
	}
	return map[string]any{"questionId": questionID, "surveys": surveys, "ownSurveyCount": len(ownSurveys), "totalSurveyCount": totalSurveys, "totalOccurrenceCount": totalOccurrences}, nil
}

type libraryCounts struct {
	Answered int64 `bson:"answered"`
	Surveys  int64 `bson:"surveys"`
}

func (s *Store) questionReferenceCounts(ctx context.Context, questionID string) (libraryCounts, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"questions.sourceQuestionId": questionID}}},
		{{Key: "$unwind", Value: "$questions"}},
		{{Key: "$match", Value: bson.M{"questions.sourceQuestionId": questionID}}},
		{{Key: "$group", Value: bson.M{"_id": "$_id", "answered": bson.M{"$sum": 1}}}},
		{{Key: "$group", Value: bson.M{"_id": nil, "answered": bson.M{"$sum": "$answered"}, "surveys": bson.M{"$sum": 1}}}},
	}
	return s.libraryCounts(ctx, pipeline)
}

func (s *Store) libraryCounts(ctx context.Context, pipeline mongo.Pipeline) (libraryCounts, error) {
	cursor, err := s.db.Collection("surveys").Aggregate(ctx, pipeline)
	if err != nil {
		return libraryCounts{}, err
	}
	defer cursor.Close(ctx)
	var result libraryCounts
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return result, err
		}
	}
	return result, cursor.Err()
}

// questionAnswers joins only the viewer's surveys. The immediate lookup/unwind
// pair lets MongoDB stream responses instead of constructing a huge joined array.
// One row represents one answered occurrence, even when a version appears twice.
func questionAnswers(questionID, viewerID, versionID string) mongo.Pipeline {
	questionMatch := bson.M{"questions.sourceQuestionId": questionID}
	if versionID != "" {
		questionMatch["questions.sourceVersionId"] = versionID
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"ownerId": viewerID, "questions.sourceQuestionId": questionID}}},
		{{Key: "$unwind", Value: "$questions"}},
		{{Key: "$match", Value: questionMatch}},
		{{Key: "$project", Value: bson.M{"surveyId": "$_id", "questionId": "$questions.id", "versionId": "$questions.sourceVersionId"}}},
		{{Key: "$lookup", Value: bson.M{"from": "responses", "localField": "surveyId", "foreignField": "surveyId", "as": "response"}}},
		{{Key: "$unwind", Value: "$response"}},
		{{Key: "$project", Value: bson.M{"_id": 0, "surveyId": 1, "questionId": 1, "versionId": 1, "responseId": "$response._id", "submittedAt": "$response.submittedAt", "value": bson.M{"$getField": bson.M{"field": "$questionId", "input": "$response.answers"}}}}},
		{{Key: "$match", Value: bson.M{"value": bson.M{"$exists": true, "$ne": nil}}}},
	}
	return pipeline
}

func (s *Store) QuestionStats(ctx context.Context, questionID, viewerID, versionID string, offset, limit int) (map[string]any, error) {
	if offset < 0 || limit < 0 || limit > 50 {
		return nil, errors.New("statistics offset must be nonnegative and limit between 0 and 50")
	}
	if _, err := s.QuestionTemplate(ctx, questionID); err != nil {
		return nil, err
	}
	var versions []domain.QuestionVersion
	if versionID != "" {
		version, err := s.QuestionVersion(ctx, versionID)
		if err != nil {
			return nil, err
		}
		if version.QuestionID != questionID {
			return nil, ErrNotFound
		}
		versions = []domain.QuestionVersion{*version}
	} else {
		var err error
		versions, err = s.QuestionVersions(ctx, questionID)
		if err != nil {
			return nil, err
		}
		// The all-version view is summary-only regardless of requested paging.
		offset, limit = 0, 0
	}
	pipeline := questionAnswers(questionID, viewerID, versionID)
	pipeline = append(pipeline,
		bson.D{{Key: "$group", Value: bson.M{"_id": "$surveyId", "answered": bson.M{"$sum": 1}}}},
		bson.D{{Key: "$group", Value: bson.M{"_id": nil, "answered": bson.M{"$sum": "$answered"}, "surveys": bson.M{"$sum": 1}}}},
	)
	counts, err := s.libraryCounts(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(versions))
	for _, version := range versions {
		result, err := s.questionVersionStats(ctx, version, viewerID, offset, limit)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return map[string]any{"questionId": questionID, "totalAnswers": counts.Answered, "surveyCount": counts.Surveys, "versions": results}, nil
}

func (s *Store) questionVersionStats(ctx context.Context, version domain.QuestionVersion, viewerID string, offset, limit int) (map[string]any, error) {
	q := version.Question
	group := bson.M{"_id": nil, "answered": bson.M{"$sum": 1}}
	numeric := q.Type == "number" || (q.Type == "fill" && q.FillType == "number")
	if numeric {
		group["average"] = bson.M{"$avg": "$value"}
	}
	optionCounts := make(bson.A, 0, len(q.Options))
	for i, option := range q.Options {
		key := fmt.Sprintf("o%d", i)
		condition := bson.M{"$eq": bson.A{"$value", bson.M{"$literal": option.ID}}}
		if q.Type == "multiple" {
			condition = bson.M{"$in": bson.A{bson.M{"$literal": option.ID}, bson.M{"$cond": bson.A{bson.M{"$isArray": "$value"}, "$value", bson.A{}}}}}
		}
		group[key] = bson.M{"$sum": bson.M{"$cond": bson.A{condition, 1, 0}}}
		optionCounts = append(optionCounts, "$"+key)
	}
	facets := bson.M{
		"summary": mongo.Pipeline{
			{{Key: "$group", Value: group}},
			{{Key: "$project", Value: bson.M{"answered": 1, "average": 1, "optionCounts": optionCounts}}},
		},
		"surveys": mongo.Pipeline{{{Key: "$group", Value: bson.M{"_id": "$surveyId"}}}, {{Key: "$count", Value: "count"}}},
	}
	if numeric {
		facets["distribution"] = mongo.Pipeline{
			{{Key: "$match", Value: bson.M{"value": bson.M{"$type": "number"}}}},
			{{Key: "$bucketAuto", Value: bson.M{"groupBy": "$value", "buckets": 10}}},
			{{Key: "$project", Value: bson.M{"_id": 0, "min": "$_id.min", "max": "$_id.max", "count": 1}}},
		}
	}
	if limit > 0 {
		facets["values"] = mongo.Pipeline{
			{{Key: "$sort", Value: bson.D{{Key: "submittedAt", Value: 1}, {Key: "responseId", Value: 1}, {Key: "surveyId", Value: 1}, {Key: "questionId", Value: 1}}}},
			{{Key: "$skip", Value: int64(offset)}},
			{{Key: "$limit", Value: int64(limit + 1)}},
			{{Key: "$project", Value: bson.M{"_id": 0, "value": 1}}},
		}
	}
	pipeline := append(questionAnswers(version.QuestionID, viewerID, version.ID), bson.D{{Key: "$facet", Value: facets}})
	cursor, err := s.db.Collection("surveys").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var result struct {
		Summary []struct {
			Answered     int64    `bson:"answered"`
			Average      *float64 `bson:"average"`
			OptionCounts []int64  `bson:"optionCounts"`
		} `bson:"summary"`
		Surveys []struct {
			Count int64 `bson:"count"`
		} `bson:"surveys"`
		Distribution []bson.M `bson:"distribution"`
		Values       []struct {
			Value any `bson:"value"`
		} `bson:"values"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return nil, err
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	answered, surveyCount := int64(0), int64(0)
	var average any
	counts := []int64{}
	if len(result.Summary) > 0 {
		row := result.Summary[0]
		answered, counts = row.Answered, row.OptionCounts
		if row.Average != nil {
			average = *row.Average
		}
	}
	if len(result.Surveys) > 0 {
		surveyCount = result.Surveys[0].Count
	}
	optionStats := make([]map[string]any, 0, len(q.Options))
	for i, option := range q.Options {
		count := int64(0)
		if i < len(counts) {
			count = counts[i]
		}
		optionStats = append(optionStats, map[string]any{"id": option.ID, "label": option.Label, "count": count})
	}
	values := make([]any, 0, limit)
	for i, value := range result.Values {
		if i >= limit {
			break
		}
		values = append(values, value.Value)
	}
	if result.Distribution == nil {
		result.Distribution = []bson.M{}
	}
	return map[string]any{
		"versionId": version.ID, "version": version.Version, "title": q.Title, "type": q.Type, "fillType": q.FillType,
		"answered": answered, "surveyCount": surveyCount, "options": optionStats, "average": average,
		"numericDistribution": result.Distribution, "values": values, "valuesOffset": offset, "valuesLimit": limit,
		"hasMore": limit > 0 && len(result.Values) > limit,
	}, nil
}
