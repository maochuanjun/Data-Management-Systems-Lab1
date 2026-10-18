package unit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"questionnaire/internal/domain"
	"questionnaire/internal/web"
)

// The real HTTP handlers and domain validators run in these tests. Only the
// database boundary is replaced; unexpected repository methods remain unusable.
type requestSizeRepository struct {
	web.Repository
	survey    *domain.Survey
	responses []*domain.Response
}

func (repo *requestSizeRepository) SessionUser(context.Context, string) (*domain.User, error) {
	return &domain.User{ID: "size-respondent", Username: "size_respondent"}, nil
}

func (repo *requestSizeRepository) Survey(context.Context, string) (*domain.Survey, error) {
	return repo.survey, nil
}

func (repo *requestSizeRepository) InsertResponse(_ context.Context, response *domain.Response, _ string) error {
	repo.responses = append(repo.responses, response)
	return nil
}

func requestSizeSurvey(t *testing.T, count int, answer string) (*domain.Survey, map[string]any) {
	t.Helper()
	length := utf8.RuneCountInString(answer)
	survey := &domain.Survey{
		SchemaVersion: 1, ID: "large-response", OwnerID: "size-owner",
		Title: "Text answer capacity", Status: "published", AllowRepeat: true,
		Questions: make([]domain.Question, count),
	}
	answers := make(map[string]any, count)
	for index := range survey.Questions {
		id := fmt.Sprintf("q%d", index+1)
		survey.Questions[index] = domain.Question{
			ID: id, Title: fmt.Sprintf("Comment %d", index+1),
			Type: "fill", FillType: "text", Required: true,
			Validation: domain.Validation{MinLength: &length, MaxLength: &length},
		}
		answers[id] = answer
	}
	if err := domain.ValidateSurvey(survey); err != nil {
		t.Fatalf("survey must be legal: %v", err)
	}
	if _, path, err := domain.ValidateSubmission(survey, answers); err != nil || len(path) != count {
		t.Fatalf("answer set must be legal: path=%d error=%v", len(path), err)
	}
	return survey, answers
}

func requestSizeJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func requestSizePOST(handler http.Handler, path string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "session", Value: "size-session"})
	// Exercise streaming enforcement too: an untrusted/missing Content-Length
	// must not bypass the limit or be required to detect an oversized body.
	request.ContentLength = -1
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestLargeChineseResponseCanBeSubmitted(t *testing.T) {
	survey, answers := requestSizeSurvey(t, 36, strings.Repeat("中", 10000))
	repo := &requestSizeRepository{survey: survey}
	handler := web.New(repo, false)
	base := "/api/public/surveys/" + survey.ID
	for index, question := range survey.Questions {
		body := requestSizeJSON(t, map[string]any{"questionId": question.ID, "answer": answers[question.ID]})
		response := requestSizePOST(handler, base+"/next", body)
		if response.Code != http.StatusOK {
			t.Fatalf("question %s: status=%d body=%s", question.ID, response.Code, response.Body)
		}
		var result struct {
			Answer string `json:"answer"`
			Next   string `json:"nextQuestionId"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		wantNext := domain.End
		if index+1 < len(survey.Questions) {
			wantNext = survey.Questions[index+1].ID
		}
		if result.Answer != answers[question.ID] || result.Next != wantNext {
			t.Fatalf("question %s did not retain its answer and next step", question.ID)
		}
	}
	body := requestSizeJSON(t, map[string]any{"anonymous": false, "answers": answers})
	if len(body) <= 1<<20 {
		t.Fatalf("regression must exceed the old limit, got %d bytes", len(body))
	}
	response := requestSizePOST(handler, base+"/responses", body)
	if response.Code != http.StatusCreated || len(repo.responses) != 1 {
		t.Fatalf("submission: status=%d persisted=%d body=%s", response.Code, len(repo.responses), response.Body)
	}
	if got := repo.responses[0]; !reflect.DeepEqual(got.Answers, answers) || len(got.Path) != 36 {
		t.Fatal("persisted response lost answers or path entries")
	}
	t.Logf("36 answers with 10000 Chinese characters each: %d bytes, every /next=200 and /responses=201", len(body))
}

func TestMaximumEscapedUnicodeResponseCanBeSubmitted(t *testing.T) {
	const questionCount, answerLength = 100, 10000
	survey, answers := requestSizeSurvey(t, questionCount, strings.Repeat("🙂", answerLength))
	escaped := json.RawMessage(`"` + strings.Repeat(`\ud83d\ude42`, answerLength) + `"`)
	escapedAnswers := make(map[string]json.RawMessage, questionCount)
	for index := range survey.Questions {
		oldID := survey.Questions[index].ID
		// Also use the maximum legal question-ID length, including key overhead.
		id := strings.Repeat("q", 61) + fmt.Sprintf("%03d", index+1)
		survey.Questions[index].ID = id
		answers[id] = answers[oldID]
		delete(answers, oldID)
		escapedAnswers[id] = escaped
	}
	body := requestSizeJSON(t, map[string]any{"anonymous": false, "answers": escapedAnswers})
	if len(body) <= 12*questionCount*answerLength || len(body) >= 16<<20 {
		t.Fatalf("unexpected maximum escaped payload size: %d", len(body))
	}
	repo := &requestSizeRepository{survey: survey}
	response := requestSizePOST(web.New(repo, false), "/api/public/surveys/"+survey.ID+"/responses", body)
	if response.Code != http.StatusCreated || len(repo.responses) != 1 {
		t.Fatalf("maximum escaped submission: status=%d persisted=%d body=%s", response.Code, len(repo.responses), response.Body)
	}
	if got := repo.responses[0]; !reflect.DeepEqual(got.Answers, answers) || len(got.Path) != questionCount {
		t.Fatal("escaped Unicode answers or path did not survive decoding and validation")
	}
	t.Logf("100 answers x 10000 non-BMP code points, 12 escaped bytes per code point, 64-byte IDs: %d-byte JSON accepted", len(body))
}

func TestRequestBodyLimits(t *testing.T) {
	for _, endpoint := range []struct {
		name, path, prefix, suffix string
		limit                      int
	}{
		{"ordinary", "/api/register", `{"username":"`, `","password":"password123"}`, 1 << 20},
		{"response", "/api/public/surveys/large-response/responses", `{"answers":{"q1":"`, `"}}`, 16 << 20},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, phase := range []string{"first value", "trailing whitespace"} {
				t.Run(phase, func(t *testing.T) {
					repo := &requestSizeRepository{}
					var body []byte
					if phase == "first value" {
						body = []byte(endpoint.prefix + strings.Repeat("a", endpoint.limit) + endpoint.suffix)
					} else {
						body = append([]byte(`{}`), bytes.Repeat([]byte(" "), endpoint.limit-1)...)
					}
					response := requestSizePOST(web.New(repo, false), endpoint.path, body)
					if response.Code != http.StatusRequestEntityTooLarge {
						t.Fatalf("%d-byte body: status=%d body=%s", len(body), response.Code, response.Body)
					}
					var result struct {
						Error string `json:"error"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					want := fmt.Sprintf("请求内容超过 %d MiB 大小限制", endpoint.limit>>20)
					if result.Error != want || len(repo.responses) != 0 {
						t.Fatalf("error=%q want=%q persisted=%d", result.Error, want, len(repo.responses))
					}
				})
			}
		})
	}
}

func TestResponseBodyAtLimitCanBeSubmitted(t *testing.T) {
	const limit = 16 << 20
	survey, answers := requestSizeSurvey(t, 1, "ok")
	body := requestSizeJSON(t, map[string]any{"answers": answers})
	body = append(body, bytes.Repeat([]byte(" "), limit-len(body))...)
	repo := &requestSizeRepository{survey: survey}
	response := requestSizePOST(web.New(repo, false), "/api/public/surveys/"+survey.ID+"/responses", body)
	if response.Code != http.StatusCreated || len(repo.responses) != 1 {
		t.Fatalf("body exactly at %d-byte limit: status=%d persisted=%d body=%s", limit, response.Code, len(repo.responses), response.Body)
	}
}

func TestMalformedJSONRemainsBadRequest(t *testing.T) {
	for _, path := range []string{"/api/register", "/api/public/surveys/large-response/responses"} {
		for _, body := range []string{`{`, `{} [`, `{} {}`, `{"unknownField":true}`} {
			t.Run(path+" "+body, func(t *testing.T) {
				repo := &requestSizeRepository{}
				response := requestSizePOST(web.New(repo, false), path, []byte(body))
				if response.Code != http.StatusBadRequest || len(repo.responses) != 0 {
					t.Fatalf("status=%d persisted=%d body=%s", response.Code, len(repo.responses), response.Body)
				}
			})
		}
	}
}
