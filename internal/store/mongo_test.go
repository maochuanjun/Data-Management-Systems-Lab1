package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"questionnaire/internal/domain"
)

func statsTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	uri := os.Getenv("INTEGRATION_MONGO_URI")
	if uri == "" {
		t.Skip("set INTEGRATION_MONGO_URI to run MongoDB regression tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	repo, err := New(ctx, uri, "questionnaire_test_stats_"+rand.Text())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if err := repo.db.Drop(cleanup); err != nil {
			t.Error(err)
		}
		if err := repo.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	return repo, ctx
}

func TestStatsLargeTextResponses(t *testing.T) {
	repo, ctx := statsTestStore(t)
	survey := &domain.Survey{ID: "capacity", Title: "capacity", Questions: []domain.Question{{ID: "text", Title: "text", Type: "text", Required: true}}}
	value := strings.Repeat("中", 10000)
	answers, path, err := domain.ValidateSubmission(survey, map[string]any{"text": value})
	if err != nil {
		t.Fatal(err)
	}
	documents := make([]any, 600)
	for i := range documents {
		documents[i] = &domain.Response{SchemaVersion: 1, ID: fmt.Sprintf("%04d", i), SurveyID: survey.ID, Anonymous: true, Answers: answers, Path: path, SubmittedAt: time.Unix(0, 0)}
	}
	if _, err := repo.db.Collection("responses").InsertMany(ctx, documents); err != nil {
		t.Fatal(err)
	}
	stats, err := repo.Stats(ctx, survey, 0, 50)
	if err != nil {
		t.Fatalf("statistics must support 600 valid long-text responses: %v", err)
	}
	if stats["totalResponses"] != int64(600) {
		t.Fatalf("totalResponses = %v, want 600", stats["totalResponses"])
	}
	q := stats["questions"].([]map[string]any)[0]
	if q["answered"] != int64(600) || q["hasMore"] != true || len(q["values"].([]any)) != 50 {
		t.Fatalf("incorrect large-response page: answered=%v hasMore=%v length=%d", q["answered"], q["hasMore"], len(q["values"].([]any)))
	}
	for _, got := range q["values"].([]any) {
		if got != value {
			t.Fatal("long text was truncated or changed")
		}
	}
	stats, err = repo.Stats(ctx, survey, 599, 50)
	if err != nil {
		t.Fatal(err)
	}
	q = stats["questions"].([]map[string]any)[0]
	if q["hasMore"] != false || len(q["values"].([]any)) != 1 || q["values"].([]any)[0] != value {
		t.Fatal("last page must contain the final intact long-text answer")
	}
}

