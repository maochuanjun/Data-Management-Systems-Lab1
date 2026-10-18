package integration_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"questionnaire/internal/domain"
	"questionnaire/internal/web"
	"questionnaire/tests/support"
)

// These documents are versioned fixtures, deliberately loaded without marshaling
// current domain structs. Changing a current struct must not silently change the
// historical schema that this compatibility contract exercises.
func TestVersionOneDocumentsRemainUsable(t *testing.T) {
	fixture := support.NewMongo(t)
	for _, seed := range []struct{ file, collection string }{
		{"user.json", "users"},
		{"survey-published.json", "surveys"},
		{"survey-draft.json", "surveys"},
		{"response.json", "responses"},
	} {
		data, err := os.ReadFile(filepath.Join("..", "fixtures", "v1", seed.file))
		if err != nil {
			t.Fatal(err)
		}
		var document bson.M
		if err := bson.UnmarshalExtJSON(data, false, &document); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.DB.Collection(seed.collection).InsertOne(fixture.Context, document); err != nil {
			t.Fatalf("insert %s: %v", seed.file, err)
		}
	}
	var oldResponse bson.M
	if err := fixture.DB.Collection("responses").FindOne(fixture.Context, bson.M{"_id": "legacy-response"}).Decode(&oldResponse); err != nil {
		t.Fatal(err)
	}
	// Init must safely reapply validators and indexes around existing v1 data.
	for range 2 {
		if err := fixture.Store.Init(fixture.Context); err != nil {
			t.Fatalf("Init over version one documents: %v", err)
		}
	}
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	// The original respondent's first retry must already be rejected, including
	// switching between named and anonymous submission after the upgrade.
	legacyRespondent := legacyDedupeClient(t, fixture, server.URL, "legacy-respondent")
	for _, anonymous := range []bool{false, true} {
		legacyRespondent.must(t, "POST", "/api/public/surveys/legacy-published/responses",
			integrationAnswers(anonymous, map[string]any{"single": "finish"}), 409)
	}
	if count, err := fixture.DB.Collection("responses").CountDocuments(fixture.Context, bson.M{"surveyId": "legacy-published"}); err != nil || count != 1 {
		t.Fatalf("legacy retries must preserve the one historical response: count=%d err=%v", count, err)
	}
	owner := newIntegrationClient(t, server.URL)
	owner.must(t, "POST", "/api/login", map[string]string{"username": "legacy_owner", "password": "legacy-password-123"}, 200)
	surveys := integrationDecode[[]domain.Survey](t, owner.must(t, "GET", "/api/surveys", nil, 200))
	if len(surveys) != 2 {
		t.Fatalf("legacy surveys = %v", surveys)
	}
	for _, id := range []string{"legacy-published", "legacy-draft"} {
		survey := integrationDecode[domain.Survey](t, owner.must(t, "GET", "/api/surveys/"+id, nil, 200))
		if survey.SchemaVersion != 1 || survey.Archived || survey.OwnerID != "legacy-owner" || len(survey.Questions) != 4 || !survey.CreatedAt.Equal(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)) {
			t.Fatalf("legacy survey did not retain its schema: %#v", survey)
		}
	}
	published := integrationDecode[domain.Survey](t, owner.must(t, "POST", "/api/surveys/legacy-draft/publish", nil, 200))
	if published.Status != "published" || published.SchemaVersion != 1 || published.Revision != 2 {
		t.Fatalf("legacy draft publish = %#v", published)
	}
	respondent := newIntegrationClient(t, server.URL)
	respondent.must(t, "POST", "/api/register", map[string]string{"username": "new_respondent", "password": "test-password-123"}, 201)
	for _, id := range []string{"legacy-published", "legacy-draft"} {
		public := "/api/public/surveys/" + id
		respondent.must(t, "GET", public, nil, 200)
		next := integrationDecode[map[string]any](t, respondent.must(t, "POST", public+"/next", map[string]any{"questionId": "single", "answer": "finish"}, 200))
		if next["nextQuestionId"] != "END" {
			t.Fatalf("legacy rule = %v", next)
		}
		respondent.must(t, "POST", public+"/responses", integrationAnswers(false, map[string]any{
			"single": "continue", "multiple": []string{"b", "c"}, "text": "新增答案", "number": 20,
		}), 201)
	}
	var stats struct {
		Total     int `json:"totalResponses"`
		Questions []struct {
			ID       string   `json:"id"`
			Answered int      `json:"answered"`
			Average  *float64 `json:"average"`
			Values   []any    `json:"values"`
			Options  []struct {
				ID    string `json:"id"`
				Count int    `json:"count"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(owner.must(t, "GET", "/api/surveys/legacy-published/stats", nil, 200), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 || len(stats.Questions) != 4 {
		t.Fatalf("combined legacy/current statistics = %#v", stats)
	}
	for _, q := range stats.Questions {
		if q.Answered != 2 {
			t.Fatalf("question %s answered = %d", q.ID, q.Answered)
		}
		if q.ID == "number" && (q.Average == nil || *q.Average != 15) {
			t.Fatalf("legacy/current average = %v", q.Average)
		}
		if q.ID == "text" && !reflect.DeepEqual(q.Values, []any{"原始答案", "新增答案"}) {
			t.Fatalf("legacy/current text = %v", q.Values)
		}
		if q.ID == "multiple" {
			want := map[string]int{"a": 1, "b": 2, "c": 1}
			for _, option := range q.Options {
				if option.Count != want[option.ID] {
					t.Fatalf("legacy/current selection count = %v", q.Options)
				}
			}
		}
	}
	var retained bson.M
	if err := fixture.DB.Collection("responses").FindOne(fixture.Context, bson.M{"_id": "legacy-response"}).Decode(&retained); err != nil {
		t.Fatal(err)
	}
	if retained["dedupeKey"] != legacyDedupeKey("legacy-published", "legacy-respondent") {
		t.Fatalf("historical response lacks its recovered account guard: %#v", retained)
	}
	// The compatibility migration may add its auxiliary key; all original data,
	// including answers, path, identity, schema version, and timestamp, must stay.
	delete(retained, "dedupeKey")
	if !reflect.DeepEqual(retained, oldResponse) {
		t.Fatalf("legacy response changed: before=%v after=%v", oldResponse, retained)
	}
}
