package unit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"questionnaire/internal/domain"
	"questionnaire/internal/web"
)

func TestFeedbackDuplicateOptionLabelsRejected(t *testing.T) {
	for _, kind := range []string{"single", "multiple"} {
		for _, label := range []string{"相同选项", " \t相同选项\n", "\u3000相同选项\u00a0"} {
			t.Run(kind+"/"+label, func(t *testing.T) {
				q := choiceQuestion(kind)
				q.Options = []domain.Option{{ID: "first", Label: "相同选项"}, {ID: "second", Label: label}}
				survey := &domain.Survey{Title: "选项检查", Questions: []domain.Question{
					{ID: "intro", Title: "说明", Type: "fill", FillType: "text"}, q,
				}}
				err := domain.ValidateSurvey(survey)
				if err == nil || !strings.Contains(err.Error(), "第 2 题") || !strings.Contains(err.Error(), "第 2 个选项") || !strings.Contains(err.Error(), "第 1 个选项") {
					t.Fatalf("duplicate labels need question and option positions, got %v", err)
				}
				if err := domain.ValidateReusableQuestion(&q); err == nil {
					t.Fatal("reusable question accepted duplicate labels")
				}
			})
		}
	}
	q := choiceQuestion("single")
	q.Options[0].Label = "甲"
	q.Options[1].Label = " 乙 "
	before := append([]domain.Option(nil), q.Options...)
	if err := domain.ValidateSurvey(&domain.Survey{Title: "不改写文本", Questions: []domain.Question{q}}); err != nil || !reflect.DeepEqual(q.Options, before) {
		t.Fatalf("distinct labels must retain their original text: %v %#v", err, q.Options)
	}
}

func TestFeedbackValidationUsesQuestionNumbers(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*domain.Survey)
		want string
	}{
		{"invalid question identifier", func(s *domain.Survey) { s.Questions[1].ID = "hidden.invalid-id" }, "第 2 题"},
		{"duplicate question identifier", func(s *domain.Survey) { s.Questions[1].ID = s.Questions[0].ID }, "第 2 题"},
		{"blank title", func(s *domain.Survey) { s.Questions[1].Title = "" }, "第 2 题"},
		{"invalid option", func(s *domain.Survey) { s.Questions[0].Options[1].ID = ".hidden-option" }, "第 1 题"},
		{"bad target", func(s *domain.Survey) {
			s.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: "hidden-target"}}
		}, "第 1 题"},
		{"bad rule", func(s *domain.Survey) {
			s.Questions[0].Rules = []domain.Rule{{Operator: "gt", Value: "a", Target: domain.End}}
		}, "第 1 题"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSurvey()
			for i := range s.Questions {
				s.Questions[i].ID = "hidden-" + s.Questions[i].ID
			}
			tc.edit(s)
			err := domain.ValidateSurvey(s)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "hidden-") || strings.Contains(err.Error(), "hidden.") {
				t.Fatalf("need a readable position without internal identifiers, got %v", err)
			}
		})
	}

	s := testSurvey()
	s.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: "q3"}}
	for _, tc := range []struct {
		name    string
		answers map[string]any
		want    string
	}{
		{"missing required answer", map[string]any{"q1": "a"}, "第 3 题"},
		{"invalid answer", map[string]any{"q1": "a", "q3": "NaN"}, "第 3 题"},
		{"skipped answer", map[string]any{"q1": "a", "q2": nil, "q3": 20}, "第 2 题已跳过"},
		{"unknown answer", map[string]any{"q1": "a", "q3": 20, "hidden.unknown-id": nil}, "答案包含不存在的题目"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := domain.ValidateSubmission(s, tc.answers)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "hidden.") || strings.Contains(err.Error(), "q2") || strings.Contains(err.Error(), "q3") {
				t.Fatalf("need accurate question numbering without raw identifiers, got %v", err)
			}
		})
	}
	// Legal protocol identifiers that resemble JavaScript object properties must
	// still follow the saved order and retain their exact path/answer keys.
	special := testSurvey()
	for i, id := range []string{"__proto__", "constructor", "toString"} {
		special.Questions[i].ID = id
	}
	special.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: "toString"}}
	got, path, err := domain.ValidateSubmission(special, map[string]any{"__proto__": "a", "toString": 20})
	if err != nil || !reflect.DeepEqual(path, []string{"__proto__", "toString"}) || got["toString"] != float64(20) {
		t.Fatalf("special identifiers changed path or answer semantics: %v %v %v", got, path, err)
	}
	_, _, err = domain.ValidateSubmission(special, map[string]any{"__proto__": "a"})
	if err == nil || !strings.Contains(err.Error(), "第 3 题") || strings.Contains(err.Error(), "toString") {
		t.Fatalf("special identifier must not leak in errors: %v", err)
	}
}

