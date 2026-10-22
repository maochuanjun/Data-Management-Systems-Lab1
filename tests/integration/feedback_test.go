package integration_test

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"questionnaire/internal/domain"
	"questionnaire/internal/web"
	"questionnaire/tests/support"
)

func feedbackBlocked(t *testing.T, client integrationHTTP, path string, want bool) {
	t.Helper()
	data := client.must(t, "GET", path, nil, 200)
	result := integrationDecode[struct {
		SubmissionBlocked *bool `json:"submissionBlocked"`
	}](t, data)
	if result.SubmissionBlocked == nil || *result.SubmissionBlocked != want {
		t.Fatalf("submissionBlocked=%v, want %v; body=%s", result.SubmissionBlocked, want, data)
	}
	for _, private := range []string{`"ownerId"`, `"userId"`, `"dedupeKey"`, `"answers"`, `"submittedAt"`} {
		if strings.Contains(string(data), private) {
			t.Fatalf("public status leaked %s: %s", private, data)
		}
	}
}

func TestFeedbackSubmissionPrecheckIdentityAndRepeatPolicy(t *testing.T) {
	owner, participant, other := libraryClients(t)
	for _, repeat := range []bool{false, true} {
		for _, anonymous := range []bool{false, true} {
			t.Run(fmt.Sprintf("repeat=%t/anonymous=%t", repeat, anonymous), func(t *testing.T) {
				survey := integrationDecode[domain.Survey](t, owner.must(t, "POST", "/api/surveys", domain.Survey{
					Title: "早期提交状态", AllowAnonymous: true, AllowRepeat: repeat,
					Questions: []domain.Question{{ID: "answer", Title: "填写数字", Type: "fill", FillType: "number", Required: true}},
				}, 201))
				owner.must(t, "POST", "/api/surveys/"+survey.ID+"/publish", nil, 200)
				path := "/api/public/surveys/" + survey.ID
				feedbackBlocked(t, participant, path, false)
				participant.must(t, "POST", path+"/responses", integrationAnswers(anonymous, map[string]any{"answer": 42}), 201)
				feedbackBlocked(t, participant, path, !repeat)
				feedbackBlocked(t, other, path, false)
				feedbackBlocked(t, owner, path, false)
				status := 409
				if repeat {
					status = 201
				}
				// Switching anonymity never changes the identity used for no-repeat.
				participant.must(t, "POST", path+"/responses", integrationAnswers(!anonymous, map[string]any{"answer": 43}), status)
				feedbackBlocked(t, participant, path, !repeat)
			})
		}
	}
}

func TestFeedbackLegacySubmissionPrecheck(t *testing.T) {
	fixture := support.NewMongo(t)
	survey := legacyDedupeSurvey(t, fixture, "feedback-legacy", false)
	const userID = "legacy-feedback-account"
	legacy := bson.M{"_id": "legacy-feedback-response", "surveyId": survey.ID, "schemaVersion": 1,
		"anonymous": true, "userId": userID, "answers": bson.M{"q": 12.0}, "path": bson.A{"q"}, "submittedAt": time.Unix(1000, 0).UTC()}
	if _, err := fixture.DB.Collection("responses").InsertOne(fixture.Context, legacy); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Store.Init(fixture.Context); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	client := legacyDedupeClient(t, fixture, server.URL, userID)
	feedbackBlocked(t, client, "/api/public/surveys/"+survey.ID, true)
	client.must(t, "POST", "/api/public/surveys/"+survey.ID+"/responses", integrationAnswers(false, map[string]any{"q": 13}), 409)
	var got bson.M
	if err := fixture.DB.Collection("responses").FindOne(fixture.Context, bson.M{"_id": legacy["_id"]}).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["dedupeKey"] != legacyDedupeKey(survey.ID, userID) || got["anonymous"] != true || got["userId"] != userID {
		t.Fatalf("legacy record lost its policy or identity: %#v", got)
	}
}

