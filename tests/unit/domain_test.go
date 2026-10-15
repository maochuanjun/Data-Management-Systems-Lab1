package unit_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"questionnaire/internal/domain"
)

// JavaScript and JSON number interoperability specification: 2^53 - 1.
const maxSafeInteger = 9007199254740991

func intPtr(n int) *int           { return &n }
func floatPtr(n float64) *float64 { return &n }

func choiceQuestion(kind string) domain.Question {
	return domain.Question{ID: "q1", Title: "请选择", Type: kind, Required: true,
		Options: []domain.Option{{ID: "a", Label: "选项甲"}, {ID: "b", Label: "选项乙"}, {ID: "c", Label: "选项丙"}}}
}

func testSurvey() *domain.Survey {
	return &domain.Survey{Title: "测试问卷", Questions: []domain.Question{
		choiceQuestion("single"),
		{ID: "q2", Title: "描述", Type: "text", Required: true},
		{ID: "q3", Title: "年龄", Type: "number", Required: true},
	}}
}

func TestValidateSurvey(t *testing.T) {
	tests := []struct {
		name   string
		change func(*domain.Survey)
		valid  bool
	}{
		{"normal", func(s *domain.Survey) {}, true},
		{"empty draft", func(s *domain.Survey) { s.Questions = nil }, true},
		{"blank title", func(s *domain.Survey) { s.Title = " \t" }, false},
		{"unicode title limit", func(s *domain.Survey) { s.Title = strings.Repeat("中", 200) }, true},
		{"long title", func(s *domain.Survey) { s.Title = strings.Repeat("中", 201) }, false},
		{"duplicate question", func(s *domain.Survey) { s.Questions[1].ID = "q1" }, false},
		{"unsafe ID", func(s *domain.Survey) { s.Questions[0].ID = "q.1" }, false},
		{"reserved ID", func(s *domain.Survey) { s.Questions[0].ID = "END" }, false},
		{"blank question", func(s *domain.Survey) { s.Questions[0].Title = "" }, false},
		{"long question", func(s *domain.Survey) { s.Questions[0].Title = strings.Repeat("文", 501) }, false},
		{"invalid type", func(s *domain.Survey) { s.Questions[0].Type = "date" }, false},
		{"too few options", func(s *domain.Survey) { s.Questions[0].Options = s.Questions[0].Options[:1] }, false},
		{"duplicate option", func(s *domain.Survey) { s.Questions[0].Options[1].ID = "a" }, false},
		{"blank label", func(s *domain.Survey) { s.Questions[0].Options[1].Label = " " }, false},
		{"wrong validation type", func(s *domain.Survey) { s.Questions[0].Validation.MinLength = intPtr(2) }, false},
		{"negative length", func(s *domain.Survey) { s.Questions[1].Validation.MinLength = intPtr(-1) }, false},
		{"excessive length", func(s *domain.Survey) { s.Questions[1].Validation.MaxLength = intPtr(10001) }, false},
		{"inverted length", func(s *domain.Survey) {
			s.Questions[1].Validation = domain.Validation{MinLength: intPtr(5), MaxLength: intPtr(2)}
		}, false},
		{"inverted number", func(s *domain.Survey) {
			s.Questions[2].Validation = domain.Validation{Min: floatPtr(5), Max: floatPtr(2)}
		}, false},
		{"nonfinite bound", func(s *domain.Survey) { s.Questions[2].Validation.Min = floatPtr(math.NaN()) }, false},
		{"selection greater than options", func(s *domain.Survey) {
			s.Questions[0].Type = "multiple"
			s.Questions[0].Validation.MinSelect = intPtr(4)
		}, false},
		{"forward rule", func(s *domain.Survey) {
			s.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: "q3"}}
		}, true},
		{"end rule", func(s *domain.Survey) {
			s.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: domain.End}}
		}, true},
		{"unknown target", func(s *domain.Survey) {
			s.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: "missing"}}
		}, false},
		{"self target", func(s *domain.Survey) {
			s.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: "q1"}}
		}, false},
		{"backwards target", func(s *domain.Survey) { s.Questions[2].Rules = []domain.Rule{{Operator: "gt", Value: 1, Target: "q1"}} }, false},
		{"unknown rule option", func(s *domain.Survey) {
			s.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "unknown", Target: domain.End}}
		}, false},
		{"invalid operator", func(s *domain.Survey) {
			s.Questions[0].Rules = []domain.Rule{{Operator: "gt", Value: "a", Target: domain.End}}
		}, false},
		{"text rule forbidden", func(s *domain.Survey) {
			s.Questions[1].Rules = []domain.Rule{{Operator: "eq", Value: "yes", Target: domain.End}}
		}, false},
		{"numeric integer rule", func(s *domain.Survey) {
			s.Questions[2].Rules = []domain.Rule{{Operator: "gte", Value: int32(18), Target: domain.End}}
		}, true},
		{"numeric string rule", func(s *domain.Survey) {
			s.Questions[2].Rules = []domain.Rule{{Operator: "gte", Value: "18", Target: domain.End}}
		}, false},
		{"numeric nonfinite rule", func(s *domain.Survey) {
			s.Questions[2].Rules = []domain.Rule{{Operator: "gte", Value: math.Inf(1), Target: domain.End}}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testSurvey()
			tt.change(s)
			if err := domain.ValidateSurvey(s); (err == nil) != tt.valid {
				t.Fatalf("domain.ValidateSurvey = %v, valid = %v", err, tt.valid)
			}
		})
	}
	if domain.ValidateSurvey(nil) == nil {
		t.Fatal("nil survey accepted")
	}
	s := testSurvey()
	s.Questions = make([]domain.Question, 101)
	if domain.ValidateSurvey(s) == nil {
		t.Fatal("101 questions accepted")
	}
}

