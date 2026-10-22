package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// A template supplies ownership and sharing; its version documents never change.
type QuestionTemplate struct {
	ID              string           `json:"id" bson:"_id"`
	OwnerID         string           `json:"ownerId" bson:"ownerId"`
	SharedWith      []string         `json:"sharedWith" bson:"sharedWith"`
	SharedUsernames []string         `json:"sharedUsernames,omitempty" bson:"-"`
	NextVersion     int              `json:"nextVersion" bson:"nextVersion"`
	Ready           bool             `json:"-" bson:"ready"`
	CreatedAt       time.Time        `json:"createdAt" bson:"createdAt"`
	LatestVersion   *QuestionVersion `json:"latestVersion,omitempty" bson:"-"`
}

type QuestionVersion struct {
	ID                    string    `json:"id" bson:"_id"`
	QuestionID            string    `json:"questionId" bson:"questionId"`
	Version               int       `json:"version" bson:"version"`
	ParentVersionID       string    `json:"parentVersionId,omitempty" bson:"parentVersionId,omitempty"`
	RestoredFromVersionID string    `json:"restoredFromVersionId,omitempty" bson:"restoredFromVersionId,omitempty"`
	Question              Question  `json:"question" bson:"question"`
	Note                  string    `json:"note" bson:"note"`
	CreatedAt             time.Time `json:"createdAt" bson:"createdAt"`
}

type QuestionBank struct {
	ID          string    `json:"id" bson:"_id"`
	OwnerID     string    `json:"ownerId" bson:"ownerId"`
	Name        string    `json:"name" bson:"name"`
	Description string    `json:"description" bson:"description"`
	VersionIDs  []string  `json:"versionIds" bson:"versionIds"`
	Revision    int       `json:"revision" bson:"revision"`
	CreatedAt   time.Time `json:"createdAt" bson:"createdAt"`
}

// Rules refer to local survey occurrences and remain on the survey snapshot.
func ValidateReusableQuestion(q *Question) error {
	if q == nil {
		return fmt.Errorf("题目不能为空")
	}
	if len(q.Rules) > 0 {
		return fmt.Errorf("题库题目不保存问卷跳转规则，请在问卷内设置跳转")
	}
	if q.SourceQuestionID != "" || q.SourceVersionID != "" {
		return fmt.Errorf("题目版本内容不能嵌套来源引用")
	}
	q.ID = "template"
	if q.Type == "text" || q.Type == "number" {
		q.FillType = q.Type
		q.Type = "fill"
	}
	return ValidateSurvey(&Survey{Title: "题库校验", Questions: []Question{*q}})
}

// ContentEqual ignores only occurrence ID, provenance and local jump rules.
func QuestionContentEqual(a, b Question) bool {
	normalize := func(q Question) []byte {
		q.ID = ""
		q.SourceQuestionID = ""
		q.SourceVersionID = ""
		q.Rules = nil
		if len(q.Options) == 0 {
			q.Options = nil
		}
		if q.Type == "text" || q.Type == "number" {
			q.FillType = q.Type
			q.Type = "fill"
		}
		data, err := json.Marshal(q)
		if err != nil {
			return nil
		}
		return data
	}
	aa, bb := normalize(a), normalize(b)
	return aa != nil && bb != nil && bytes.Equal(aa, bb)
}

func ValidateBank(b *QuestionBank) error {
	if b == nil || strings.TrimSpace(b.Name) == "" || utf8.RuneCountInString(b.Name) > 100 {
		return fmt.Errorf("题库名称不能为空且最多100个字符")
	}
	if utf8.RuneCountInString(b.Description) > 2000 || len(b.VersionIDs) > 1000 {
		return fmt.Errorf("题库说明或题目数量超出限制")
	}
	seen := map[string]bool{}
	for _, id := range b.VersionIDs {
		if !validID.MatchString(id) || seen[id] {
			return fmt.Errorf("题库版本ID无效或重复")
		}
		seen[id] = true
	}
	return nil
}
