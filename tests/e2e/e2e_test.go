package e2e_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"questionnaire/internal/web"
	"questionnaire/tests/support"
)

// The opt-in target runs the embedded application and a real Chromium browser
// against a freshly allocated database. No existing project data is changed.
func TestBrowserAcceptance(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("run make e2e to enable real-browser acceptance")
	}
	if os.Getenv("INTEGRATION_MONGO_URI") == "" {
		t.Fatal("INTEGRATION_MONGO_URI is required for browser acceptance")
	}
	fixture := support.NewMongo(t)
	server := httptest.NewServer(web.New(fixture.Store, false))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "browser.test.cjs")
	cmd.Env = append(os.Environ(), "E2E_BASE_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("browser acceptance failed: %v", err)
	}
}
