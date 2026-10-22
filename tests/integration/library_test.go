package integration_test

import (
	"errors"
	"fmt"
	"math"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"questionnaire/internal/domain"
	"questionnaire/internal/store"
	"questionnaire/internal/web"
	"questionnaire/tests/support"
)

// User flows use isolated databases and the public HTTP contract. Recovery
// scenarios explicitly seed interrupted writes to exercise startup repair.
func libraryClients(t *testing.T) (integrationHTTP, integrationHTTP, integrationHTTP) {
	t.Helper()
	fixture := support.NewMongo(t)
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	clients := []integrationHTTP{newIntegrationClient(t, server.URL), newIntegrationClient(t, server.URL), newIntegrationClient(t, server.URL)}
	for i, name := range []string{"library_owner", "library_reader", "library_outsider"} {
		clients[i].must(t, "POST", "/api/register", map[string]string{"username": name, "password": "test-password-123"}, 201)
	}
	return clients[0], clients[1], clients[2]
}

func libraryCreate(t *testing.T, c integrationHTTP, q domain.Question) domain.QuestionTemplate {
	t.Helper()
	result := integrationDecode[domain.QuestionTemplate](t, c.must(t, "POST", "/api/questions", map[string]any{"question": q, "note": "initial"}, 201))
	if result.ID == "" || result.LatestVersion == nil || result.LatestVersion.ID == "" || result.LatestVersion.Version != 1 || result.LatestVersion.QuestionID != result.ID {
		t.Fatalf("invalid initial template: %#v", result)
	}
	return result
}

func libraryOccurrence(v domain.QuestionVersion, id string) domain.Question {
	q := v.Question
	q.ID = id
	q.SourceQuestionID = v.QuestionID
	q.SourceVersionID = v.ID
	return q
}

func librarySurvey(t *testing.T, c integrationHTTP, title string, questions ...domain.Question) domain.Survey {
	t.Helper()
	return integrationDecode[domain.Survey](t, c.must(t, "POST", "/api/surveys", domain.Survey{Title: title, AllowAnonymous: true, AllowRepeat: true, Questions: questions}, 201))
}

func libraryVersion(t *testing.T, c integrationHTTP, family string, base domain.QuestionVersion, q domain.Question) domain.QuestionVersion {
	t.Helper()
	return integrationDecode[domain.QuestionVersion](t, c.must(t, "POST", "/api/questions/"+family+"/versions", map[string]any{"baseVersionId": base.ID, "question": q, "note": "edit"}, 201))
}

