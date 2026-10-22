package domain

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"
)

const End = "END"
const maxSafeInteger = 9007199254740991

var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// answerType treats fill subtypes as their validation kind. The old text/number
// question types remain readable for first-stage saved data.
func answerType(q Question) string {
	if q.Type == "fill" {
		return q.FillType
	}
	return q.Type
}

// ValidateSurvey checks the schema, including forward-only conditional jumps.
// An empty question list is allowed while a survey is being drafted.
func ValidateSurvey(s *Survey) error {
	return validateSurvey(s, true)
}

// Unique option labels are an authoring rule. Submission still validates the
// saved schema and answer IDs, but must not invalidate older published snapshots
// whose distinct option IDs happen to have the same display text.
func validateSurvey(s *Survey, uniqueOptionLabels bool) error {
	if s == nil {
		return fmt.Errorf("问卷不能为空")
	}
	if strings.TrimSpace(s.Title) == "" || utf8.RuneCountInString(s.Title) > 200 {
		return fmt.Errorf("问卷标题不能为空且不能超过 200 个字符")
	}
	if len(s.Questions) > 100 {
		return fmt.Errorf("问卷不能超过 100 道题")
	}
	positions := make(map[string]int, len(s.Questions))
	for i, q := range s.Questions {
		if !validID.MatchString(q.ID) || q.ID == End {
			return fmt.Errorf("第 %d 题的标识无效，请重新添加该题", i+1)
		}
		if previous, exists := positions[q.ID]; exists {
			return fmt.Errorf("第 %d 题与第 %d 题的标识重复，请重新添加该题", i+1, previous+1)
		}
		positions[q.ID] = i
	}
	for i, q := range s.Questions {
		if err := validateQuestion(q, uniqueOptionLabels); err != nil {
			return fmt.Errorf("第 %d 题：%w", i+1, err)
		}
		for _, rule := range q.Rules {
			if rule.Target != End {
				position, exists := positions[rule.Target]
				if !exists || position <= i {
					return fmt.Errorf("第 %d 题的跳转目标必须为后续题目或结束问卷", i+1)
				}
			}
			if err := validateRule(q, rule); err != nil {
				return fmt.Errorf("第 %d 题的跳转规则：%w", i+1, err)
			}
		}
	}
	return nil
}

func validateQuestion(q Question, uniqueOptionLabels bool) error {
	if strings.TrimSpace(q.Title) == "" || utf8.RuneCountInString(q.Title) > 500 {
		return fmt.Errorf("标题不能为空且不能超过 500 个字符")
	}
	if q.Type == "fill" && q.FillType != "text" && q.FillType != "number" {
		return fmt.Errorf("填空题必须设置文本或数字子类型")
	}
	if q.Type != "fill" && q.FillType != "" {
		return fmt.Errorf("只有填空题可以设置填空子类型")
	}
	kind := answerType(q)
	switch kind {
	case "single", "multiple":
		if len(q.Options) < 2 || len(q.Options) > 100 {
			return fmt.Errorf("选择题需要 2 至 100 个选项")
		}
		seen := make(map[string]int, len(q.Options))
		labels := make(map[string]int, len(q.Options))
		for i, option := range q.Options {
			if !validID.MatchString(option.ID) {
				return fmt.Errorf("第 %d 个选项的标识无效，请重新添加该选项", i+1)
			}
			if previous, exists := seen[option.ID]; exists {
				return fmt.Errorf("第 %d 个选项与第 %d 个选项的标识重复，请重新添加该选项", i+1, previous+1)
			}
			label := strings.TrimSpace(option.Label)
			if label == "" {
				return fmt.Errorf("第 %d 个选项的文本不能为空", i+1)
			}
			if previous, exists := labels[label]; uniqueOptionLabels && exists {
				return fmt.Errorf("第 %d 个选项与第 %d 个选项的文本重复", i+1, previous+1)
			}
			seen[option.ID], labels[label] = i, i
		}
	case "text", "number":
		if len(q.Options) != 0 {
			return fmt.Errorf("只有选择题可以设置选项")
		}
	default:
		return fmt.Errorf("不支持的题目类型: %s", q.Type)
	}
	v := q.Validation
	if q.Type != "multiple" && (v.MinSelect != nil || v.MaxSelect != nil) {
		return fmt.Errorf("只有多选题可以限制选择数量")
	}
	if kind != "text" && (v.MinLength != nil || v.MaxLength != nil) {
		return fmt.Errorf("只有文本题可以限制文本长度")
	}
	if kind != "number" && (v.Min != nil || v.Max != nil || v.Integer) {
		return fmt.Errorf("只有数字题可以设置数值限制")
	}
	if err := validateIntRange(v.MinSelect, v.MaxSelect, len(q.Options)); err != nil {
		return fmt.Errorf("选择数量限制: %w", err)
	}
	if err := validateIntRange(v.MinLength, v.MaxLength, 10000); err != nil {
		return fmt.Errorf("文本长度限制: %w", err)
	}
	if (v.Min != nil && !finite(*v.Min)) || (v.Max != nil && !finite(*v.Max)) ||
		(v.Min != nil && v.Max != nil && *v.Min > *v.Max) {
		return fmt.Errorf("数值上下限无效")
	}
	if q.Required {
		if q.Type == "multiple" && v.MaxSelect != nil && *v.MaxSelect == 0 {
			return fmt.Errorf("必答多选题必须允许至少选择一个选项")
		}
		if kind == "text" && v.MaxLength != nil && *v.MaxLength == 0 {
			return fmt.Errorf("必答文本题必须允许至少一个字符")
		}
		if kind == "number" {
			// Beyond this interval every representable float64 is an unsafe
			// integer, which ValidateAnswer rejects even for decimal questions.
			lower, upper := -float64(maxSafeInteger), float64(maxSafeInteger)
			if v.Min != nil {
				lower = math.Max(lower, *v.Min)
			}
			if v.Max != nil {
				upper = math.Min(upper, *v.Max)
			}
			if v.Integer {
				lower, upper = math.Ceil(lower), math.Floor(upper)
			}
			if lower > upper {
				return fmt.Errorf("必答数字题的范围内没有可接受的数值")
			}
		}
	}
	return nil
}

