package domain

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func intPtr(n int) *int           { return &n }
func floatPtr(n float64) *float64 { return &n }

func choiceQuestion(kind string) Question {
	return Question{ID: "q1", Title: "请选择", Type: kind, Required: true,
		Options: []Option{{ID: "a", Label: "选项甲"}, {ID: "b", Label: "选项乙"}, {ID: "c", Label: "选项丙"}}}
}

func testSurvey() *Survey {
	return &Survey{Title: "测试问卷", Questions: []Question{
		choiceQuestion("single"),
		{ID: "q2", Title: "描述", Type: "text", Required: true},
		{ID: "q3", Title: "年龄", Type: "number", Required: true},
	}}
}

func TestValidateSurvey(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Survey)
		valid  bool
	}{
		{"normal", func(s *Survey) {}, true},
		{"empty draft", func(s *Survey) { s.Questions = nil }, true},
		{"blank title", func(s *Survey) { s.Title = " \t" }, false},
		{"unicode title limit", func(s *Survey) { s.Title = strings.Repeat("中", 200) }, true},
		{"long title", func(s *Survey) { s.Title = strings.Repeat("中", 201) }, false},
		{"duplicate question", func(s *Survey) { s.Questions[1].ID = "q1" }, false},
		{"unsafe ID", func(s *Survey) { s.Questions[0].ID = "q.1" }, false},
		{"reserved ID", func(s *Survey) { s.Questions[0].ID = "END" }, false},
		{"blank question", func(s *Survey) { s.Questions[0].Title = "" }, false},
		{"long question", func(s *Survey) { s.Questions[0].Title = strings.Repeat("文", 501) }, false},
		{"invalid type", func(s *Survey) { s.Questions[0].Type = "date" }, false},
		{"too few options", func(s *Survey) { s.Questions[0].Options = s.Questions[0].Options[:1] }, false},
		{"duplicate option", func(s *Survey) { s.Questions[0].Options[1].ID = "a" }, false},
		{"blank label", func(s *Survey) { s.Questions[0].Options[1].Label = " " }, false},
		{"wrong validation type", func(s *Survey) { s.Questions[0].Validation.MinLength = intPtr(2) }, false},
		{"negative length", func(s *Survey) { s.Questions[1].Validation.MinLength = intPtr(-1) }, false},
		{"excessive length", func(s *Survey) { s.Questions[1].Validation.MaxLength = intPtr(10001) }, false},
		{"inverted length", func(s *Survey) { s.Questions[1].Validation = Validation{MinLength: intPtr(5), MaxLength: intPtr(2)} }, false},
		{"inverted number", func(s *Survey) { s.Questions[2].Validation = Validation{Min: floatPtr(5), Max: floatPtr(2)} }, false},
		{"nonfinite bound", func(s *Survey) { s.Questions[2].Validation.Min = floatPtr(math.NaN()) }, false},
		{"selection greater than options", func(s *Survey) { s.Questions[0].Type = "multiple"; s.Questions[0].Validation.MinSelect = intPtr(4) }, false},
		{"forward rule", func(s *Survey) { s.Questions[0].Rules = []Rule{{Operator: "eq", Value: "a", Target: "q3"}} }, true},
		{"end rule", func(s *Survey) { s.Questions[0].Rules = []Rule{{Operator: "eq", Value: "a", Target: End}} }, true},
		{"unknown target", func(s *Survey) { s.Questions[0].Rules = []Rule{{Operator: "eq", Value: "a", Target: "missing"}} }, false},
		{"self target", func(s *Survey) { s.Questions[0].Rules = []Rule{{Operator: "eq", Value: "a", Target: "q1"}} }, false},
		{"backwards target", func(s *Survey) { s.Questions[2].Rules = []Rule{{Operator: "gt", Value: 1, Target: "q1"}} }, false},
		{"unknown rule option", func(s *Survey) { s.Questions[0].Rules = []Rule{{Operator: "eq", Value: "unknown", Target: End}} }, false},
		{"invalid operator", func(s *Survey) { s.Questions[0].Rules = []Rule{{Operator: "gt", Value: "a", Target: End}} }, false},
		{"text rule forbidden", func(s *Survey) { s.Questions[1].Rules = []Rule{{Operator: "eq", Value: "yes", Target: End}} }, false},
		{"numeric integer rule", func(s *Survey) { s.Questions[2].Rules = []Rule{{Operator: "gte", Value: int32(18), Target: End}} }, true},
		{"numeric string rule", func(s *Survey) { s.Questions[2].Rules = []Rule{{Operator: "gte", Value: "18", Target: End}} }, false},
		{"numeric nonfinite rule", func(s *Survey) { s.Questions[2].Rules = []Rule{{Operator: "gte", Value: math.Inf(1), Target: End}} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testSurvey()
			tt.change(s)
			if err := ValidateSurvey(s); (err == nil) != tt.valid {
				t.Fatalf("ValidateSurvey = %v, valid = %v", err, tt.valid)
			}
		})
	}
	if ValidateSurvey(nil) == nil {
		t.Fatal("nil survey accepted")
	}
	s := testSurvey()
	s.Questions = make([]Question, 101)
	if ValidateSurvey(s) == nil {
		t.Fatal("101 questions accepted")
	}
}