func TestValidateAnswer(t *testing.T) {
	single := choiceQuestion("single")
	multiple := choiceQuestion("multiple")
	multiple.Validation = domain.Validation{MinSelect: intPtr(1), MaxSelect: intPtr(2)}
	text := domain.Question{Type: "text", Required: true, Validation: domain.Validation{MinLength: intPtr(2), MaxLength: intPtr(3)}}
	number := domain.Question{Type: "number", Required: true, Validation: domain.Validation{Integer: true, Min: floatPtr(0), Max: floatPtr(120)}}
	tests := []struct {
		name  string
		q     domain.Question
		value any
		want  any
		valid bool
	}{
		{"single", single, "a", "a", true},
		{"single nonexistent", single, "d", nil, false},
		{"single wrong type", single, []string{"a"}, nil, false},
		{"required nil", single, nil, nil, false},
		{"optional nil", domain.Question{Type: "text"}, nil, nil, true},
		{"multiple json array", multiple, []any{"a", "b"}, []string{"a", "b"}, true},
		{"multiple strings", multiple, []string{"b"}, []string{"b"}, true},
		{"multiple duplicate", multiple, []any{"a", "a"}, nil, false},
		{"multiple unknown", multiple, []any{"a", "z"}, nil, false},
		{"multiple number", multiple, []any{"a", 1}, nil, false},
		{"multiple minimum", multiple, []string{}, nil, false},
		{"multiple maximum", multiple, []string{"a", "b", "c"}, nil, false},
		{"unicode text", text, "中文🙂", "中文🙂", true},
		{"text too short", text, "中", nil, false},
		{"text too long", text, "中文🙂文", nil, false},
		{"text whitespace", text, "   ", nil, false},
		{"text wrong type", text, 123, nil, false},
		{"text hard limit", domain.Question{Type: "text"}, strings.Repeat("文", 10001), nil, false},
		{"integer", number, float64(18), float64(18), true},
		{"integer BSON", number, int64(18), float64(18), true},
		{"fraction forbidden", number, 18.5, nil, false},
		{"below bound", number, -1.0, nil, false},
		{"above bound", number, 121.0, nil, false},
		{"string number", number, "18", nil, false},
		{"NaN", number, math.NaN(), nil, false},
		{"infinity", number, math.Inf(-1), nil, false},
		{"decimal", domain.Question{Type: "number"}, 1.25, 1.25, true},
		{"safe integer", domain.Question{Type: "number"}, float64(maxSafeInteger), float64(maxSafeInteger), true},
		{"unsafe integer", domain.Question{Type: "number"}, float64(maxSafeInteger + 1), nil, false},
		{"unsafe negative integer", domain.Question{Type: "number"}, -float64(maxSafeInteger + 1), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ValidateAnswer(tt.q, tt.value)
			if (err == nil) != tt.valid || (tt.valid && !reflect.DeepEqual(got, tt.want)) {
				t.Fatalf("got (%#v, %v), want (%#v, valid=%v)", got, err, tt.want, tt.valid)
			}
		})
	}
}