func validateIntRange(min, max *int, limit int) error {
	if (min != nil && (*min < 0 || *min > limit)) ||
		(max != nil && (*max < 0 || *max > limit)) ||
		(min != nil && max != nil && *min > *max) {
		return fmt.Errorf("上下限必须在 0 至 %d 之间且下限不超过上限", limit)
	}
	return nil
}

func validateRule(q Question, r Rule) error {
	switch answerType(q) {
	case "single":
		value, ok := r.Value.(string)
		if r.Operator == "eq" && ok && hasOption(q, value) {
			return nil
		}
	case "multiple":
		if r.Operator == "contains" {
			value, ok := r.Value.(string)
			if ok && hasOption(q, value) {
				return nil
			}
		}
		if r.Operator == "contains_all" {
			values, ok := stringSlice(r.Value)
			if ok && len(values) > 0 {
				seen := make(map[string]bool, len(values))
				for _, value := range values {
					if seen[value] || !hasOption(q, value) {
						return fmt.Errorf("规则包含重复或不存在的选项")
					}
					seen[value] = true
				}
				return nil
			}
		}
	case "number":
		switch r.Operator {
		case "eq", "gt", "gte", "lt", "lte":
			if value, ok := numeric(r.Value); ok && finite(value) {
				return nil
			}
		}
	}
	return fmt.Errorf("操作符或比较值不适用于当前题型")
}

// ValidateAnswer returns a consistently typed value suitable for storage.
func ValidateAnswer(q Question, value any) (any, error) {
	if value == nil {
		if q.Required {
			return nil, fmt.Errorf("必答题不能为空")
		}
		return nil, nil
	}
	v := q.Validation
	switch answerType(q) {
	case "single":
		selected, ok := value.(string)
		if !ok || !hasOption(q, selected) {
			return nil, fmt.Errorf("请选择一个有效选项")
		}
		return selected, nil
	case "multiple":
		selected, ok := stringSlice(value)
		if !ok {
			return nil, fmt.Errorf("多选答案必须是选项数组")
		}
		seen := make(map[string]bool, len(selected))
		for _, id := range selected {
			if seen[id] || !hasOption(q, id) {
				return nil, fmt.Errorf("选项重复或不存在")
			}
			seen[id] = true
		}
		if (q.Required && len(selected) == 0) ||
			(v.MinSelect != nil && len(selected) < *v.MinSelect) ||
			(v.MaxSelect != nil && len(selected) > *v.MaxSelect) {
			return nil, fmt.Errorf("选择数量不符合要求")
		}
		return selected, nil
	case "text":
		answer, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("文本答案必须是字符串")
		}
		length := utf8.RuneCountInString(answer)
		if (q.Required && strings.TrimSpace(answer) == "") || length > 10000 ||
			(v.MinLength != nil && length < *v.MinLength) ||
			(v.MaxLength != nil && length > *v.MaxLength) {
			return nil, fmt.Errorf("文本为空或长度不符合要求")
		}
		return answer, nil
	case "number":
		answer, ok := numeric(value)
		if !ok || !finite(answer) ||
			(math.Trunc(answer) == answer && math.Abs(answer) > maxSafeInteger) ||
			(v.Integer && math.Trunc(answer) != answer) ||
			(v.Min != nil && answer < *v.Min) || (v.Max != nil && answer > *v.Max) {
			return nil, fmt.Errorf("数字类型、精度或范围不符合要求")
		}
		return answer, nil
	default:
		return nil, fmt.Errorf("不支持的题目类型")
	}
}