func TestLibraryReuseImmutableHistoryAndRestore(t *testing.T) {
	owner, reader, outsider := libraryClients(t)
	template := libraryCreate(t, owner, domain.Question{Title: "Original", Type: "single", Required: true, Options: []domain.Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}})
	v1 := *template.LatestVersion
	path := "/api/questions/" + template.ID
	first := libraryOccurrence(v1, "local_a")
	first.Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: domain.End}}
	survey := librarySurvey(t, owner, "Frozen published", first, domain.Question{ID: "follow", Title: "Follow-up", Type: "fill", FillType: "text"})
	owner.must(t, "POST", "/api/surveys/"+survey.ID+"/publish", nil, 200)
	second := librarySurvey(t, owner, "Independent local rules", libraryOccurrence(v1, "local_b"))
	reader.must(t, "POST", "/api/public/surveys/"+survey.ID+"/responses", integrationAnswers(false, map[string]any{"local_a": "a"}), 201)
	edited := v1.Question
	edited.Title = "Revised"
	edited.Options = []domain.Option{{ID: "a", Label: "Different meaning"}, {ID: "c", Label: "C"}}
	v2 := libraryVersion(t, owner, template.ID, v1, edited)
	branchContent := v1.Question
	branchContent.Title = "Branch from first"
	v3 := libraryVersion(t, owner, template.ID, v1, branchContent)
	restored := integrationDecode[domain.QuestionVersion](t, owner.must(t, "POST", path+"/restore", map[string]any{"versionId": v1.ID, "note": "restore first"}, 201))
	if v2.Version != 2 || v3.Version != 3 || restored.Version != 4 || v2.ParentVersionID != v1.ID || v3.ParentVersionID != v1.ID || restored.ParentVersionID != v3.ID || restored.RestoredFromVersionID != v1.ID || !domain.QuestionContentEqual(restored.Question, v1.Question) {
		t.Fatalf("version graph/restore invalid: v2=%#v v3=%#v restored=%#v", v2, v3, restored)
	}
	history := integrationDecode[[]domain.QuestionVersion](t, owner.must(t, "GET", path+"/versions", nil, 200))
	if len(history) != 4 {
		t.Fatalf("history length=%d", len(history))
	}
	fetched := integrationDecode[domain.QuestionVersion](t, owner.must(t, "GET", "/api/question-versions/"+v1.ID, nil, 200))
	if fetched.ID != v1.ID || fetched.QuestionID != v1.QuestionID || fetched.Version != 1 || fetched.Note != v1.Note || fetched.ParentVersionID != "" || fetched.RestoredFromVersionID != "" || !reflect.DeepEqual(fetched.Question, v1.Question) || fetched.CreatedAt.UnixMilli() != v1.CreatedAt.UnixMilli() {
		t.Fatalf("old version mutated: %#v", fetched)
	}
	for _, s := range []domain.Survey{survey, second} {
		persisted := integrationDecode[domain.Survey](t, owner.must(t, "GET", "/api/surveys/"+s.ID, nil, 200))
		if !reflect.DeepEqual(persisted.Questions, s.Questions) {
			t.Fatalf("survey snapshot changed with template: %#v", persisted.Questions)
		}
	}
	current := integrationDecode[domain.QuestionTemplate](t, owner.must(t, "GET", path, nil, 200))
	if current.LatestVersion == nil || current.LatestVersion.ID != restored.ID {
		t.Fatalf("latest version: %#v", current)
	}
	outsider.must(t, "GET", path, nil, 404)
	outsider.must(t, "GET", "/api/question-versions/"+v1.ID, nil, 404)
	owner.must(t, "PUT", "/api/question-versions/"+v1.ID, map[string]any{"question": edited}, 404)
	afterRejectedWrite := integrationDecode[domain.QuestionVersion](t, owner.must(t, "GET", "/api/question-versions/"+v1.ID, nil, 200))
	if !reflect.DeepEqual(afterRejectedWrite, fetched) {
		t.Fatal("rejected direct write changed immutable version")
	}
}

func TestLibrarySharingRevocationAndSnapshotIntegrity(t *testing.T) {
	owner, reader, outsider := libraryClients(t)
	template := libraryCreate(t, owner, domain.Question{Title: "Shared number", Type: "fill", FillType: "number", Required: true, Validation: domain.Validation{Min: integrationFloat(0), Max: integrationFloat(100)}})
	path := "/api/questions/" + template.ID
	v1 := *template.LatestVersion
	owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{"library_reader"}, "sharingRevision": 0}, 200)
	reader.must(t, "GET", path, nil, 200)
	listed := integrationDecode[[]domain.QuestionTemplate](t, reader.must(t, "GET", "/api/questions", nil, 200))
	if len(listed) != 1 || listed[0].ID != template.ID {
		t.Fatalf("shared listing: %#v", listed)
	}
	if got := integrationDecode[[]domain.QuestionTemplate](t, outsider.must(t, "GET", "/api/questions", nil, 200)); len(got) != 0 {
		t.Fatalf("private listing leaked: %#v", got)
	}
	for _, op := range []struct {
		method, suffix string
		body           any
	}{
		{"POST", "/versions", map[string]any{"baseVersionId": v1.ID, "question": v1.Question}},
		{"POST", "/restore", map[string]any{"versionId": v1.ID}},
		{"PUT", "/sharing", map[string]any{"usernames": []string{"library_outsider"}}},
	} {
		reader.must(t, op.method, path+op.suffix, op.body, 404)
	}
	shared := libraryOccurrence(v1, "shared")
	draft := librarySurvey(t, reader, "Recipient draft", shared)
	foreign := libraryCreate(t, owner, domain.Question{Title: "Another family", Type: "fill", FillType: "text"})
	for _, tc := range []struct {
		name string
		edit func(*domain.Question)
	}{
		{"title", func(q *domain.Question) { q.Title = "forged" }},
		{"required", func(q *domain.Question) { q.Required = false }},
		{"bounds", func(q *domain.Question) { q.Validation.Min = integrationFloat(-10) }},
		{"missing family", func(q *domain.Question) { q.SourceQuestionID = "" }},
		{"missing version", func(q *domain.Question) { q.SourceVersionID = "" }},
		{"foreign family", func(q *domain.Question) { q.SourceQuestionID = foreign.ID }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := shared
			tc.edit(&q)
			owner.must(t, "POST", "/api/surveys", domain.Survey{Title: tc.name, Questions: []domain.Question{q}}, 400)
		})
	}
	outsider.must(t, "POST", "/api/surveys", domain.Survey{Title: "Unauthorized reuse", Questions: []domain.Question{shared}}, 404)
	owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{}, "sharingRevision": 1}, 200)
	reader.must(t, "GET", path, nil, 404)
	reader.must(t, "GET", "/api/question-versions/"+v1.ID, nil, 404)
	// A saved occurrence remains usable after access is revoked; new copies do not.
	draft.Title = "Still editable"
	draft.Questions[0].Rules = []domain.Rule{{Operator: "gte", Value: 50, Target: domain.End}}
	saved := integrationDecode[domain.Survey](t, reader.must(t, "PUT", "/api/surveys/"+draft.ID, draft, 200))
	forged := saved
	forged.Questions = append([]domain.Question(nil), saved.Questions...)
	forged.Questions[0].Required = false
	reader.must(t, "PUT", "/api/surveys/"+saved.ID, forged, 400)
	copied := saved
	copied.Questions = append(append([]domain.Question(nil), saved.Questions...), libraryOccurrence(v1, "new_copy"))
	reader.must(t, "PUT", "/api/surveys/"+saved.ID, copied, 404)
	reader.must(t, "POST", "/api/surveys", domain.Survey{Title: "New copy after revoke", Questions: []domain.Question{shared}}, 404)
	reader.must(t, "POST", "/api/surveys/"+draft.ID+"/publish", nil, 200)
	outsider.must(t, "POST", "/api/public/surveys/"+draft.ID+"/responses", integrationAnswers(false, map[string]any{"shared": 50}), 201)
}

