package integration_test

import (
	"net/http/httptest"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"questionnaire/internal/domain"
	"questionnaire/internal/web"
	"questionnaire/tests/support"
)

func TestLibrarySharingRejectsStaleRevocationOverwrite(t *testing.T) {
	owner, reader, outsider := libraryClients(t)
	template := libraryCreate(t, owner, domain.Question{Title: "Sharing conflict", Type: "fill", FillType: "text"})
	path := "/api/questions/" + template.ID
	owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{"library_reader"}, "sharingRevision": 0}, 200)
	reader.must(t, "GET", path, nil, 200)
	// Both owner pages observed revision 1. Page A revokes access, then stale
	// page B submits its old recipient list with an additional user.
	owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{}, "sharingRevision": 1}, 200)
	reader.must(t, "GET", path, nil, 404)
	owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{"library_reader", "library_outsider"}, "sharingRevision": 1}, 409)
	reader.must(t, "GET", path, nil, 404)
	outsider.must(t, "GET", path, nil, 404)
}

func TestLibrarySharingRequiresValidRevision(t *testing.T) {
	owner, reader, _ := libraryClients(t)
	template := libraryCreate(t, owner, domain.Question{Title: "Revision validation", Type: "fill", FillType: "text"})
	path := "/api/questions/" + template.ID
	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"missing", map[string]any{"usernames": []string{"library_reader"}}},
		{"null", map[string]any{"usernames": []string{"library_reader"}, "sharingRevision": nil}},
		{"negative", map[string]any{"usernames": []string{"library_reader"}, "sharingRevision": -1}},
		{"fraction", map[string]any{"usernames": []string{"library_reader"}, "sharingRevision": 0.5}},
		{"string", map[string]any{"usernames": []string{"library_reader"}, "sharingRevision": "0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner.must(t, "PUT", path+"/sharing", tc.body, 400)
			reader.must(t, "GET", path, nil, 404)
		})
	}
	// A content version does not invalidate an independently loaded sharing form.
	libraryVersion(t, owner, template.ID, *template.LatestVersion, template.LatestVersion.Question)
	updated := integrationDecode[domain.QuestionTemplate](t, owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{"library_reader"}, "sharingRevision": 0}, 200))
	if updated.SharingRevision != 1 || updated.NextVersion != 2 {
		t.Fatalf("sharing and content revisions must be independent: %#v", updated)
	}
	reader.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{}, "sharingRevision": 1}, 404)
	reader.must(t, "GET", path, nil, 200)
}

func TestLibrarySharingConcurrentUpdatesAndLegacyDocuments(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "new template"
		if legacy {
			name = "legacy template without revision"
		}
		t.Run(name, func(t *testing.T) {
			fixture := support.NewMongo(t)
			server := httptest.NewServer(web.New(fixture.Store, false))
			t.Cleanup(server.Close)
			owner := newIntegrationClient(t, server.URL)
			for _, username := range []string{"sharing_owner", "sharing_first", "sharing_second"} {
				client := newIntegrationClient(t, server.URL)
				if username == "sharing_owner" {
					client = owner
				}
				client.must(t, "POST", "/api/register", map[string]string{"username": username, "password": "test-password-123"}, 201)
			}
			template := libraryCreate(t, owner, domain.Question{Title: "Concurrent sharing", Type: "fill", FillType: "text"})
			path := "/api/questions/" + template.ID
			if legacy {
				if _, err := fixture.DB.Collection("question_templates").UpdateOne(fixture.Context, bson.M{"_id": template.ID}, bson.M{"$unset": bson.M{"sharingRevision": ""}}); err != nil {
					t.Fatal(err)
				}
				if err := fixture.Store.Init(fixture.Context); err != nil {
					t.Fatalf("startup with a legacy template: %v", err)
				}
			}
			initial := integrationDecode[map[string]any](t, owner.must(t, "GET", path, nil, 200))
			if initial["sharingRevision"] != float64(0) {
				t.Fatalf("initial sharing revision must be exposed as zero: %#v", initial)
			}
			type response struct {
				username string
				status   int
				body     []byte
				err      error
			}
			start, results := make(chan struct{}), make(chan response, 2)
			for _, username := range []string{"sharing_first", "sharing_second"} {
				go func() {
					<-start
					status, body, err := owner.send("PUT", path+"/sharing", map[string]any{"usernames": []string{username}, "sharingRevision": 0})
					results <- response{username, status, body, err}
				}()
			}
			close(start)
			statuses := map[int]int{}
			winner := ""
			for range 2 {
				result := <-results
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.status != 200 && result.status != 409 {
					t.Fatalf("unexpected sharing response: %d %s", result.status, result.body)
				}
				statuses[result.status]++
				if result.status == 200 {
					winner = result.username
				}
			}
			if statuses[200] != 1 || statuses[409] != 1 {
				t.Fatalf("concurrent writes must have one winner and one conflict: %v", statuses)
			}
			current := integrationDecode[domain.QuestionTemplate](t, owner.must(t, "GET", path, nil, 200))
			if current.SharingRevision != 1 || current.NextVersion != 1 || len(current.SharedUsernames) != 1 || current.SharedUsernames[0] != winner {
				t.Fatalf("only the winning sharing update must persist: %#v", current)
			}
			owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{}, "sharingRevision": 0}, 409)
			owner.must(t, "PUT", path+"/sharing", map[string]any{"usernames": []string{}, "sharingRevision": 1}, 200)
			persisted, err := fixture.Store.QuestionTemplate(fixture.Context, template.ID)
			if err != nil || len(persisted.SharedWith) != 0 || persisted.SharingRevision != 2 {
				t.Fatalf("subsequent update failed: template=%#v err=%v", persisted, err)
			}
		})
	}
}
