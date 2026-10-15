package unit_test

import (
	"io"
	"net/http/httptest"
	"questionnaire/internal/web"
	"strings"
	"testing"
)

func TestHTTPGuardAndJSON(t *testing.T) {
	handler := web.New(nil, false)
	for _, tc := range []struct {
		name, body, contentType, origin, fetch string
		want                                   int
	}{
		{"malformed", "{", "application/json", "", "", 400},
		{"unknown field", `{"username":"alice","password":"password123","extra":true}`, "application/json", "", "", 400},
		{"trailing JSON", `{} {}`, "application/json", "", "", 400},
		{"wrong media type", `{}`, "text/plain", "", "", 400},
		{"cross origin", `{}`, "application/json", "https://attacker.invalid", "", 403},
		{"cross site", `{}`, "application/json", "", "cross-site", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://example.test/api/register", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.fetch)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			response := recorder.Result()
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tc.want {
				t.Fatalf("status %d, want %d: %s", response.StatusCode, tc.want, body)
			}
			if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("security/cache headers missing")
			}
		})
	}
}