func TestLibraryQuestionBankCRUDPinsAndConflicts(t *testing.T) {
	owner, reader, _ := libraryClients(t)
	template := libraryCreate(t, owner, domain.Question{Title: "Pinned", Type: "fill", FillType: "text"})
	v1 := *template.LatestVersion
	bank := integrationDecode[domain.QuestionBank](t, owner.must(t, "POST", "/api/question-banks", domain.QuestionBank{Name: "Research", Description: "Stable versions", VersionIDs: []string{v1.ID}}, 201))
	path := "/api/question-banks/" + bank.ID
	if bank.ID == "" || bank.Revision < 1 || !reflect.DeepEqual(bank.VersionIDs, []string{v1.ID}) {
		t.Fatalf("bank: %#v", bank)
	}
	content := v1.Question
	content.Title = "Second"
	v2 := libraryVersion(t, owner, template.ID, v1, content)
	unchanged := integrationDecode[domain.QuestionBank](t, owner.must(t, "GET", path, nil, 200))
	if !reflect.DeepEqual(unchanged.VersionIDs, []string{v1.ID}) {
		t.Fatal("new version silently changed bank pin")
	}
	reader.must(t, "GET", path, nil, 404)
	reader.must(t, "PUT", path, bank, 404)
	reader.must(t, "DELETE", path+"?revision="+fmt.Sprint(bank.Revision), nil, 404)
	owner.must(t, "POST", "/api/question-banks", domain.QuestionBank{Name: "Duplicate", VersionIDs: []string{v1.ID, v1.ID}}, 400)
	owner.must(t, "POST", "/api/question-banks", domain.QuestionBank{Name: " "}, 400)
	reader.must(t, "POST", "/api/question-banks", domain.QuestionBank{Name: "Private pin", VersionIDs: []string{v1.ID}}, 404)
	owner.must(t, "PUT", path, domain.QuestionBank{Name: "No revision", VersionIDs: []string{v1.ID}}, 400)
	bank.Name = "Updated"
	bank.VersionIDs = []string{v1.ID, v2.ID}
	updated := integrationDecode[domain.QuestionBank](t, owner.must(t, "PUT", path, bank, 200))
	if updated.Revision != bank.Revision+1 || updated.Name != "Updated" || len(updated.VersionIDs) != 2 {
		t.Fatalf("bank update: %#v", updated)
	}
	owner.must(t, "PUT", path, bank, 409)
	owner.must(t, "DELETE", path+"?revision="+fmt.Sprint(bank.Revision), nil, 409)
	banks := integrationDecode[[]domain.QuestionBank](t, owner.must(t, "GET", "/api/question-banks", nil, 200))
	if len(banks) != 1 || banks[0].ID != bank.ID {
		t.Fatalf("bank listing: %#v", banks)
	}
	owner.must(t, "DELETE", path+"?revision="+fmt.Sprint(updated.Revision), nil, 200)
	owner.must(t, "GET", path, nil, 404)
	owner.must(t, "GET", "/api/question-versions/"+v1.ID, nil, 200)
}

