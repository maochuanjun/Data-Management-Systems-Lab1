package domain

import "time"

type User struct {
	ID           string    `json:"id" bson:"_id"`
	Username     string    `json:"username" bson:"username"`
	PasswordHash string    `json:"-" bson:"passwordHash"`
	CreatedAt    time.Time `json:"createdAt" bson:"createdAt"`
}

type Survey struct {
	SchemaVersion  int        `json:"schemaVersion" bson:"schemaVersion"`
	ID             string     `json:"id" bson:"_id"`
	OwnerID        string     `json:"ownerId" bson:"ownerId"`
	Title          string     `json:"title" bson:"title"`
	Description    string     `json:"description" bson:"description"`
	AllowAnonymous bool       `json:"allowAnonymous" bson:"allowAnonymous"`
	AllowRepeat    bool       `json:"allowRepeat" bson:"allowRepeat"`
	Archived       bool       `json:"archived" bson:"archived,omitempty"`
	Deadline       *time.Time `json:"deadline" bson:"deadline"`
	Status         string     `json:"status" bson:"status"`
	Questions      []Question `json:"questions" bson:"questions"`
	CreatedAt      time.Time  `json:"createdAt" bson:"createdAt"`
	Revision       int        `json:"revision" bson:"revision"`
}

type Question struct {
	SourceQuestionID string     `json:"sourceQuestionId,omitempty" bson:"sourceQuestionId,omitempty"`
	SourceVersionID  string     `json:"sourceVersionId,omitempty" bson:"sourceVersionId,omitempty"`
	ID               string     `json:"id" bson:"id"`
	Title            string     `json:"title" bson:"title"`
	Type             string     `json:"type" bson:"type"`                             // single, multiple, fill (legacy: text, number)
	FillType         string     `json:"fillType,omitempty" bson:"fillType,omitempty"` // text or number for fill
	Required         bool       `json:"required" bson:"required"`
	Options          []Option   `json:"options" bson:"options"`
	Validation       Validation `json:"validation" bson:"validation"`
	Rules            []Rule     `json:"rules" bson:"rules"`
}
type Option struct {
	ID    string `json:"id" bson:"id"`
	Label string `json:"label" bson:"label"`
}
type Validation struct {
	MinSelect *int     `json:"minSelect,omitempty" bson:"minSelect,omitempty"`
	MaxSelect *int     `json:"maxSelect,omitempty" bson:"maxSelect,omitempty"`
	MinLength *int     `json:"minLength,omitempty" bson:"minLength,omitempty"`
	MaxLength *int     `json:"maxLength,omitempty" bson:"maxLength,omitempty"`
	Min       *float64 `json:"min,omitempty" bson:"min,omitempty"`
	Max       *float64 `json:"max,omitempty" bson:"max,omitempty"`
	Integer   bool     `json:"integer" bson:"integer"`
}
type Rule struct {
	Operator string `json:"operator" bson:"operator"` // eq, contains, contains_all, gt, gte, lt, lte
	Value    any    `json:"value" bson:"value"`
	Target   string `json:"target" bson:"target"` // question ID or END
}
type Response struct {
	SchemaVersion int            `json:"schemaVersion" bson:"schemaVersion"`
	ID            string         `json:"id" bson:"_id"`
	SurveyID      string         `json:"surveyId" bson:"surveyId"`
	UserID        string         `json:"userId,omitempty" bson:"userId,omitempty"`
	Anonymous     bool           `json:"anonymous" bson:"anonymous"`
	Answers       map[string]any `json:"answers" bson:"answers"`
	Path          []string       `json:"path" bson:"path"`
	SubmittedAt   time.Time      `json:"submittedAt" bson:"submittedAt"`
}