func TestFeedbackChoiceConfigurationAndPublishedCompatibility(t *testing.T) {
	fixture := support.NewMongo(t)
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	owner := newIntegrationClient(t, server.URL)
	owner.must(t, "POST", "/api/register", map[string]string{"username": "feedback_owner", "password": "test-password-123"}, 201)
	for _, kind := range []string{"single", "multiple"} {
		t.Run(kind, func(t *testing.T) {
			q := domain.Question{ID: "hidden-generated-id", Title: "选择", Type: kind, Required: true,
				Options: []domain.Option{{ID: "left", Label: "相同"}, {ID: "right", Label: " \t相同\u3000"}}}
			bad := domain.Survey{Title: "重复选项", AllowRepeat: true, Questions: []domain.Question{q}}
			expectPosition := func(data []byte) {
				t.Helper()
				message := string(data)
				if !strings.Contains(message, "第 1 题") || !strings.Contains(message, "第 2 个选项") || strings.Contains(message, q.ID) {
					t.Fatalf("error should locate question and option: %s", data)
				}
			}
			expectPosition(owner.must(t, "POST", "/api/surveys", bad, 400))
			expectPosition(owner.must(t, "POST", "/api/questions", map[string]any{"question": q}, 400))
			valid := q
			valid.Options = append([]domain.Option(nil), q.Options...)
			valid.Options[1].Label = "不同"
			template := libraryCreate(t, owner, valid)
			versionPath := "/api/questions/" + template.ID
			expectPosition(owner.must(t, "POST", versionPath+"/versions", map[string]any{"baseVersionId": template.LatestVersion.ID, "question": q}, 400))
			// Seed content accepted by the older application. History restoration
			// must copy it faithfully, while new survey configuration stays strict.
			legacy := *template.LatestVersion
			legacy.Question.Options = append([]domain.Option(nil), q.Options...)
			if _, err := fixture.DB.Collection("question_versions").UpdateOne(fixture.Context, bson.M{"_id": legacy.ID}, bson.M{"$set": bson.M{"question": legacy.Question}}); err != nil {
				t.Fatal(err)
			}
			restored := integrationDecode[domain.QuestionVersion](t, owner.must(t, "POST", versionPath+"/restore", map[string]any{"versionId": legacy.ID}, 201))
			if restored.Version != 2 || restored.RestoredFromVersionID != legacy.ID || restored.ParentVersionID != legacy.ID || !reflect.DeepEqual(restored.Question, legacy.Question) {
				t.Fatalf("restore must retain historical content and lineage: %#v", restored)
			}
			expectPosition(owner.must(t, "POST", "/api/surveys", domain.Survey{Title: "恢复内容仍需修正", Questions: []domain.Question{libraryOccurrence(restored, q.ID)}}, 400))
			fixed := libraryVersion(t, owner, template.ID, restored, valid)
			fixedSurvey := librarySurvey(t, owner, "修复后的新版本", libraryOccurrence(fixed, "fixed-question"))
			owner.must(t, "POST", "/api/surveys/"+fixedSurvey.ID+"/publish", nil, 200)
			unchanged := integrationDecode[domain.QuestionVersion](t, owner.must(t, "GET", "/api/question-versions/"+legacy.ID, nil, 200))
			if !reflect.DeepEqual(unchanged.Question, legacy.Question) || unchanged.Version != legacy.Version {
				t.Fatal("historical version changed during restore or editing")
			}
			draft := integrationDecode[domain.Survey](t, owner.must(t, "POST", "/api/surveys", domain.Survey{Title: "原草稿"}, 201))
			draft.Questions = []domain.Question{q}
			expectPosition(owner.must(t, "PUT", "/api/surveys/"+draft.ID, draft, 400))
			// An older draft may already contain these labels: publish must reject it.
			if _, err := fixture.DB.Collection("surveys").UpdateOne(fixture.Context, bson.M{"_id": draft.ID}, bson.M{"$set": bson.M{"questions": []domain.Question{q}}}); err != nil {
				t.Fatal(err)
			}
			expectPosition(owner.must(t, "POST", "/api/surveys/"+draft.ID+"/publish", nil, 400))
			// Previously published snapshots stay answerable with their original IDs.
			if _, err := fixture.DB.Collection("surveys").UpdateOne(fixture.Context, bson.M{"_id": draft.ID}, bson.M{"$set": bson.M{"status": "published", "allowRepeat": true}}); err != nil {
				t.Fatal(err)
			}
			public := "/api/public/surveys/" + draft.ID
			var answer any = "right"
			if kind == "multiple" {
				answer = []string{"left", "right"}
			}
			owner.must(t, "POST", public+"/next", map[string]any{"questionId": q.ID, "answer": answer}, 200)
			owner.must(t, "POST", public+"/responses", integrationAnswers(false, map[string]any{q.ID: answer}), 201)
			persisted := integrationDecode[domain.Survey](t, owner.must(t, "GET", "/api/surveys/"+draft.ID, nil, 200))
			if !reflect.DeepEqual(persisted.Questions, []domain.Question{q}) {
				t.Fatal("published snapshot changed")
			}
			data := owner.must(t, "POST", public+"/next", map[string]any{"questionId": q.ID, "answer": "not-an-option"}, 400)
			if !strings.Contains(string(data), "第 1 题") || strings.Contains(string(data), q.ID) {
				t.Fatalf("next error lacks readable position: %s", data)
			}
		})
	}
}