type libraryStatsResult struct {
	QuestionID   string `json:"questionId"`
	TotalAnswers int    `json:"totalAnswers"`
	SurveyCount  int    `json:"surveyCount"`
	Versions     []struct {
		VersionID   string   `json:"versionId"`
		Version     int      `json:"version"`
		Answered    int      `json:"answered"`
		SurveyCount int      `json:"surveyCount"`
		Average     *float64 `json:"average"`
		Options     []struct {
			ID    string `json:"id"`
			Count int    `json:"count"`
		} `json:"options"`
		NumericDistribution []struct {
			Min, Max float64
			Count    int
		} `json:"numericDistribution"`
		Values       []any `json:"values"`
		ValuesOffset int   `json:"valuesOffset"`
		ValuesLimit  int   `json:"valuesLimit"`
		HasMore      bool  `json:"hasMore"`
	} `json:"versions"`
}

func TestLibraryUsagePrivacyAndWeightedNumericStats(t *testing.T) {
	owner, reader, outsider := libraryClients(t)
	template := libraryCreate(t, owner, domain.Question{Title: "Score", Type: "fill", FillType: "number", Required: true})
	v1 := *template.LatestVersion
	path := "/api/questions/" + template.ID
	owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{"library_reader"}, "sharingRevision": 0}, 200)
	one := librarySurvey(t, owner, "One response", libraryOccurrence(v1, "score"))
	two := librarySurvey(t, owner, "Two occurrences", libraryOccurrence(v1, "first"), libraryOccurrence(v1, "second"))
	foreign := librarySurvey(t, reader, "Private respondent survey", libraryOccurrence(v1, "hidden"))
	for _, entry := range []struct {
		c integrationHTTP
		s domain.Survey
	}{{owner, one}, {owner, two}, {reader, foreign}} {
		entry.c.must(t, "POST", "/api/surveys/"+entry.s.ID+"/publish", nil, 200)
	}
	outsider.must(t, "POST", "/api/public/surveys/"+one.ID+"/responses", integrationAnswers(false, map[string]any{"score": 100}), 201)
	for _, n := range []int{10, 20, 30} {
		outsider.must(t, "POST", "/api/public/surveys/"+two.ID+"/responses", integrationAnswers(false, map[string]any{"first": n, "second": n + 1}), 201)
	}
	outsider.must(t, "POST", "/api/public/surveys/"+foreign.ID+"/responses", integrationAnswers(false, map[string]any{"hidden": 999}), 201)
	type usageResult struct {
		Surveys []struct {
			SurveyID   string `json:"surveyId"`
			Title      string `json:"title"`
			QuestionID string `json:"questionId"`
			VersionID  string `json:"versionId"`
		} `json:"surveys"`
		OwnSurveyCount       int `json:"ownSurveyCount"`
		TotalSurveyCount     int `json:"totalSurveyCount"`
		TotalOccurrenceCount int `json:"totalOccurrenceCount"`
	}
	usage := integrationDecode[usageResult](t, owner.must(t, "GET", path+"/usage", nil, 200))
	if usage.OwnSurveyCount != 2 || usage.TotalSurveyCount != 3 || usage.TotalOccurrenceCount != 4 || len(usage.Surveys) != 3 {
		t.Fatalf("usage counts: %#v", usage)
	}
	for _, s := range usage.Surveys {
		if s.SurveyID == foreign.ID || s.Title == foreign.Title || s.VersionID != v1.ID {
			t.Fatalf("usage disclosed foreign survey or wrong reference: %#v", s)
		}
	}
	recipientUsage := integrationDecode[usageResult](t, reader.must(t, "GET", path+"/usage", nil, 200))
	if len(recipientUsage.Surveys) != 1 || recipientUsage.Surveys[0].SurveyID != foreign.ID || recipientUsage.OwnSurveyCount != 1 || recipientUsage.TotalSurveyCount > 1 || recipientUsage.TotalOccurrenceCount > 1 {
		t.Fatalf("recipient global usage leak: %#v", recipientUsage)
	}
	stats := integrationDecode[libraryStatsResult](t, owner.must(t, "GET", path+"/stats", nil, 200))
	if stats.QuestionID != template.ID || stats.TotalAnswers != 7 || stats.SurveyCount != 2 || len(stats.Versions) != 1 {
		t.Fatalf("aggregate scope/count: %#v", stats)
	}
	version := stats.Versions[0]
	if version.Answered != 7 || version.SurveyCount != 2 || version.Average == nil || math.Abs(*version.Average-223.0/7) > 1e-9 || len(version.Values) != 0 {
		t.Fatalf("weighted aggregate: %#v", version)
	}
	count := 0
	for _, bin := range version.NumericDistribution {
		if bin.Count < 0 || bin.Min > bin.Max || bin.Min < 10 || bin.Max > 100 {
			t.Fatalf("numeric distribution invalid/leaked: %#v", bin)
		}
		count += bin.Count
	}
	if len(version.NumericDistribution) == 0 || count != 7 {
		t.Fatalf("distribution lost answers: %#v", version.NumericDistribution)
	}
	own := integrationDecode[libraryStatsResult](t, reader.must(t, "GET", path+"/stats", nil, 200))
	if own.TotalAnswers != 1 || own.SurveyCount != 1 || len(own.Versions) != 1 || own.Versions[0].Average == nil || *own.Versions[0].Average != 999 {
		t.Fatalf("recipient stats scope: %#v", own)
	}
	outsider.must(t, "GET", path+"/usage", nil, 404)
	outsider.must(t, "GET", path+"/stats", nil, 404)
}