func TestFeedbackPublishedDuplicateLabelsRemainAnswerable(t *testing.T) {
	for _, kind := range []string{"single", "multiple"} {
		t.Run(kind, func(t *testing.T) {
			q := choiceQuestion(kind)
			q.Options[1].Label = q.Options[0].Label
			s := &domain.Survey{Title: "已发布旧问卷", Status: "published", Questions: []domain.Question{q}}
			before := append([]domain.Option(nil), q.Options...)
			var answer any = "b"
			if kind == "multiple" {
				answer = []string{"a", "b"}
			}
			got, path, err := domain.ValidateSubmission(s, map[string]any{q.ID: answer})
			if err != nil || len(path) != 1 || !reflect.DeepEqual(got[q.ID], answer) || !reflect.DeepEqual(s.Questions[0].Options, before) {
				t.Fatalf("saved snapshot and option IDs must remain usable, got=%v path=%v err=%v", got, path, err)
			}
		})
	}
}

type feedbackPrecheckRepository struct {
	web.Repository
	survey       *domain.Survey
	found        bool
	err          error
	lookupSurvey string
	lookupKey    string
}

func (r *feedbackPrecheckRepository) SessionUser(context.Context, string) (*domain.User, error) {
	return &domain.User{ID: "feedback-account"}, nil
}
func (r *feedbackPrecheckRepository) Survey(context.Context, string) (*domain.Survey, error) {
	return r.survey, nil
}
func (r *feedbackPrecheckRepository) HasResponse(_ context.Context, survey, key string) (bool, error) {
	r.lookupSurvey, r.lookupKey = survey, key
	return r.found, r.err
}

func TestFeedbackPublicSubmissionPrecheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		repeat bool
		found  bool
		err    error
		status int
	}{
		{"no previous response", false, false, nil, 200},
		{"existing response", false, true, nil, 200},
		{"lookup failure", false, false, errors.New("database unavailable"), 500},
		{"repeat enabled skips lookup", true, true, errors.New("must not query"), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &feedbackPrecheckRepository{survey: &domain.Survey{ID: "feedback-survey", Title: "填写", Status: "published", AllowRepeat: tc.repeat}, found: tc.found, err: tc.err}
			req := httptest.NewRequest("GET", "/api/public/surveys/feedback-survey", nil)
			req.AddCookie(&http.Cookie{Name: "session", Value: "feedback-session"})
			out := httptest.NewRecorder()
			web.New(repo, false).ServeHTTP(out, req)
			if out.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", out.Code, tc.status, out.Body)
			}
			if tc.status == 200 {
				var body struct {
					SubmissionBlocked *bool `json:"submissionBlocked"`
				}
				if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil || body.SubmissionBlocked == nil || *body.SubmissionBlocked != (!tc.repeat && tc.found) {
					t.Fatalf("missing or incorrect per-account submission status: %s; %v", out.Body, err)
				}
			}
			if tc.repeat {
				if repo.lookupKey != "" {
					t.Fatal("repeat-enabled survey queried prior submissions")
				}
			} else {
				hash := sha256.Sum256([]byte("feedback-survey:feedback-account"))
				if repo.lookupSurvey != "feedback-survey" || repo.lookupKey != hex.EncodeToString(hash[:]) {
					t.Fatalf("precheck must use the final submission key: %q %q", repo.lookupSurvey, repo.lookupKey)
				}
			}
		})
	}
}
