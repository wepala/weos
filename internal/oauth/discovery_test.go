package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/labstack/echo/v4"
)

const testBaseURL = "https://example.com"

// runHandler invokes handler and routes any returned error through Echo's
// default HTTPErrorHandler so rec.Code reflects the wire-level status.
func runHandler(t *testing.T, handler echo.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}
	return rec
}

func TestProtectedResourceMetadata_HappyPaths(t *testing.T) {
	t.Parallel()
	known := map[string]bool{"/api/mcp": true, "/api/agents": true}
	handler := ProtectedResourceMetadata(testBaseURL, "/api/mcp", known)

	cases := []struct {
		name         string
		path         string
		wantResource string
	}{
		{"bare returns default", WellKnownProtectedResourcePrefix, testBaseURL + "/api/mcp"},
		{"trailing slash treated as bare", WellKnownProtectedResourcePrefix + "/", testBaseURL + "/api/mcp"},
		{"first known suffix", WellKnownProtectedResourcePrefix + "/api/mcp", testBaseURL + "/api/mcp"},
		{"second known suffix", WellKnownProtectedResourcePrefix + "/api/agents", testBaseURL + "/api/agents"},
		{"query string ignored in match", WellKnownProtectedResourcePrefix + "/api/mcp?x=1", testBaseURL + "/api/mcp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := runHandler(t, handler, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status: got %d want 200", rec.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got := body["resource"]; got != tc.wantResource {
				t.Errorf("resource: got %v want %s", got, tc.wantResource)
			}
			wantAuthServers := []any{testBaseURL}
			if got := body["authorization_servers"]; !reflect.DeepEqual(got, wantAuthServers) {
				t.Errorf("authorization_servers: got %v want %v", got, wantAuthServers)
			}
			wantBearer := []any{"header"}
			if got := body["bearer_methods_supported"]; !reflect.DeepEqual(got, wantBearer) {
				t.Errorf("bearer_methods_supported: got %v want %v", got, wantBearer)
			}
			scopes, ok := body["scopes_supported"].([]any)
			if !ok || len(scopes) != len(SupportedScopesList) {
				t.Errorf("scopes_supported: got %v want %d entries", body["scopes_supported"], len(SupportedScopesList))
			}
		})
	}
}

func TestProtectedResourceMetadata_NotFound(t *testing.T) {
	t.Parallel()
	known := map[string]bool{"/api/mcp": true}
	handler := ProtectedResourceMetadata(testBaseURL, "/api/mcp", known)

	cases := []struct {
		name string
		path string
	}{
		{"unknown suffix", WellKnownProtectedResourcePrefix + "/bogus"},
		{"trailing slash on known suffix", WellKnownProtectedResourcePrefix + "/api/mcp/"},
		{"prefix collision without slash boundary", WellKnownProtectedResourcePrefix + "X"},
		{"foreign path entirely", "/some/other/path"},
		{"case-sensitive miss", WellKnownProtectedResourcePrefix + "/API/MCP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := runHandler(t, handler, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status: got %d want 404 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestProtectedResourceMetadata_BarePath404WhenNoDefault(t *testing.T) {
	t.Parallel()
	// When no default resource is configured (e.g. MCP disabled), the bare
	// path must 404 instead of advertising a path the server doesn't expose.
	handler := ProtectedResourceMetadata(testBaseURL, "", map[string]bool{})
	for _, path := range []string{
		WellKnownProtectedResourcePrefix,
		WellKnownProtectedResourcePrefix + "/",
		WellKnownProtectedResourcePrefix + "/api/mcp",
	} {
		rec := runHandler(t, handler, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("path %q: got %d want 404", path, rec.Code)
		}
	}
}

func TestIsProtectedResourceMetadataRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"GET bare", http.MethodGet, WellKnownProtectedResourcePrefix, true},
		{"GET suffixed", http.MethodGet, WellKnownProtectedResourcePrefix + "/api/mcp", true},
		{"GET trailing slash", http.MethodGet, WellKnownProtectedResourcePrefix + "/", true},
		{"GET prefix collision", http.MethodGet, WellKnownProtectedResourcePrefix + "X", false},
		{"GET foreign path", http.MethodGet, "/.well-known/oauth-authorization-server", false},
		{"POST bare", http.MethodPost, WellKnownProtectedResourcePrefix, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsProtectedResourceMetadataRequest(tc.method, tc.path); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}