func TestNextQuestion(t *testing.T) {
	s := testSurvey()
	s.Questions[0].Rules = []domain.Rule{
		{Operator: "eq", Value: "a", Target: "q3"},
		{Operator: "eq", Value: "a", Target: domain.End},
		{Operator: "eq", Value: "b", Target: domain.End},
	}
	for _, tt := range []struct {
		id     string
		answer any
		want   string
	}{
		{"q1", "a", "q3"}, {"q1", "b", domain.End}, {"q1", "c", "q2"},
		{"q1", nil, "q2"}, {"q2", "hello", "q3"}, {"q3", 12.0, domain.End},
	} {
		if got := domain.NextQuestion(s, tt.id, tt.answer); got != tt.want {
			t.Fatalf("domain.NextQuestion(%s, %v) = %s, want %s", tt.id, tt.answer, got, tt.want)
		}
	}
	for _, tt := range []struct {
		op     string
		answer float64
		match  bool
	}{
		{"eq", 18, true}, {"eq", 19, false}, {"gt", 19, true}, {"gt", 18, false},
		{"gte", 18, true}, {"gte", 17, false}, {"lt", 17, true}, {"lt", 18, false},
		{"lte", 18, true}, {"lte", 19, false},
	} {
		numeric := &domain.Survey{Questions: []domain.Question{
			{ID: "number", Type: "number", Rules: []domain.Rule{{Operator: tt.op, Value: int32(18), Target: domain.End}}},
			{ID: "following", Type: "text"},
		}}
		if got := domain.NextQuestion(numeric, "number", tt.answer) == domain.End; got != tt.match {
			t.Errorf("%v %s 18 = %v, want %v", tt.answer, tt.op, got, tt.match)
		}
	}
}

func TestMultipleRulesAndNamedArrays(t *testing.T) {
	// BSON arrays are named slices of interface values as well.
	type bsonArray []any
	q := choiceQuestion("multiple")
	q.Rules = []domain.Rule{
		{Operator: "contains_all", Value: bsonArray{"a", "b"}, Target: domain.End},
		{Operator: "contains", Value: "c", Target: domain.End},
	}
	s := testSurvey()
	s.Questions[0] = q
	if err := domain.ValidateSurvey(s); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		answer any
		want   string
	}{
		{bsonArray{"a", "b"}, domain.End}, {[]string{"b", "a"}, domain.End},
		{[]any{"a"}, "q2"}, {[]string{"c"}, domain.End},
	} {
		if got := domain.NextQuestion(s, "q1", tt.answer); got != tt.want {
			t.Errorf("domain.NextQuestion(%v) = %s, want %s", tt.answer, got, tt.want)
		}
	}
	for _, invalid := range []any{[]string{}, []string{"a", "a"}, []any{"a", 1}, []string{"z"}} {
		s.Questions[0].Rules[0].Value = invalid
		if domain.ValidateSurvey(s) == nil {
			t.Errorf("invalid contains_all value accepted: %#v", invalid)
		}
	}
}

func TestValidateSubmission(t *testing.T) {
	s := testSurvey()
	s.Questions[0].Rules = []domain.Rule{{Operator: "eq", Value: "a", Target: "q3"}, {Operator: "eq", Value: "b", Target: domain.End}}
	answers, path, err := domain.ValidateSubmission(s, map[string]any{"q1": "a", "q3": int32(23)})
	if err != nil || !reflect.DeepEqual(path, []string{"q1", "q3"}) || answers["q3"] != float64(23) {
		t.Fatalf("submission = %#v, %#v, %v", answers, path, err)
	}
	_, path, err = domain.ValidateSubmission(s, map[string]any{"q1": "b"})
	if err != nil || !reflect.DeepEqual(path, []string{"q1"}) {
		t.Fatalf("END = %v, %v", path, err)
	}
	for _, bad := range []map[string]any{
		{"q1": "a"},
		{"q1": "a", "q2": "forged", "q3": 23.0},
		{"q1": "a", "q2": nil, "q3": 23.0},
		{"q1": "b", "unknown": nil},
		{"q1": "c", "q3": 23.0},
	} {
		if _, _, err := domain.ValidateSubmission(s, bad); err == nil {
			t.Errorf("bad answers accepted: %#v", bad)
		}
	}
	s.Questions[0].Required = false
	_, path, err = domain.ValidateSubmission(s, map[string]any{"q2": "描述", "q3": 0.0})
	if err != nil || !reflect.DeepEqual(path, []string{"q1", "q2", "q3"}) {
		t.Fatalf("optional = %v, %v", path, err)
	}
	s.Questions = nil
	if _, _, err := domain.ValidateSubmission(s, nil); err == nil {
		t.Fatal("empty survey accepted")
	}
}