func TestStatsPaginationAndSummaries(t *testing.T) {
	repo, ctx := statsTestStore(t)
	choices := []domain.Option{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}, {ID: "c", Label: "C"}}
	survey := &domain.Survey{ID: "pages", Questions: []domain.Question{
		{ID: "text", Title: "Text", Type: "text"},
		{ID: "single", Title: "Single", Type: "single", Options: choices},
		{ID: "multiple", Title: "Multiple", Type: "multiple", Options: choices},
		{ID: "number", Title: "Number", Type: "number"},
	}}
	answers := []map[string]any{
		{"text": "first", "single": "a", "multiple": []string{"a", "b"}, "number": float64(1)},
		{"text": "second", "single": "b", "multiple": []string{"a"}, "number": float64(3)},
		{"text": "third", "single": "a", "multiple": []string{"b"}, "number": float64(5)},
		{}, // Skipped questions do not count as answered.
		{"text": nil, "single": nil, "multiple": nil, "number": nil},
		{"text": "", "multiple": []string{}, "number": float64(0)},
	}
	// Timestamp groups, nonchronological IDs, and reversed insertion order exercise
	// both the primary sort and the _id tie-breaker.
	for i := len(answers) - 1; i >= 0; i-- {
		response := &domain.Response{SchemaVersion: 1, ID: fmt.Sprintf("%04d", i%2+(2-i/2)*2), SurveyID: survey.ID,
			UserID: "private-participant", Answers: answers[i], Path: []string{}, SubmittedAt: time.Unix(int64(i/2), 0)}
		if err := repo.InsertResponse(ctx, response, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.InsertResponse(ctx, &domain.Response{SchemaVersion: 1, ID: "other", SurveyID: "other-survey",
		Answers: map[string]any{"text": "must not leak", "single": "c", "number": float64(100)}, Path: []string{}, SubmittedAt: time.Unix(0, 0)}, ""); err != nil {
		t.Fatal(err)
	}
	wantAnswered := []int64{4, 3, 4, 4}
	wantCounts := [][]int64{nil, {2, 1, 0}, {2, 2, 0}, nil}
	var allText []any
	for _, offset := range []int{0, 2, 4, 99} {
		stats, err := repo.Stats(ctx, survey, offset, 2)
		if err != nil {
			t.Fatal(err)
		}
		if stats["totalResponses"] != int64(6) {
			t.Fatalf("totalResponses = %v, want 6", stats["totalResponses"])
		}
		questions := stats["questions"].([]map[string]any)
		if len(questions) != len(survey.Questions) {
			t.Fatalf("questions = %v", questions)
		}
		for i, q := range questions {
			if q["answered"] != wantAnswered[i] || q["id"] != survey.Questions[i].ID || q["title"] != survey.Questions[i].Title || q["type"] != survey.Questions[i].Type {
				t.Fatalf("incorrect summary: %v", q)
			}
			if q["valuesOffset"] != offset || q["valuesLimit"] != 2 || q["hasMore"] != (int64(offset+2) < wantAnswered[i]) {
				t.Fatalf("incorrect pagination: %v", q)
			}
			values := q["values"].([]any)
			wantLength := max(0, min(2, int(wantAnswered[i])-offset))
			if values == nil || len(values) != wantLength {
				t.Fatalf("offset %d question %v: values = %v, want %d entries", offset, q["id"], values, wantLength)
			}
			if i == 0 {
				allText = append(allText, values...)
			}
			if i == 3 && q["average"] != 2.25 || i != 3 && q["average"] != nil {
				t.Fatalf("incorrect average: %v", q)
			}
			for j, option := range q["options"].([]map[string]any) {
				if option["id"] != choices[j].ID || option["label"] != choices[j].Label || option["count"] != wantCounts[i][j] {
					t.Fatalf("incorrect option count: %v", option)
				}
			}
		}
	}
	if fmt.Sprint(allText) != "[first second third ]" {
		t.Fatalf("pages must preserve order without duplicates or omissions: %v", allText)
	}
}

func TestStatsEmptySurveys(t *testing.T) {
	repo, ctx := statsTestStore(t)
	for _, questions := range [][]domain.Question{nil, {{ID: "q", Title: "Number", Type: "number"}}} {
		stats, err := repo.Stats(ctx, &domain.Survey{ID: "empty", Questions: questions}, 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		if stats["totalResponses"] != int64(0) {
			t.Fatalf("totalResponses = %v, want 0", stats["totalResponses"])
		}
		got := stats["questions"].([]map[string]any)
		if got == nil || len(got) != len(questions) {
			t.Fatalf("questions = %v, want %d entries", got, len(questions))
		}
		for _, q := range got {
			if q["answered"] != int64(0) || q["average"] != nil || q["hasMore"] != false || len(q["values"].([]any)) != 0 || q["values"].([]any) == nil {
				t.Fatalf("incorrect empty question stats: %v", q)
			}
		}
	}
}

func TestStatsRejectsInvalidPagination(t *testing.T) {
	// Invalid parameters must be rejected before any database access.
	repo := &Store{}
	for _, page := range [][2]int{{-1, 20}, {0, -1}, {0, 0}, {0, 51}} {
		if _, err := repo.Stats(context.Background(), &domain.Survey{}, page[0], page[1]); err == nil {
			t.Errorf("accepted offset=%d limit=%d", page[0], page[1])
		}
	}
}