func TestLibraryStatsVersionSeparationAndRawPagination(t *testing.T) {
	owner, reader, _ := libraryClients(t)
	template := libraryCreate(t, owner, domain.Question{Title: "Text", Type: "fill", FillType: "text", Required: true})
	v1 := *template.LatestVersion
	path := "/api/questions/" + template.ID
	textSurvey := librarySurvey(t, owner, "Long answers", libraryOccurrence(v1, "text"))
	owner.must(t, "POST", "/api/surveys/"+textSurvey.ID+"/publish", nil, 200)
	expected := map[string]bool{}
	for i := 0; i < 22; i++ {
		value := fmt.Sprintf("%02d-%s", i, strings.Repeat("答", 200))
		expected[value] = true
		reader.must(t, "POST", "/api/public/surveys/"+textSurvey.ID+"/responses", integrationAnswers(false, map[string]any{"text": value}), 201)
	}
	v2 := libraryVersion(t, owner, template.ID, v1, domain.Question{Title: "Choice", Type: "single", Required: true, Options: []domain.Option{{ID: "a", Label: "Old A"}, {ID: "b", Label: "B"}}})
	q3 := v2.Question
	q3.Options = []domain.Option{{ID: "a", Label: "New A"}, {ID: "c", Label: "C"}}
	v3 := libraryVersion(t, owner, template.ID, v2, q3)
	for i, v := range []domain.QuestionVersion{v2, v3} {
		s := librarySurvey(t, owner, fmt.Sprintf("Version %d", i+2), libraryOccurrence(v, "choice"))
		owner.must(t, "POST", "/api/surveys/"+s.ID+"/publish", nil, 200)
		for j := 0; j < i+1; j++ {
			reader.must(t, "POST", "/api/public/surveys/"+s.ID+"/responses", integrationAnswers(false, map[string]any{"choice": "a"}), 201)
		}
	}
	stats := integrationDecode[libraryStatsResult](t, owner.must(t, "GET", path+"/stats", nil, 200))
	if stats.TotalAnswers != 25 || stats.SurveyCount != 3 || len(stats.Versions) != 3 {
		t.Fatalf("version aggregation: %#v", stats)
	}
	for _, v := range stats.Versions {
		want := map[string]int{v1.ID: 22, v2.ID: 1, v3.ID: 2}[v.VersionID]
		if want == 0 || v.Answered != want || len(v.Values) != 0 {
			t.Fatalf("mixed version answers: %#v", v)
		}
		if v.VersionID != v1.ID {
			if len(v.Options) != 2 || v.Options[0].ID != "a" || v.Options[0].Count != want {
				t.Fatalf("mixed option counts: %#v", v)
			}
		}
	}
	seen := map[string]bool{}
	for _, offset := range []int{0, 20, 22} {
		result := integrationDecode[libraryStatsResult](t, owner.must(t, "GET", path+"/stats?versionId="+v1.ID+"&offset="+fmt.Sprint(offset), nil, 200))
		if len(result.Versions) != 1 {
			t.Fatalf("version filter: %#v", result)
		}
		v := result.Versions[0]
		want := map[int]int{0: 20, 20: 2, 22: 0}[offset]
		if v.VersionID != v1.ID || v.Answered != 22 || v.ValuesOffset != offset || v.ValuesLimit != 20 || len(v.Values) != want || v.HasMore != (offset == 0) {
			t.Fatalf("raw answer pagination: %#v", v)
		}
		for _, raw := range v.Values {
			value, ok := raw.(string)
			if !ok || !expected[value] || seen[value] {
				t.Fatalf("missing, foreign, or duplicate raw value: %v", raw)
			}
			seen[value] = true
		}
	}
	if len(seen) != 22 {
		t.Fatalf("page traversal returned %d answers", len(seen))
	}
	foreign := libraryCreate(t, owner, domain.Question{Title: "Foreign", Type: "fill", FillType: "text"})
	owner.must(t, "GET", path+"/stats?versionId="+foreign.LatestVersion.ID, nil, 404)
	owner.must(t, "POST", path+"/versions", map[string]any{"baseVersionId": foreign.LatestVersion.ID, "question": v1.Question}, 400)
	owner.must(t, "POST", path+"/restore", map[string]any{"versionId": foreign.LatestVersion.ID}, 400)
	for _, query := range []string{"offset=-1", "offset=bad", "offset=0&offset=1", "limit=0", "limit=51", "limit=1&limit=2", "versionId=" + v1.ID + "&versionId=" + v2.ID} {
		owner.must(t, "GET", path+"/stats?"+query, nil, 400)
	}
}