func TestRequiredConstraintsHaveAnAnswer(t *testing.T) {
	for _, tt := range []struct {
		name   string
		q      domain.Question
		valid  bool
		answer any
	}{
		{"required multiple zero maximum", domain.Question{Type: "multiple", Required: true, Validation: domain.Validation{MaxSelect: intPtr(0)}}, false, nil},
		{"optional multiple zero maximum", domain.Question{Type: "multiple", Validation: domain.Validation{MaxSelect: intPtr(0)}}, true, nil},
		{"required multiple one maximum", domain.Question{Type: "multiple", Required: true, Validation: domain.Validation{MaxSelect: intPtr(1)}}, true, []string{"a"}},
		{"required text zero maximum", domain.Question{Type: "text", Required: true, Validation: domain.Validation{MaxLength: intPtr(0)}}, false, nil},
		{"optional text zero maximum", domain.Question{Type: "text", Validation: domain.Validation{MaxLength: intPtr(0)}}, true, nil},
		{"required text one maximum", domain.Question{Type: "text", Required: true, Validation: domain.Validation{MaxLength: intPtr(1)}}, true, "中"},
		{"integer gap", domain.Question{Type: "number", Required: true, Validation: domain.Validation{Integer: true, Min: floatPtr(.1), Max: floatPtr(.9)}}, false, nil},
		{"negative integer gap", domain.Question{Type: "number", Required: true, Validation: domain.Validation{Integer: true, Min: floatPtr(-.9), Max: floatPtr(-.1)}}, false, nil},
		{"optional integer gap", domain.Question{Type: "number", Validation: domain.Validation{Integer: true, Min: floatPtr(.1), Max: floatPtr(.9)}}, true, nil},
		{"integer endpoint", domain.Question{Type: "number", Required: true, Validation: domain.Validation{Integer: true, Min: floatPtr(.1), Max: floatPtr(1)}}, true, 1.0},
		{"decimal interval", domain.Question{Type: "number", Required: true, Validation: domain.Validation{Min: floatPtr(.1), Max: floatPtr(.9)}}, true, .5},
		{"unsafe lower bound", domain.Question{Type: "number", Required: true, Validation: domain.Validation{Min: floatPtr(maxSafeInteger + 1)}}, false, nil},
		{"unsafe upper bound", domain.Question{Type: "number", Required: true, Validation: domain.Validation{Max: floatPtr(-maxSafeInteger - 1)}}, false, nil},
		{"positive safe endpoint", domain.Question{Type: "number", Required: true, Validation: domain.Validation{Integer: true, Min: floatPtr(maxSafeInteger)}}, true, float64(maxSafeInteger)},
		{"negative safe endpoint", domain.Question{Type: "number", Required: true, Validation: domain.Validation{Max: floatPtr(-maxSafeInteger)}}, true, -float64(maxSafeInteger)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q := tt.q
			q.ID, q.Title = "q", tt.name
			if q.Type == "multiple" {
				q.Options = choiceQuestion("multiple").Options
			}
			if err := domain.ValidateSurvey(&domain.Survey{Title: "constraints", Questions: []domain.Question{q}}); (err == nil) != tt.valid {
				t.Fatalf("domain.ValidateSurvey = %v, valid = %v", err, tt.valid)
			}
			if tt.valid {
				if _, err := domain.ValidateAnswer(q, tt.answer); err != nil {
					t.Fatalf("witness answer rejected: %v", err)
				}
			}
		})
	}
}

func TestSubmissionWithJavaScriptPropertyIDs(t *testing.T) {
	s := &domain.Survey{Title: "special keys", Questions: []domain.Question{
		{ID: "__proto__", Title: "text", Type: "text", Required: true},
		{ID: "constructor", Title: "number", Type: "number", Required: true},
	}}
	answers, path, err := domain.ValidateSubmission(s, map[string]any{"__proto__": "answer", "constructor": 42.0})
	if err != nil || answers["__proto__"] != "answer" || answers["constructor"] != 42.0 || len(path) != 2 {
		t.Fatalf("special keys submission = %#v, %v, %v", answers, path, err)
	}
}

func TestMultipleChoiceExactCount(t *testing.T) {
	q := choiceQuestion("multiple")
	q.Validation = domain.Validation{MinSelect: intPtr(2), MaxSelect: intPtr(2)}
	survey := &domain.Survey{Title: "Exactly two choices", Questions: []domain.Question{q}}
	if err := domain.ValidateSurvey(survey); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		answer any
		valid  bool
	}{
		{"one", []string{"a"}, false},
		{"exactly two", []string{"a", "b"}, true},
		{"three", []string{"a", "b", "c"}, false},
		{"repeated choice is not two choices", []string{"a", "a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := domain.ValidateSubmission(survey, map[string]any{"q1": tc.answer})
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateSubmission = %v, want valid=%v", err, tc.valid)
			}
		})
	}
}
