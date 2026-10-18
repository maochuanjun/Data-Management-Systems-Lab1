package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"questionnaire/internal/domain"
	"questionnaire/tests/support"
)

func legacyDedupeKey(surveyID, userID string) string {
	hash := sha256.Sum256([]byte(surveyID + ":" + userID))
	return hex.EncodeToString(hash[:])
}

// Restore an authenticated session for the identity recorded by the old fixture;
// registering a new account would test a different user and miss this regression.
func legacyDedupeClient(t *testing.T, fixture *support.Mongo, serverURL, userID string) integrationHTTP {
	t.Helper()
	user := &domain.User{
		ID: userID, Username: "legacy_respondent",
		PasswordHash: "session-only-test-fixture", CreatedAt: time.Now().UTC(),
	}
	if err := fixture.Store.CreateUser(fixture.Context, user); err != nil {
		t.Fatal(err)
	}
	const token = "legacy-dedupe-test-session"
	hash := sha256.Sum256([]byte(token))
	if err := fixture.Store.CreateSession(fixture.Context, hex.EncodeToString(hash[:]), user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	client := newIntegrationClient(t, serverURL)
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	client.client.Jar.SetCookies(parsed, []*http.Cookie{{Name: "session", Value: token, Path: "/"}})
	return client
}

func legacyDedupeSurvey(t *testing.T, fixture *support.Mongo, id string, allowRepeat bool) *domain.Survey {
	t.Helper()
	survey := &domain.Survey{
		SchemaVersion: 1, ID: id, OwnerID: "legacy-owner", Title: "Legacy deduplication",
		AllowAnonymous: true, AllowRepeat: allowRepeat, Status: "published", Revision: 1,
		CreatedAt: time.Unix(1, 0).UTC(),
		Questions: []domain.Question{{ID: "q", Title: "Number", Type: "fill", FillType: "number"}},
	}
	if err := fixture.Store.CreateSurvey(fixture.Context, survey); err != nil {
		t.Fatal(err)
	}
	return survey
}

func TestLegacyDedupeMigrationPreservesRecordsAndPolicies(t *testing.T) {
	fixture := support.NewMongo(t)
	noRepeat := legacyDedupeSurvey(t, fixture, "legacy-no-repeat", false)
	repeat := legacyDedupeSurvey(t, fixture, "legacy-allow-repeat", true)
	responses := fixture.DB.Collection("responses")
	base := time.Unix(1000, 0).UTC()
	seeds := []struct {
		id, survey, user string
		anonymous        bool
		includeUser      bool
		seconds          int
		key              string
	}{
		// Reversed lexical insertion and different timestamps make the retained
		// key holder deterministic even with pre-existing duplicate answers.
		{"duplicate-z", noRepeat.ID, "duplicated-user", false, true, 0, ""},
		{"000-later", noRepeat.ID, "duplicated-user", false, true, 1, ""},
		{"duplicate-a", noRepeat.ID, "duplicated-user", false, true, 0, ""},
		{"already-keyed", noRepeat.ID, "keyed-user", false, true, 2, legacyDedupeKey(noRepeat.ID, "keyed-user")},
		{"unkeyed-older", noRepeat.ID, "keyed-user", false, true, -2, ""},
		{"anonymous-with-user", noRepeat.ID, "identified-anonymous", true, true, 0, ""},
		{"anonymous-no-user", noRepeat.ID, "", true, false, 0, ""},
		{"anonymous-empty-user", noRepeat.ID, "", true, true, 0, ""},
		{"repeat-known", repeat.ID, "repeat-user", false, true, 0, ""},
		{"repeat-anonymous-known", repeat.ID, "repeat-user", true, true, 0, ""},
		{"current-anonymous-keyed", noRepeat.ID, "", true, false, 2, legacyDedupeKey(noRepeat.ID, "currently-anonymous")},
		{"old-named-current-anonymous", noRepeat.ID, "currently-anonymous", false, true, -2, ""},
	}
	for i, seed := range seeds {
		document := bson.M{
			"_id": seed.id, "surveyId": seed.survey, "schemaVersion": 1,
			"anonymous": seed.anonymous, "answers": bson.M{"q": float64(i)},
			"path": bson.A{"q"}, "submittedAt": base.Add(time.Duration(seed.seconds) * time.Second),
		}
		if seed.includeUser {
			document["userId"] = seed.user
		}
		if seed.key != "" {
			document["dedupeKey"] = seed.key
		}
		if _, err := responses.InsertOne(fixture.Context, document); err != nil {
			t.Fatal(err)
		}
	}
	readAll := func() map[string]bson.M {
		t.Helper()
		cursor, err := responses.Find(fixture.Context, bson.M{})
		if err != nil {
			t.Fatal(err)
		}
		defer cursor.Close(fixture.Context)
		var documents []bson.M
		if err := cursor.All(fixture.Context, &documents); err != nil {
			t.Fatal(err)
		}
		result := make(map[string]bson.M, len(documents))
		for _, document := range documents {
			result[document["_id"].(string)] = document
		}
		return result
	}
	before := readAll()
	beforeStats, err := fixture.Store.Stats(fixture.Context, noRepeat, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise simultaneous initialization including index creation, rather than
	// only updates against indexes already installed by NewMongo.
	if err := responses.Indexes().DropAll(fixture.Context); err != nil {
		t.Fatal(err)
	}
	const initializers = 3
	var workers sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, initializers)
	for range initializers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- fixture.Store.Init(fixture.Context)
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent Init failed: %v", err)
		}
	}
	wantKeys := map[string]string{
		"duplicate-a":             legacyDedupeKey(noRepeat.ID, "duplicated-user"),
		"already-keyed":           legacyDedupeKey(noRepeat.ID, "keyed-user"),
		"anonymous-with-user":     legacyDedupeKey(noRepeat.ID, "identified-anonymous"),
		"current-anonymous-keyed": legacyDedupeKey(noRepeat.ID, "currently-anonymous"),
	}
	for pass := 0; pass < 2; pass++ {
		if err := fixture.Store.Init(fixture.Context); err != nil {
			t.Fatalf("idempotent Init %d: %v", pass, err)
		}
		after := readAll()
		if len(after) != len(before) {
			t.Fatalf("migration changed response count: before=%d after=%d", len(before), len(after))
		}
		for id, original := range before {
			want := make(bson.M, len(original)+1)
			for key, value := range original {
				want[key] = value
			}
			if key, present := wantKeys[id]; present {
				want["dedupeKey"] = key
			}
			if !reflect.DeepEqual(after[id], want) {
				t.Fatalf("response %s changed beyond its expected auxiliary key: want=%v got=%v", id, want, after[id])
			}
		}
		afterStats, err := fixture.Store.Stats(fixture.Context, noRepeat, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(beforeStats, afterStats) {
			t.Fatalf("migration changed statistics or raw answers: before=%v after=%v", beforeStats, afterStats)
		}
	}
	t.Logf("preserved all %d responses; deterministic holder, existing keys, concurrent/repeated Init, statistics, repeat-enabled and unknown-identity records verified", len(before))
}