func TestLibraryConcurrentVersionAllocation(t *testing.T) {
	owner, _, _ := libraryClients(t)
	template := libraryCreate(t, owner, domain.Question{Title: "Concurrent", Type: "fill", FillType: "text"})
	v1 := *template.LatestVersion
	const count = 8
	type outcome struct {
		status int
		body   []byte
		err    error
	}
	results := make(chan outcome, count)
	start := make(chan struct{})
	for i := 0; i < count; i++ {
		go func(i int) {
			<-start
			q := v1.Question
			q.Title = fmt.Sprintf("Branch %d", i)
			status, body, err := owner.send("POST", "/api/questions/"+template.ID+"/versions", map[string]any{"baseVersionId": v1.ID, "question": q})
			results <- outcome{status, body, err}
		}(i)
	}
	close(start)
	ids := map[string]bool{v1.ID: true}
	numbers := map[int]bool{1: true}
	for i := 0; i < count; i++ {
		r := <-results
		if r.err != nil || r.status != 201 {
			t.Errorf("concurrent version: status=%d body=%s err=%v", r.status, r.body, r.err)
			continue
		}
		v := integrationDecode[domain.QuestionVersion](t, r.body)
		if v.ID == "" || ids[v.ID] || numbers[v.Version] || v.ParentVersionID != v1.ID || v.QuestionID != template.ID || v.Version < 2 {
			t.Errorf("duplicate or wrong parent: %#v", v)
		}
		ids[v.ID] = true
		numbers[v.Version] = true
	}
	history := integrationDecode[[]domain.QuestionVersion](t, owner.must(t, "GET", "/api/questions/"+template.ID+"/versions", nil, 200))
	if len(history) != count+1 || len(ids) != count+1 || len(numbers) != count+1 {
		t.Fatalf("concurrent history lost versions: history=%d ids=%d versions=%d", len(history), len(ids), len(numbers))
	}
	for _, v := range history {
		if !ids[v.ID] || !numbers[v.Version] {
			t.Errorf("unexpected history entry: %#v", v)
		}
	}
	latest := integrationDecode[domain.QuestionTemplate](t, owner.must(t, "GET", "/api/questions/"+template.ID, nil, 200))
	if latest.LatestVersion == nil || latest.LatestVersion.Version != count+1 {
		t.Fatalf("concurrent latest version: %#v", latest)
	}
}