// NextQuestion evaluates rules in order; the first match wins.
func NextQuestion(s *Survey, questionID string, answer any) string {
	if s == nil {
		return End
	}
	for i, q := range s.Questions {
		if q.ID != questionID {
			continue
		}
		if answer != nil {
			for _, rule := range q.Rules {
				if matches(q, rule, answer) {
					return rule.Target
				}
			}
		}
		if i+1 < len(s.Questions) {
			return s.Questions[i+1].ID
		}
		return End
	}
	return End
}

func matches(question Question, rule Rule, answer any) bool {
	switch answerType(question) {
	case "single":
		actual, aok := answer.(string)
		expected, eok := rule.Value.(string)
		return rule.Operator == "eq" && aok && eok && actual == expected
	case "multiple":
		selected, ok := stringSlice(answer)
		if !ok {
			return false
		}
		contains := func(value string) bool {
			for _, id := range selected {
				if value == id {
					return true
				}
			}
			return false
		}
		if rule.Operator == "contains" {
			value, ok := rule.Value.(string)
			return ok && contains(value)
		}
		if rule.Operator == "contains_all" {
			values, ok := stringSlice(rule.Value)
			if !ok || len(values) == 0 {
				return false
			}
			for _, value := range values {
				if !contains(value) {
					return false
				}
			}
			return true
		}
	case "number":
		actual, aok := numeric(answer)
		expected, eok := numeric(rule.Value)
		if !aok || !eok || !finite(actual) || !finite(expected) {
			return false
		}
		switch rule.Operator {
		case "eq":
			return actual == expected
		case "gt":
			return actual > expected
		case "gte":
			return actual >= expected
		case "lt":
			return actual < expected
		case "lte":
			return actual <= expected
		}
	}
	return false
}

// ValidateSubmission derives the actual path on the server, never trusting a
// client-provided path or allowing answers for questions that were skipped.
func ValidateSubmission(s *Survey, answers map[string]any) (map[string]any, []string, error) {
	if err := validateSurvey(s, false); err != nil {
		return nil, nil, err
	}
	if len(s.Questions) == 0 {
		return nil, nil, fmt.Errorf("空问卷不能提交")
	}
	positions := make(map[string]int, len(s.Questions))
	for i, q := range s.Questions {
		positions[q.ID] = i
	}
	normalized := make(map[string]any)
	visited := make(map[string]bool)
	path := make([]string, 0, len(s.Questions))
	for id := s.Questions[0].ID; id != End; {
		position := positions[id]
		q := s.Questions[position]
		answer, err := ValidateAnswer(q, answers[id])
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 题：%w", position+1, err)
		}
		visited[id] = true
		path = append(path, id)
		if _, supplied := answers[id]; supplied {
			normalized[id] = answer
		}
		id = NextQuestion(s, id, answer)
	}
	for id := range answers {
		if _, exists := positions[id]; !exists {
			return nil, nil, fmt.Errorf("答案包含不存在的题目")
		}
	}
	for i, q := range s.Questions {
		if _, supplied := answers[q.ID]; supplied && !visited[q.ID] {
			return nil, nil, fmt.Errorf("第 %d 题已跳过，不能提交该题答案", i+1)
		}
	}
	return normalized, path, nil
}

func hasOption(q Question, id string) bool {
	for _, option := range q.Options {
		if option.ID == id {
			return true
		}
	}
	return false
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// Reflection accepts named BSON array types without coupling domain to a driver.
func stringSlice(value any) ([]string, bool) {
	if value == nil {
		return nil, false
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	result := make([]string, rv.Len())
	for i := range result {
		text, ok := rv.Index(i).Interface().(string)
		if !ok {
			return nil, false
		}
		result[i] = text
	}
	return result, true
}

func numeric(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	default:
		return 0, false
	}
}
