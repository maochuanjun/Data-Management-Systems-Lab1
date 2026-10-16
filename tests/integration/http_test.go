package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"questionnaire/internal/domain"
	"questionnaire/internal/web"
	"questionnaire/tests/support"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type integrationHTTP struct {
	url    string
	client *http.Client
}

func (c integrationHTTP) send(method, path string, body any) (int, []byte, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
	}
	req, err := http.NewRequest(method, c.url+path, bytes.NewReader(data))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, err = io.ReadAll(response.Body)
	return response.StatusCode, data, err
}

func (c integrationHTTP) must(t *testing.T, method, path string, body any, want int) []byte {
	t.Helper()
	status, data, err := c.send(method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if status != want {
		t.Fatalf("%s %s: status %d, want %d; body %s", method, path, status, want, data)
	}
	return data
}

func integrationDecode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return value
}

func integrationInt(n int) *int           { return &n }
func integrationFloat(n float64) *float64 { return &n }
func integrationAnswers(anonymous bool, answers map[string]any) map[string]any {
	return map[string]any{"anonymous": anonymous, "answers": answers}
}

// Set INTEGRATION_MONGO_URI to opt in. Every run creates and drops only its own
// randomly named questionnaire_test_* database, never the URI's default DB.
func TestMongoHTTPIntegration(t *testing.T) {
	fixture := support.NewMongo(t)
	repo, ctx, db := fixture.Store, fixture.Context, fixture.DB
	server := httptest.NewServer(web.New(repo, false))
	t.Cleanup(server.Close)
	newClient := func() integrationHTTP {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		return integrationHTTP{server.URL, &http.Client{Jar: jar, Timeout: 20 * time.Second}}
	}
	owner, alice, bob, charlie, guest := newClient(), newClient(), newClient(), newClient(), newClient()
	password := "test-password-123"
	var aliceID string
	for i, client := range []integrationHTTP{owner, alice, bob, charlie} {
		name := []string{"owner", "alice", "bob", "charlie"}[i]
		user := integrationDecode[domain.User](t, client.must(t, "POST", "/api/register", map[string]string{"username": name, "password": password}, 201))
		if user.ID == "" || user.Username != name {
			t.Fatalf("bad account response: %#v", user)
		}
		if i == 1 {
			aliceID = user.ID
		}
		client.must(t, "GET", "/api/me", nil, 200)
	}
	guest.must(t, "POST", "/api/register", map[string]string{"username": "alice", "password": password}, 409)
	// Password minimum is eight characters, while the bcrypt upper bound
	// remains 72 UTF-8 bytes.
	unicodeClient := newClient()
	unicodeClient.must(t, "POST", "/api/register", map[string]string{"username": "short_unicode", "password": "中文中文"}, 400)
	unicodeClient.must(t, "POST", "/api/register", map[string]string{"username": "valid_unicode", "password": "中文中文中文中文中文中文中文中文"}, 201)
	alice.must(t, "POST", "/api/logout", nil, 200)
	alice.must(t, "GET", "/api/me", nil, 401)
	alice.must(t, "POST", "/api/login", map[string]string{"username": "alice", "password": "wrong-password"}, 401)
	alice.must(t, "POST", "/api/login", map[string]string{"username": "alice", "password": password}, 200)

	draft := integrationDecode[domain.Survey](t, owner.must(t, "POST", "/api/surveys", domain.Survey{Title: "集成测试", AllowAnonymous: true}, 201))
	private := "/api/surveys/" + draft.ID
	public := "/api/public/surveys/" + draft.ID
	owner.must(t, "POST", private+"/publish", nil, 400)
	alice.must(t, "GET", public, nil, 404)
	for _, suffix := range []string{"", "/stats"} {
		alice.must(t, "GET", private+suffix, nil, 404)
	}
	alice.must(t, "PUT", private, draft, 404)
	alice.must(t, "POST", private+"/publish", nil, 404)
	if list := integrationDecode[[]domain.Survey](t, alice.must(t, "GET", "/api/surveys", nil, 200)); len(list) != 0 {
		t.Fatalf("foreign survey in list: %#v", list)
	}
	if list := integrationDecode[[]domain.Survey](t, owner.must(t, "GET", "/api/surveys", nil, 200)); len(list) != 1 || list[0].ID != draft.ID {
		t.Fatalf("owner list: %#v", list)
	}

	// Reject impossible required constraints both on API writes and when
	// publishing a draft previously stored by an older application version.
	for _, q := range []domain.Question{
		{ID: "q", Title: "必答文本", Type: "text", Required: true, Validation: domain.Validation{MaxLength: integrationInt(0)}},
		{ID: "q", Title: "必答多选", Type: "multiple", Required: true, Options: []domain.Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}, Validation: domain.Validation{MaxSelect: integrationInt(0)}},
		{ID: "q", Title: "必答整数", Type: "number", Required: true, Validation: domain.Validation{Integer: true, Min: integrationFloat(.1), Max: integrationFloat(.9)}},
	} {
		invalid := draft
		invalid.Questions = []domain.Question{q}
		owner.must(t, "POST", "/api/surveys", invalid, 400)
		owner.must(t, "PUT", private, invalid, 400)
		if _, err := db.Collection("surveys").UpdateOne(ctx, bson.M{"_id": draft.ID}, bson.M{"$set": bson.M{"questions": invalid.Questions}}); err != nil {
			t.Fatal(err)
		}
		owner.must(t, "POST", private+"/publish", nil, 400)
	}
	if _, err := db.Collection("surveys").UpdateOne(ctx, bson.M{"_id": draft.ID}, bson.M{"$set": bson.M{"questions": draft.Questions}}); err != nil {
		t.Fatal(err)
	}

	draft.Questions = []domain.Question{
		{ID: "q1", Title: "单选", Type: "single", Required: true,
			Options: []domain.Option{{ID: "a", Label: "跳过多选"}, {ID: "b", Label: "继续"}, {ID: "end", Label: "结束"}},
			Rules:   []domain.Rule{{Operator: "eq", Value: "a", Target: "q3"}, {Operator: "eq", Value: "end", Target: "END"}}},
		{ID: "q2", Title: "多选", Type: "multiple", Required: true,
			Options:    []domain.Option{{ID: "x", Label: "甲"}, {ID: "y", Label: "乙"}, {ID: "z", Label: "结束"}},
			Validation: domain.Validation{MinSelect: integrationInt(1), MaxSelect: integrationInt(2)},
			Rules:      []domain.Rule{{Operator: "contains_all", Value: []any{"x", "y"}, Target: "q4"}, {Operator: "contains", Value: "z", Target: "END"}}},
		{ID: "q3", Title: "数字", Type: "number", Required: true, Validation: domain.Validation{Integer: true, Min: integrationFloat(0), Max: integrationFloat(100)},
			Rules: []domain.Rule{{Operator: "gte", Value: 18.0, Target: "END"}}},
		{ID: "q4", Title: "中文文本", Type: "text", Required: true, Validation: domain.Validation{MinLength: integrationInt(2), MaxLength: integrationInt(3)}},
	}
	updated := integrationDecode[domain.Survey](t, owner.must(t, "PUT", private, draft, 200))
	if updated.Revision != draft.Revision+1 {
		t.Fatalf("revision = %d", updated.Revision)
	}
	owner.must(t, "PUT", private, draft, 409)
	published := integrationDecode[domain.Survey](t, owner.must(t, "POST", private+"/publish", nil, 200))
	if published.Status != "published" || published.Revision != updated.Revision+1 {
		t.Fatalf("publish = %#v", published)
	}
	owner.must(t, "PUT", private, updated, 409)
	owner.must(t, "POST", private+"/publish", nil, 409)
	guest.must(t, "GET", public, nil, 401)
	guest.must(t, "POST", public+"/next", map[string]any{"questionId": "q1", "answer": "a"}, 401)
	guest.must(t, "POST", public+"/responses", integrationAnswers(true, map[string]any{"q1": "end"}), 401)
	publicBody := integrationDecode[map[string]any](t, alice.must(t, "GET", public, nil, 200))
	if _, present := publicBody["ownerId"]; present {
		t.Fatal("public survey exposes ownerId")
	}

	for _, tc := range []struct {
		id     string
		answer any
		next   string
	}{
		{"q1", "a", "q3"}, {"q1", "b", "q2"}, {"q1", "end", "END"},
		{"q2", []string{"x", "y"}, "q4"}, {"q2", []string{"z"}, "END"}, {"q2", []string{"x"}, "q3"},
		{"q3", 18, "END"}, {"q3", 10, "q4"}, {"q4", "中文🙂", "END"},
	} {
		result := integrationDecode[map[string]any](t, alice.must(t, "POST", public+"/next", map[string]any{"questionId": tc.id, "answer": tc.answer}, 200))
		if result["nextQuestionId"] != tc.next {
			t.Errorf("next for %s %v = %v, want %s", tc.id, tc.answer, result, tc.next)
		}
	}
	for _, tc := range []struct {
		id     string
		answer any
	}{
		{"q1", "missing"}, {"q2", []string{"x", "x"}}, {"q2", []string{}}, {"q2", []string{"x", "y", "z"}},
		{"q3", 10.5}, {"q3", 101}, {"q3", -1}, {"q3", "18"}, {"q4", "中"}, {"q4", "中文测试"}, {"unknown", nil},
	} {
		alice.must(t, "POST", public+"/next", map[string]any{"questionId": tc.id, "answer": tc.answer}, 400)
	}
	for _, answers := range []map[string]any{
		{"q1": "a"}, {"q1": "a", "q3": 10}, {"q1": "a", "q3": 20, "q2": nil},
		{"q1": "end", "unknown": nil}, {"q1": "a", "q3": 20.5}, {"q1": "b", "q2": []string{"x", "y"}, "q4": "中"},
	} {
		alice.must(t, "POST", public+"/responses", integrationAnswers(true, answers), 400)
	}

	anonymous := integrationDecode[map[string]any](t, alice.must(t, "POST", public+"/responses", integrationAnswers(true, map[string]any{"q1": "a", "q3": 20}), 201))
	var stored bson.M
	if err := db.Collection("responses").FindOne(ctx, bson.M{"_id": anonymous["id"]}).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if _, present := stored["userId"]; present {
		t.Fatal("anonymous response stores userId")
	}
	dedupe, ok := stored["dedupeKey"].(string)
	if !ok || len(dedupe) != 64 || strings.Contains(dedupe, aliceID) {
		t.Fatalf("invalid private dedupe key: %#v", stored)
	}
	if !reflect.DeepEqual(stored["path"], bson.A{"q1", "q3"}) {
		t.Fatalf("server path: %#v", stored["path"])
	}
	for _, anonymous := range []bool{true, false} {
		alice.must(t, "POST", public+"/responses", integrationAnswers(anonymous, map[string]any{"q1": "end"}), 409)
	}
	bob.must(t, "POST", public+"/responses", integrationAnswers(false, map[string]any{"q1": "b", "q2": []string{"x", "y"}, "q4": "中文🙂"}), 201)
	charlie.must(t, "POST", public+"/responses", integrationAnswers(false, map[string]any{"q1": "b", "q2": []string{"x"}, "q3": 10, "q4": "中文"}), 201)

	var stats struct {
		Total     int `json:"totalResponses"`
		Questions []struct {
			ID       string   `json:"id"`
			Answered int      `json:"answered"`
			Average  *float64 `json:"average"`
			Options  []struct {
				ID    string `json:"id"`
				Count int    `json:"count"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(owner.must(t, "GET", private+"/stats", nil, 200), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Total != 3 || len(stats.Questions) != 4 {
		t.Fatalf("stats totals: %#v", stats)
	}
	expectedAnswered := map[string]int{"q1": 3, "q2": 2, "q3": 2, "q4": 2}
	expectedCounts := map[string]map[string]int{"q1": {"a": 1, "b": 2, "end": 0}, "q2": {"x": 2, "y": 1, "z": 0}}
	for _, question := range stats.Questions {
		if question.Answered != expectedAnswered[question.ID] {
			t.Errorf("answered %s = %d", question.ID, question.Answered)
		}
		if question.ID == "q3" && (question.Average == nil || *question.Average != 15) {
			t.Errorf("numeric average: %v", question.Average)
		}
		for _, option := range question.Options {
			if option.Count != expectedCounts[question.ID][option.ID] {
				t.Errorf("count %s.%s = %d", question.ID, option.ID, option.Count)
			}
		}
	}
	if err := json.Unmarshal(owner.must(t, "GET", private+"/stats?questionId=q3", nil, 200), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Total != 3 || len(stats.Questions) != 1 || stats.Questions[0].ID != "q3" {
		t.Fatalf("single question stats: %#v", stats)
	}
	owner.must(t, "GET", private+"/stats?questionId=unknown", nil, 404)
	var summary struct {
		Total int `json:"totalResponses"`
		Questions []struct {
			ID string `json:"id"`
			Answered int `json:"answered"`
			Values []any `json:"values"`
			ValuesLimit int `json:"valuesLimit"`
			HasMore bool `json:"hasMore"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(owner.must(t, "GET", private+"/stats?summaryOnly=true", nil, 200), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Total != 3 || len(summary.Questions) != 4 {
		t.Fatalf("summary-only result: %#v", summary)
	}
	for _, q := range summary.Questions {
		if q.Answered != expectedAnswered[q.ID] || len(q.Values) != 0 || q.ValuesLimit != 0 || !q.HasMore {
			t.Fatalf("summary-only question: %#v", q)
		}
	}
	for _, query := range []string{"summaryOnly=1", "summaryOnly=", "summaryOnly=true&summaryOnly=false"} {
		owner.must(t, "GET", private+"/stats?"+query, nil, 400)
	}
	for _, query := range []string{
		"offset=-1", "offset=bad", "offset=", "offset=99999999999999999999999999",
		"offset=0&offset=1", "limit=0", "limit=51", "limit=-1", "limit=1.5", "limit=", "limit=1&limit=2",
	} {
		owner.must(t, "GET", private+"/stats?"+query, nil, 400)
	}
	pagedNumbers := make(map[float64]bool)
	for offset := 0; offset < 2; offset++ {
		var page struct {
			Questions []struct {
				Answered int       `json:"answered"`
				Average  float64   `json:"average"`
				Values   []float64 `json:"values"`
				Offset   int       `json:"valuesOffset"`
				Limit    int       `json:"valuesLimit"`
				HasMore  bool      `json:"hasMore"`
			} `json:"questions"`
		}
		query := private + "/stats?questionId=q3&limit=1&offset=" + fmt.Sprint(offset)
		if err := json.Unmarshal(owner.must(t, "GET", query, nil, 200), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Questions) != 1 {
			t.Fatalf("page: %#v", page)
		}
		q := page.Questions[0]
		if q.Answered != 2 || q.Average != 15 || len(q.Values) != 1 || q.Offset != offset || q.Limit != 1 || q.HasMore != (offset == 0) {
			t.Fatalf("incorrect paginated question: %#v", q)
		}
		pagedNumbers[q.Values[0]] = true
	}
	if len(pagedNumbers) != 2 || !pagedNumbers[10] || !pagedNumbers[20] {
		t.Fatalf("pages omitted or repeated answers: %v", pagedNumbers)
	}

	const attempts = 12
	type outcome struct {
		code int
		body []byte
		err  error
	}
	outcomes := make(chan outcome, attempts)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		workers.Add(1)
		go func(anonymous bool) {
			defer workers.Done()
			<-start
			code, body, err := owner.send("POST", public+"/responses", integrationAnswers(anonymous, map[string]any{"q1": "end"}))
			outcomes <- outcome{code, body, err}
		}(i%2 == 0)
	}
	close(start)
	workers.Wait()
	close(outcomes)
	successes := 0
	for result := range outcomes {
		if result.err != nil {
			t.Errorf("concurrent submit: %v", result.err)
			continue
		}
		if result.code == 201 {
			successes++
		} else if result.code != 409 {
			t.Errorf("concurrent status %d: %s", result.code, result.body)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent submissions saved %d, want exactly 1", successes)
	}
	if count, err := db.Collection("responses").CountDocuments(ctx, bson.M{"surveyId": draft.ID}); err != nil || count != 4 {
		t.Fatalf("deduped response count %d: %v", count, err)
	}

	repeat := integrationDecode[domain.Survey](t, owner.must(t, "POST", "/api/surveys", domain.Survey{Title: "允许重复", AllowRepeat: true, Questions: []domain.Question{{ID: "n", Title: "数字", Type: "number", Required: true}}}, 201))
	repeatPrivate := "/api/surveys/" + repeat.ID
	repeatPublic := "/api/public/surveys/" + repeat.ID
	owner.must(t, "POST", repeatPrivate+"/publish", nil, 200)
	alice.must(t, "POST", repeatPublic+"/responses", integrationAnswers(true, map[string]any{"n": 0}), 400)
	for i := 0; i < 5; i++ {
		alice.must(t, "POST", repeatPublic+"/responses", integrationAnswers(false, map[string]any{"n": i}), 201)
	}
	if count, err := db.Collection("responses").CountDocuments(ctx, bson.M{"surveyId": repeat.ID}); err != nil || count != 5 {
		t.Fatalf("repeat count %d: %v", count, err)
	}
	if count, err := db.Collection("responses").CountDocuments(ctx, bson.M{"surveyId": repeat.ID, "dedupeKey": bson.M{"$exists": true}}); err != nil || count != 0 {
		t.Fatalf("repeat dedupe count %d: %v", count, err)
	}
	owner.must(t, "POST", repeatPrivate+"/close", nil, 200)
	alice.must(t, "POST", repeatPublic+"/responses", integrationAnswers(false, map[string]any{"n": 0}), 409)
	alice.must(t, "POST", repeatPublic+"/next", map[string]any{"questionId": "n", "answer": 0}, 409)

	future := time.Now().Add(time.Hour)
	expiring := integrationDecode[domain.Survey](t, owner.must(t, "POST", "/api/surveys", domain.Survey{Title: "截止时间", Deadline: &future, Questions: []domain.Question{{ID: "n", Title: "数字", Type: "number", Required: true}}}, 201))
	owner.must(t, "POST", "/api/surveys/"+expiring.ID+"/publish", nil, 200)
	// Advance this test document's deadline without relying on sleeps or clock races.
	if _, err := db.Collection("surveys").UpdateOne(ctx, bson.M{"_id": expiring.ID}, bson.M{"$set": bson.M{"deadline": time.Now().Add(-time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	alice.must(t, "POST", "/api/public/surveys/"+expiring.ID+"/responses", integrationAnswers(false, map[string]any{"n": 1}), 409)
	alice.must(t, "POST", "/api/public/surveys/"+expiring.ID+"/next", map[string]any{"questionId": "n", "answer": 1}, 409)
	for _, collection := range []string{"users", "sessions", "surveys", "responses"} {
		_, err := db.Collection(collection).InsertOne(ctx, bson.M{"_id": "invalid-document"})
		if err == nil {
			t.Errorf("%s accepted invalid document", collection)
		} else {
			var writeErr mongo.WriteException
			if !errors.As(err, &writeErr) || !writeErr.HasErrorCode(121) {
				t.Errorf("%s failed for unexpected reason: %v", collection, err)
			}
		}
	}
}

func newIntegrationClient(t *testing.T, serverURL string) integrationHTTP {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return integrationHTTP{serverURL, &http.Client{Jar: jar, Timeout: 20 * time.Second}}
}

func TestDistinctAccountsSubmitConcurrently(t *testing.T) {
	fixture := support.NewMongo(t)
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	const accounts = 8
	clients := make([]integrationHTTP, accounts)
	for i := range clients {
		clients[i] = newIntegrationClient(t, server.URL)
		clients[i].must(t, "POST", "/api/register", map[string]string{"username": fmt.Sprintf("participant_%d", i), "password": "test-password-123"}, 201)
	}
	survey := integrationDecode[domain.Survey](t, clients[0].must(t, "POST", "/api/surveys", domain.Survey{
		Title: "Exactly two choices", AllowAnonymous: true, Questions: []domain.Question{
			{ID: "choices", Title: "Choose two", Type: "multiple", Required: true,
				Options:    []domain.Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}, {ID: "c", Label: "C"}},
				Validation: domain.Validation{MinSelect: integrationInt(2), MaxSelect: integrationInt(2)}},
		},
	}, 201))
	clients[0].must(t, "POST", "/api/surveys/"+survey.ID+"/publish", nil, 200)
	public := "/api/public/surveys/" + survey.ID
	for _, answer := range [][]string{{"a"}, {"a", "b", "c"}, {"a", "a"}} {
		clients[0].must(t, "POST", public+"/responses", integrationAnswers(false, map[string]any{"choices": answer}), 400)
	}
	start := make(chan struct{})
	type outcome struct {
		code int
		body []byte
		err  error
	}
	outcomes := make(chan outcome, accounts)
	for i, client := range clients {
		go func(i int, client integrationHTTP) {
			<-start
			code, body, err := client.send("POST", public+"/responses", integrationAnswers(i%2 == 0, map[string]any{"choices": []string{"a", "b"}}))
			outcomes <- outcome{code, body, err}
		}(i, client)
	}
	close(start)
	for range accounts {
		result := <-outcomes
		if result.err != nil || result.code != http.StatusCreated {
			t.Errorf("distinct user's first submission: status=%d body=%s err=%v", result.code, result.body, result.err)
		}
	}
	if count, err := fixture.DB.Collection("responses").CountDocuments(fixture.Context, bson.M{"surveyId": survey.ID}); err != nil || count != accounts {
		t.Fatalf("distinct users saved %d responses, want %d: %v", count, accounts, err)
	}
	for _, client := range clients {
		client.must(t, "POST", public+"/responses", integrationAnswers(false, map[string]any{"choices": []string{"b", "c"}}), 409)
	}
	stats := integrationDecode[map[string]any](t, clients[0].must(t, "GET", "/api/surveys/"+survey.ID+"/stats", nil, 200))
	if stats["totalResponses"] != float64(accounts) {
		t.Fatalf("totalResponses = %v", stats["totalResponses"])
	}
}

func TestSurveyDeleteArchiveRestoreAndReopen(t *testing.T) {
	fixture := support.NewMongo(t)
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	owner := newIntegrationClient(t, server.URL)
	respondent := newIntegrationClient(t, server.URL)
	other := newIntegrationClient(t, server.URL)
	for _, tc := range []struct {
		client integrationHTTP
		name   string
	}{{owner, "lifecycle_owner"}, {respondent, "lifecycle_respondent"}, {other, "lifecycle_other"}} {
		tc.client.must(t, "POST", "/api/register", map[string]string{
			"username": tc.name, "password": "test-password-123",
		}, 201)
	}

	// A draft may be archived, restored, and permanently deleted.
	draft := integrationDecode[domain.Survey](t, owner.must(t, "POST", "/api/surveys",
		domain.Survey{Title: "Temporary draft", Archived: true}, 201))
	if draft.Archived {
		t.Fatal("API callers must not be able to create a pre-archived survey")
	}
	private := "/api/surveys/" + draft.ID
	respondent.must(t, "DELETE", private, nil, 404)
	respondent.must(t, "POST", private+"/archive", nil, 404)
	archived := integrationDecode[domain.Survey](t, owner.must(t, "POST", private+"/archive", nil, 200))
	if !archived.Archived || archived.Status != "draft" || archived.Revision != draft.Revision+1 {
		t.Fatalf("archived draft changed unexpectedly: %#v", archived)
	}
	owner.must(t, "PUT", private, draft, 409)
	owner.must(t, "POST", private+"/publish", nil, 409)
	listed := integrationDecode[[]domain.Survey](t, owner.must(t, "GET", "/api/surveys", nil, 200))
	if len(listed) != 1 || !listed[0].Archived {
		t.Fatalf("archived survey must remain visible to its owner: %#v", listed)
	}
	restored := integrationDecode[domain.Survey](t, owner.must(t, "POST", private+"/restore", nil, 200))
	if restored.Archived || restored.Status != "draft" || restored.Revision != archived.Revision+1 {
		t.Fatalf("restoring draft: %#v", restored)
	}
	owner.must(t, "DELETE", private, nil, 200)
	owner.must(t, "GET", private, nil, 404)

	// Configured single-choice rules still use stable IDs, never display text.
	survey := integrationDecode[domain.Survey](t, owner.must(t, "POST", "/api/surveys",
		domain.Survey{Title: "Branch and lifecycle", AllowAnonymous: true, Questions: []domain.Question{
			{ID: "gender", Title: "男或女", Type: "single", Required: true,
				Options: []domain.Option{{ID: "male", Label: "男"}, {ID: "female", Label: "女"}},
				Rules: []domain.Rule{{Operator: "eq", Value: "male", Target: "details"},
					{Operator: "eq", Value: "female", Target: "age"}}},
			{ID: "details", Title: "后续必答题", Type: "text", Required: true},
			{ID: "age", Title: "年龄", Type: "number", Required: true,
				Validation: domain.Validation{Integer: true}},
		}}, 201))
	private = "/api/surveys/" + survey.ID
	public := "/api/public/surveys/" + survey.ID
	owner.must(t, "POST", private+"/publish", nil, 200)
	checkNext := func(value string, expected int, target string) {
		t.Helper()
		body := respondent.must(t, "POST", public+"/next", map[string]any{
			"questionId": "gender", "answer": value,
		}, expected)
		if expected == 200 {
			got := integrationDecode[map[string]any](t, body)
			if got["nextQuestionId"] != target {
				t.Fatalf("next(gender=%q)=%v, want %q", value, got, target)
			}
		}
	}
	checkNext("male", 200, "details")  // Same as the default next question.
	checkNext("female", 200, "age")   // Must skip required "details".
	checkNext("女", 400, "")
	respondent.must(t, "POST", public+"/responses", integrationAnswers(false,
		map[string]any{"gender": "female", "age": 18}), 201)
	owner.must(t, "DELETE", private, nil, 409) // No deletion of published data.

	// Closing is reversible; original answers and deduplication survive.
	owner.must(t, "POST", private+"/close", nil, 200)
	respondent.must(t, "POST", public+"/responses", integrationAnswers(false,
		map[string]any{"gender": "male", "details": "yes", "age": 18}), 409)
	reopened := integrationDecode[domain.Survey](t, owner.must(t, "POST",
		private+"/reopen", map[string]any{}, 200))
	if reopened.Status != "published" || reopened.Archived {
		t.Fatalf("reopened status: %#v", reopened)
	}
	respondent.must(t, "POST", public+"/responses", integrationAnswers(false,
		map[string]any{"gender": "female", "age": 22}), 409) // Repeat policy remains in effect.
	other.must(t, "POST", public+"/responses", integrationAnswers(false,
		map[string]any{"gender": "female", "age": 21}), 201)

	// Archiving an active survey closes it atomically, but preserves statistics.
	archived = integrationDecode[domain.Survey](t, owner.must(t, "POST", private+"/archive", nil, 200))
	if !archived.Archived || archived.Status != "closed" {
		t.Fatalf("published archive must be closed: %#v", archived)
	}
	respondent.must(t, "GET", public, nil, 404)
	owner.must(t, "POST", private+"/reopen", map[string]any{}, 409)
	var stats struct {
		Total int `json:"totalResponses"`
	}
	if err := json.Unmarshal(owner.must(t, "GET", private+"/stats", nil, 200), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 {
		t.Fatalf("archiving deleted submitted answers: %d", stats.Total)
	}
	restored = integrationDecode[domain.Survey](t, owner.must(t, "POST", private+"/restore", nil, 200))
	if restored.Archived || restored.Status != "closed" {
		t.Fatalf("restoration must not silently reopen the survey: %#v", restored)
	}
	owner.must(t, "POST", private+"/reopen", map[string]any{}, 200)

	// An expired deadline cannot be reused; the owner may replace or clear it.
	owner.must(t, "POST", private+"/close", nil, 200)
	past := time.Now().Add(-time.Hour)
	if _, err := fixture.DB.Collection("surveys").UpdateOne(fixture.Context,
		bson.M{"_id": survey.ID}, bson.M{"$set": bson.M{"deadline": past}}); err != nil {
		t.Fatal(err)
	}
	owner.must(t, "POST", private+"/reopen", map[string]any{}, 400)
	owner.must(t, "POST", private+"/reopen", map[string]any{"deadline": "invalid"}, 400)
	future := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	reopened = integrationDecode[domain.Survey](t, owner.must(t, "POST", private+"/reopen",
		map[string]any{"deadline": future}, 200))
	if reopened.Deadline == nil || !reopened.Deadline.Equal(future) {
		t.Fatalf("new deadline not applied: %#v", reopened.Deadline)
	}
	owner.must(t, "POST", private+"/close", nil, 200)
	reopened = integrationDecode[domain.Survey](t, owner.must(t, "POST", private+"/reopen",
		map[string]any{"deadline": nil}, 200))
	if reopened.Deadline != nil {
		t.Fatal("explicit null must clear the deadline")
	}
	respondent.must(t, "GET", public, nil, 200)
}