// The aggregate input exceeds MongoDB's 16 MiB document limit. A lookup that
// materializes every answer in one array fails even when the HTTP page is tiny.
func TestLibraryStatsLargeTextDatasetRemainsPaged(t *testing.T) {
	fixture := support.NewMongo(t)
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	owner := newIntegrationClient(t, server.URL)
	user := integrationDecode[domain.User](t, owner.must(t, "POST", "/api/register", map[string]string{"username": "large_library_owner", "password": "test-password-123"}, 201))
	template := libraryCreate(t, owner, domain.Question{Title: "Long text", Type: "fill", FillType: "text", Required: true})
	v1 := *template.LatestVersion
	survey := librarySurvey(t, owner, "Large aggregate", libraryOccurrence(v1, "long_text"))
	owner.must(t, "POST", "/api/surveys/"+survey.ID+"/publish", nil, 200)
	const count = 600
	value := strings.Repeat("答", 10000)
	docs := make([]any, count)
	for i := range docs {
		docs[i] = domain.Response{SchemaVersion: 1, ID: fmt.Sprintf("large_answer_%04d", i), SurveyID: survey.ID, UserID: user.ID, Answers: map[string]any{"long_text": value}, Path: []string{"long_text"}, SubmittedAt: time.Now().UTC().Add(time.Duration(i) * time.Millisecond)}
	}
	if _, err := fixture.DB.Collection("responses").InsertMany(fixture.Context, docs); err != nil {
		t.Fatal(err)
	}
	path := "/api/questions/" + template.ID + "/stats"
	summary := integrationDecode[libraryStatsResult](t, owner.must(t, "GET", path, nil, 200))
	if summary.TotalAnswers != count || len(summary.Versions) != 1 || summary.Versions[0].Answered != count || len(summary.Versions[0].Values) != 0 {
		t.Fatalf("large summary: %#v", summary)
	}
	page := integrationDecode[libraryStatsResult](t, owner.must(t, "GET", path+"?versionId="+v1.ID, nil, 200))
	if len(page.Versions) != 1 {
		t.Fatalf("large page versions=%d", len(page.Versions))
	}
	v := page.Versions[0]
	if v.Answered != count || len(v.Values) != 20 || v.ValuesLimit != 20 || !v.HasMore {
		t.Fatalf("large pagination: answered=%d values=%d limit=%d more=%v", v.Answered, len(v.Values), v.ValuesLimit, v.HasMore)
	}
	for _, got := range v.Values {
		if got != value {
			t.Fatal("large raw answer was truncated or changed")
		}
	}
}