func TestLegacyDedupeMigrationReturnsWriteFailures(t *testing.T) {
	fixture := support.NewMongo(t)
	survey := legacyDedupeSurvey(t, fixture, "legacy-invalid-response", false)
	// Simulate a historical record predating mandatory path validation. Startup
	// must surface the failed backfill instead of claiming deduplication is ready.
	document := bson.M{
		"_id": "legacy-invalid", "surveyId": survey.ID, "schemaVersion": 1,
		"anonymous": false, "userId": "legacy-user", "answers": bson.M{"q": 1.0},
		"submittedAt": time.Unix(1000, 0).UTC(),
	}
	if _, err := fixture.DB.Collection("responses").InsertOne(fixture.Context, document,
		options.InsertOne().SetBypassDocumentValidation(true)); err != nil {
		t.Fatal(err)
	}
	err := fixture.Store.Init(fixture.Context)
	var writeErr mongo.WriteException
	if err == nil || !strings.Contains(err.Error(), "backfill legacy deduplication") ||
		!errors.As(err, &writeErr) || !writeErr.HasErrorCode(121) {
		t.Fatalf("Init must expose the schema write failure: %v", err)
	}
	var retained bson.M
	if readErr := fixture.DB.Collection("responses").FindOne(fixture.Context, bson.M{"_id": "legacy-invalid"}).Decode(&retained); readErr != nil {
		t.Fatal(readErr)
	}
	if _, present := retained["dedupeKey"]; present {
		t.Fatalf("failed backfill unexpectedly changed the document: %v", retained)
	}
	t.Logf("Init correctly rejected incomplete historical data: %v", err)
}