func TestValidateAnswer(t *testing.T) {
	single := choiceQuestion("single")
	multiple := choiceQuestion("multiple")
	multiple.Validation = Validation{MinSelect: intPtr(1), MaxSelect: intPtr(2)}
	text := Question{Type: "text", Required: true, Validation: Validation{MinLength: intPtr(2), MaxLength: intPtr(3)}}
	number := Question{Type: "number", Required: true, Validation: Validation{Integer: true, Min: floatPtr(0), Max: floatPtr(120)}}
	tests := []struct {
		name  string
		q     Question
		value any
		want  any
		valid bool
	}{
		{"single", single, "a", "a", true},
		{"single nonexistent", single, "d", nil, false},
		{"single wrong type", single, []string{"a"}, nil, false},
		{"required nil", single, nil, nil, false},
		{"optional nil", Question{Type: "text"}, nil, nil, true},
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
		{"text hard limit", Question{Type: "text"}, strings.Repeat("文", 10001), nil, false},
		{"integer", number, float64(18), float64(18), true},
		{"integer BSON", number, int64(18), float64(18), true},
		{"fraction forbidden", number, 18.5, nil, false},
		{"below bound", number, -1.0, nil, false},
		{"above bound", number, 121.0, nil, false},
		{"string number", number, "18", nil, false},
		{"NaN", number, math.NaN(), nil, false},
		{"infinity", number, math.Inf(-1), nil, false},
		{"decimal", Question{Type: "number"}, 1.25, 1.25, true},
		{"safe integer", Question{Type: "number"}, float64(maxSafeInteger), float64(maxSafeInteger), true},
		{"unsafe integer", Question{Type: "number"}, float64(maxSafeInteger + 1), nil, false},
		{"unsafe negative integer", Question{Type: "number"}, -float64(maxSafeInteger + 1), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateAnswer(tt.q, tt.value)
			if (err == nil) != tt.valid || (tt.valid && !reflect.DeepEqual(got, tt.want)) {
				t.Fatalf("got (%#v, %v), want (%#v, valid=%v)", got, err, tt.want, tt.valid)
			}
		})
	}
}

func TestNextQuestion(t *testing.T) {
	s := testSurvey()
	s.Questions[0].Rules = []Rule{
		{Operator: "eq", Value: "a", Target: "q3"},
		{Operator: "eq", Value: "a", Target: End},
		{Operator: "eq", Value: "b", Target: End},
	}
	for _, tt := range []struct {
		id     string
		answer any
		want   string
	}{
		{"q1", "a", "q3"}, {"q1", "b", End}, {"q1", "c", "q2"},
		{"q1", nil, "q2"}, {"q2", "hello", "q3"}, {"q3", 12.0, End},
	} {
		if got := NextQuestion(s, tt.id, tt.answer); got != tt.want {
			t.Fatalf("NextQuestion(%s, %v) = %s, want %s", tt.id, tt.answer, got, tt.want)
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
		if got := matches("number", Rule{Operator: tt.op, Value: int32(18)}, tt.answer); got != tt.match {
			t.Errorf("%v %s 18 = %v, want %v", tt.answer, tt.op, got, tt.match)
		}
	}
}

func TestMultipleRulesAndNamedArrays(t *testing.T) {
	// BSON arrays are named slices of interface values as well.
	type bsonArray []any
	q := choiceQuestion("multiple")
	q.Rules = []Rule{
		{Operator: "contains_all", Value: bsonArray{"a", "b"}, Target: End},
		{Operator: "contains", Value: "c", Target: End},
	}
	s := testSurvey()
	s.Questions[0] = q
	if err := ValidateSurvey(s); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		answer any
		want   string
	}{
		{bsonArray{"a", "b"}, End}, {[]string{"b", "a"}, End},
		{[]any{"a"}, "q2"}, {[]string{"c"}, End},
	} {
		if got := NextQuestion(s, "q1", tt.answer); got != tt.want {
			t.Errorf("NextQuestion(%v) = %s, want %s", tt.answer, got, tt.want)
		}
	}
	for _, invalid := range []any{[]string{}, []string{"a", "a"}, []any{"a", 1}, []string{"z"}} {
		s.Questions[0].Rules[0].Value = invalid
		if ValidateSurvey(s) == nil {
			t.Errorf("invalid contains_all value accepted: %#v", invalid)
		}
	}
}

func TestValidateSubmission(t *testing.T) {
	s := testSurvey()
	s.Questions[0].Rules = []Rule{{Operator: "eq", Value: "a", Target: "q3"}, {Operator: "eq", Value: "b", Target: End}}
	answers, path, err := ValidateSubmission(s, map[string]any{"q1": "a", "q3": int32(23)})
	if err != nil || !reflect.DeepEqual(path, []string{"q1", "q3"}) || answers["q3"] != float64(23) {
		t.Fatalf("submission = %#v, %#v, %v", answers, path, err)
	}
	_, path, err = ValidateSubmission(s, map[string]any{"q1": "b"})
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
		if _, _, err := ValidateSubmission(s, bad); err == nil {
			t.Errorf("bad answers accepted: %#v", bad)
		}
	}
	s.Questions[0].Required = false
	_, path, err = ValidateSubmission(s, map[string]any{"q2": "描述", "q3": 0.0})
	if err != nil || !reflect.DeepEqual(path, []string{"q1", "q2", "q3"}) {
		t.Fatalf("optional = %v, %v", path, err)
	}
	s.Questions = nil
	if _, _, err := ValidateSubmission(s, nil); err == nil {
		t.Fatal("empty survey accepted")
	}
}