// Reproduce a crash between initial-version insertion and publication of the
// family. Init may publish complete data, but must retain incomplete documents.
func TestLibraryInitRecoversOnlyCompletePendingFamilies(t *testing.T) {
	fixture := support.NewMongo(t)
	repo, ctx := fixture.Store, fixture.Context
	now := time.Now().UTC().Truncate(time.Millisecond)
	const owner = "recovery_owner"
	complete := domain.QuestionTemplate{ID: "pending_complete", OwnerID: owner, SharedWith: []string{}, NextVersion: 1, Ready: false, CreatedAt: now}
	incomplete := domain.QuestionTemplate{ID: "pending_incomplete", OwnerID: owner, SharedWith: []string{}, NextVersion: 2, Ready: false, CreatedAt: now}
	empty := domain.QuestionTemplate{ID: "pending_empty", OwnerID: owner, SharedWith: []string{}, NextVersion: 1, Ready: false, CreatedAt: now}
	for _, family := range []domain.QuestionTemplate{complete, incomplete, empty} {
		if _, err := fixture.DB.Collection("question_templates").InsertOne(ctx, family); err != nil {
			t.Fatal(err)
		}
	}
	first := domain.QuestionVersion{ID: "recovery_v1", QuestionID: complete.ID, Version: 1, Question: domain.Question{ID: "template", Title: "Recoverable first", Type: "fill", FillType: "text"}, Note: "preserved", CreatedAt: now}
	// Having some version is not enough: publication requires the initial one.
	orphan := domain.QuestionVersion{ID: "orphan_v2", QuestionID: incomplete.ID, Version: 2, Question: domain.Question{ID: "template", Title: "Retain orphaned data", Type: "fill", FillType: "text"}, Note: "do not delete", CreatedAt: now}
	for _, version := range []domain.QuestionVersion{first, orphan} {
		if _, err := fixture.DB.Collection("question_versions").InsertOne(ctx, version); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{complete.ID, incomplete.ID, empty.ID} {
		if _, err := repo.QuestionTemplate(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("pending %s visible before recovery: %v", id, err)
		}
	}
	if listed, err := repo.ListQuestionTemplates(ctx, owner); err != nil || len(listed) != 0 {
		t.Fatalf("pending listing before recovery=%#v err=%v", listed, err)
	}
	// Repeated startup must be idempotent and may not discard partial records.
	for attempt := 0; attempt < 2; attempt++ {
		if err := repo.Init(ctx); err != nil {
			t.Fatalf("Init attempt %d: %v", attempt, err)
		}
		recovered, err := repo.QuestionTemplate(ctx, complete.ID)
		if err != nil || recovered.LatestVersion == nil || !recovered.Ready || !reflect.DeepEqual(*recovered.LatestVersion, first) {
			t.Fatalf("complete family not recovered intact: %#v err=%v", recovered, err)
		}
		listed, err := repo.ListQuestionTemplates(ctx, owner)
		if err != nil || len(listed) != 1 || listed[0].ID != complete.ID {
			t.Fatalf("recovered listing=%#v err=%v", listed, err)
		}
		for _, family := range []domain.QuestionTemplate{incomplete, empty} {
			if _, err := repo.QuestionTemplate(ctx, family.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("family without v1 exposed: id=%s err=%v", family.ID, err)
			}
			var stored domain.QuestionTemplate
			if err := fixture.DB.Collection("question_templates").FindOne(ctx, bson.M{"_id": family.ID}).Decode(&stored); err != nil || !reflect.DeepEqual(stored, family) {
				t.Fatalf("partial family removed or rewritten: %#v err=%v", stored, err)
			}
		}
		persisted, err := repo.QuestionVersion(ctx, orphan.ID)
		if err != nil || !reflect.DeepEqual(*persisted, orphan) {
			t.Fatalf("incomplete version removed or rewritten: %#v err=%v", persisted, err)
		}
	}
}

func TestLibraryVersionAllocationGapsKeepLatestReadable(t *testing.T) {
	fixture := support.NewMongo(t)
	repo, ctx := fixture.Store, fixture.Context
	now := time.Now().UTC().Truncate(time.Millisecond)
	family := domain.QuestionTemplate{ID: "gap_family", OwnerID: "gap_owner", SharedWith: []string{}, CreatedAt: now}
	first := domain.QuestionVersion{ID: "gap_v1", Question: domain.Question{ID: "template", Title: "Original", Type: "fill", FillType: "text"}, Note: "initial", CreatedAt: now}
	if err := repo.CreateQuestionTemplate(ctx, &family, &first); err != nil {
		t.Fatal(err)
	}
	second := domain.QuestionVersion{ID: "gap_v2", ParentVersionID: first.ID, Question: first.Question, Note: "second", CreatedAt: now}
	second.Question.Title = "Latest committed"
	if err := repo.CreateQuestionVersion(ctx, family.ID, family.OwnerID, &second); err != nil {
		t.Fatal(err)
	}
	// Simulate allocated numbers whose writes failed before version insertion.
	if _, err := fixture.DB.Collection("question_templates").UpdateOne(ctx, bson.M{"_id": family.ID}, bson.M{"$set": bson.M{"nextVersion": 7}}); err != nil {
		t.Fatal(err)
	}
	checkLatest := func(expected domain.QuestionVersion) {
		t.Helper()
		got, err := repo.QuestionTemplate(ctx, family.ID)
		if err != nil || got.LatestVersion == nil || !reflect.DeepEqual(*got.LatestVersion, expected) {
			t.Fatalf("latest lookup used allocated counter: %#v err=%v", got, err)
		}
		listed, err := repo.ListQuestionTemplates(ctx, family.OwnerID)
		if err != nil || len(listed) != 1 || listed[0].LatestVersion == nil || !reflect.DeepEqual(*listed[0].LatestVersion, expected) {
			t.Fatalf("listing lost latest after gap: %#v err=%v", listed, err)
		}
	}
	checkLatest(second)
	next := domain.QuestionVersion{ID: "gap_v8", ParentVersionID: second.ID, Question: second.Question, Note: "after failed writes", CreatedAt: now}
	next.Question.Title = "Next committed"
	if err := repo.CreateQuestionVersion(ctx, family.ID, family.OwnerID, &next); err != nil {
		t.Fatal(err)
	}
	if next.Version != 8 || next.ParentVersionID != second.ID {
		t.Fatalf("allocation reused gap or lost parent: %#v", next)
	}
	checkLatest(next)
	history, err := repo.QuestionVersions(ctx, family.ID)
	if err != nil || len(history) != 3 {
		t.Fatalf("history after gap: %#v err=%v", history, err)
	}
	expected := []domain.QuestionVersion{first, second, next}
	if !reflect.DeepEqual(history, expected) {
		t.Fatalf("history mutated or fabricated gap versions: got=%#v want=%#v", history, expected)
	}
}
