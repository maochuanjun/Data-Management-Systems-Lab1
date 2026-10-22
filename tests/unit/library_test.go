package unit_test

import (
	"math"
	"strings"
	"testing"

	"questionnaire/internal/domain"
)

func TestReusableQuestionValidation(t *testing.T) {
	if domain.ValidateReusableQuestion(nil) == nil {
		t.Fatal("nil question accepted")
	}
	for _, tc := range []struct {
		name  string
		edit  func(*domain.Question)
		valid bool
	}{
		{"valid", func(q *domain.Question) {}, true},
		{"blank title", func(q *domain.Question) { q.Title = " " }, false},
		{"missing fill subtype", func(q *domain.Question) { q.FillType = "" }, false},
		{"invalid fill subtype", func(q *domain.Question) { q.FillType = "date" }, false},
		{"legacy text normalized", func(q *domain.Question) { q.Type = "text"; q.FillType = "" }, true},
		{"legacy number normalized", func(q *domain.Question) { q.Type = "number"; q.FillType = "" }, true},
		{"local jump rule rejected", func(q *domain.Question) { q.Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: domain.End}} }, false},
		{"nested question provenance", func(q *domain.Question) { q.SourceQuestionID = "family" }, false},
		{"nested version provenance", func(q *domain.Question) { q.SourceVersionID = "version" }, false},
		{"wrong validation for text", func(q *domain.Question) { q.Validation.Min = floatPtr(1) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := domain.Question{ID: "survey-occurrence", Title: "描述", Type: "fill", FillType: "text", Required: true}
			tc.edit(&q)
			if err := domain.ValidateReusableQuestion(&q); (err == nil) != tc.valid {
				t.Fatalf("validation=%v, valid=%v", err, tc.valid)
			}
			if tc.valid && (q.ID != "template" || q.Type != "fill" || q.FillType == "") {
				t.Fatalf("canonical content: %#v", q)
			}
		})
	}
}

func TestQuestionContentEquality(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*domain.Question)
		equal bool
	}{
		{"identical", func(q *domain.Question) {}, true},
		{"local identity", func(q *domain.Question) { q.ID = "another" }, true},
		{"local jump rule", func(q *domain.Question) { q.Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: domain.End}} }, true},
		{"provenance", func(q *domain.Question) { q.SourceQuestionID = "family"; q.SourceVersionID = "version" }, true},
		{"required flag", func(q *domain.Question) { q.Required = false }, false},
		{"title", func(q *domain.Question) { q.Title = "Changed" }, false},
		{"option label", func(q *domain.Question) { q.Options[0].Label = "Changed" }, false},
		{"option identity", func(q *domain.Question) { q.Options[0].ID = "different" }, false},
		{"option order", func(q *domain.Question) { q.Options[0], q.Options[1] = q.Options[1], q.Options[0] }, false},
		{"validation", func(q *domain.Question) { q.Validation.MinSelect = intPtr(2) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := choiceQuestion("multiple"), choiceQuestion("multiple")
			tc.edit(&b)
			if got := domain.QuestionContentEqual(a, b); got != tc.equal {
				t.Fatalf("equality=%v, want %v", got, tc.equal)
			}
		})
	}
	for _, kind := range []string{"text", "number"} {
		legacy := domain.Question{ID: "old", Title: "Fill", Type: kind}
		modern := domain.Question{ID: "new", Title: "Fill", Type: "fill", FillType: kind, Options: []domain.Option{}}
		if !domain.QuestionContentEqual(legacy, modern) {
			t.Fatalf("legacy %s equivalent rejected", kind)
		}
	}
	a := domain.Question{Title: "Number", Type: "number", Validation: domain.Validation{Min: floatPtr(1)}}
	b := a
	b.Validation.Min = floatPtr(2)
	if domain.QuestionContentEqual(a, b) {
		t.Fatal("different numeric bounds considered equal")
	}
	a.Validation.Min = floatPtr(math.NaN())
	b = a
	if domain.QuestionContentEqual(a, b) {
		t.Fatal("non-JSON numeric content considered equal")
	}
}

func TestQuestionBankValidation(t *testing.T) {
	if domain.ValidateBank(nil) == nil {
		t.Fatal("nil bank accepted")
	}
	for _, tc := range []struct {
		name  string
		bank  domain.QuestionBank
		valid bool
	}{
		{"empty bank", domain.QuestionBank{Name: "我的题库"}, true},
		{"versions", domain.QuestionBank{Name: "题库", VersionIDs: []string{"v1", "v2"}}, true},
		{"blank", domain.QuestionBank{Name: " \t"}, false},
		{"unicode limit", domain.QuestionBank{Name: strings.Repeat("库", 100)}, true},
		{"long name", domain.QuestionBank{Name: strings.Repeat("库", 101)}, false},
		{"long description", domain.QuestionBank{Name: "题库", Description: strings.Repeat("文", 2001)}, false},
		{"duplicate pin", domain.QuestionBank{Name: "题库", VersionIDs: []string{"v1", "v1"}}, false},
		{"empty pin", domain.QuestionBank{Name: "题库", VersionIDs: []string{""}}, false},
		{"invalid pin", domain.QuestionBank{Name: "题库", VersionIDs: []string{"v.1"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := domain.ValidateBank(&tc.bank); (err == nil) != tc.valid {
				t.Fatalf("validation=%v, valid=%v", err, tc.valid)
			}
		})
	}
}
