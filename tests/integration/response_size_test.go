package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"questionnaire/internal/domain"
	"questionnaire/internal/web"
	"questionnaire/tests/support"
)

func TestMaximumEscapedUnicodeResponsePersistsInMongoDB(t *testing.T) {
	fixture := support.NewMongo(t)
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	client := newIntegrationClient(t, server.URL)
	user := integrationDecode[domain.User](t, client.must(t, http.MethodPost, "/api/register", map[string]string{
		"username": "large_text_owner", "password": "test-password-123",
	}, http.StatusCreated))

	const questionCount, answerLength = 100, 10000
	questions := make([]domain.Question, questionCount)
	wantPath := make([]string, questionCount)
	escaped := json.RawMessage(`"` + strings.Repeat(`\ud83d\ude42`, answerLength) + `"`)
	escapedAnswers := make(map[string]json.RawMessage, questionCount)
	for index := range questions {
		id := strings.Repeat("q", 61) + fmt.Sprintf("%03d", index+1)
		questions[index] = domain.Question{
			ID: id, Title: fmt.Sprintf("Text %d", index+1),
			Type: "fill", FillType: "text", Required: true,
			Validation: domain.Validation{MinLength: integrationInt(answerLength), MaxLength: integrationInt(answerLength)},
		}
		wantPath[index] = id
		escapedAnswers[id] = escaped
	}
	survey := integrationDecode[domain.Survey](t, client.must(t, http.MethodPost, "/api/surveys", domain.Survey{
		Title: "Maximum escaped Unicode response", AllowRepeat: true, Questions: questions,
	}, http.StatusCreated))
	client.must(t, http.MethodPost, "/api/surveys/"+survey.ID+"/publish", nil, http.StatusOK)
	body, err := json.Marshal(map[string]any{"anonymous": false, "answers": escapedAnswers})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) <= 12*questionCount*answerLength || len(body) >= 16<<20 {
		t.Fatalf("unexpected fully escaped request size: %d", len(body))
	}
	created := integrationDecode[struct {
		ID string `json:"id"`
	}](t, client.must(t, http.MethodPost, "/api/public/surveys/"+survey.ID+"/responses", json.RawMessage(body), http.StatusCreated))
	if created.ID == "" {
		t.Fatal("successful submission must return its persisted response ID")
	}

	// Read the actual BSON document back, independently of the HTTP return body.
	// Escaped JSON uses about 12 MB, while its decoded UTF-8 answer text uses 4 MB.
	filter := bson.M{"_id": created.ID, "surveyId": survey.ID}
	raw, err := fixture.DB.Collection("responses").FindOne(fixture.Context, filter).Raw()
	if err != nil {
		t.Fatalf("read persisted large response: %v", err)
	}
	if len(raw) <= 4*questionCount*answerLength || len(raw) >= 5<<20 {
		t.Fatalf("expected about 4 MB of BSON after decoding, got %d bytes", len(raw))
	}
	var stored domain.Response
	if err := bson.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != created.ID || stored.SurveyID != survey.ID || stored.UserID != user.ID || stored.Anonymous {
		t.Fatal("large response lost its identity or survey association")
	}
	if len(stored.Answers) != questionCount || !reflect.DeepEqual(stored.Path, wantPath) {
		t.Fatalf("large response lost answers or path: answers=%d path=%d", len(stored.Answers), len(stored.Path))
	}
	wantAnswer := strings.Repeat("🙂", answerLength)
	for _, id := range wantPath {
		answer, ok := stored.Answers[id].(string)
		if !ok || answer != wantAnswer || utf8.RuneCountInString(answer) != answerLength {
			t.Fatalf("question %s: text was changed or truncated, characters=%d", id, utf8.RuneCountInString(answer))
		}
	}
	if count, err := fixture.DB.Collection("responses").CountDocuments(fixture.Context, bson.M{"surveyId": survey.ID}); err != nil || count != 1 {
		t.Fatalf("expected one complete MongoDB response: count=%d error=%v", count, err)
	}
	t.Logf("maximum legal text survey: HTTP 201, request=%d bytes, stored BSON=%d bytes, answers=%d, characters_per_answer=%d, path=%d",
		len(body), len(raw), len(stored.Answers), answerLength, len(stored.Path))
}
