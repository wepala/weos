package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// buildProviderRegistry builds the registry exactly the way the serve boot
// does — through application.ProvideOAuthProviderRegistry — so these tests
// hold the endpoint to what /api/auth/login will actually accept, including
// the all-four-fields Apple gate, rather than to a hand-rolled fixture.
// The pointer parameter is not a micro-optimisation: config.Config is 768
// bytes, and the analyser blocks a by-value pass of anything over 80.
func buildProviderRegistry(cfg *config.Config) authapp.OAuthProviderRegistry {
	return application.ProvideOAuthProviderRegistry(struct {
		fx.In
		Config config.Config
	}{Config: *cfg})
}

// newAuthBootServer mirrors the serve.go topology this endpoint lives in: an
// /api group whose protected subgroup challenges everything it matches, the
// way RequireAuth does on an OAuth boot. The discovery route is mounted (via
// the same MountAuthProviders serve.go calls) on the anonymous group, so
// every request these tests make doubles as the anonymous-reachability check.
// Pass mount=false to get the "old instance" shape: no discovery route, only
// the auth challenge.
func newAuthBootServer(reg authapp.OAuthProviderRegistry, mount bool) *echo.Echo {
	e := echo.New()
	api := e.Group("/api")
	protected := api.Group("")
	protected.Use(func(_ echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Pericarp's RequireAuth answers a session-less caller with a
			// 401 JSON body (and no WWW-Authenticate header) — mirror that.
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		}
	})
	if mount {
		handlers.MountAuthProviders(api, handlers.NewAuthProvidersHandler(reg))
	}
	return e
}

// getProviders performs an anonymous GET /api/auth/providers — no session
// cookie, no bearer token — and returns the recorded response.
func getProviders(e *echo.Echo) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/providers", http.NoBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// decodeProviderNames unwraps the response envelope and returns the provider
// names in response order.
func decodeProviderNames(t *testing.T, body []byte) []string {
	t.Helper()
	var envelope struct {
		Data struct {
			Providers []struct {
				Name string `json:"name"`
			} `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v (body %s)", err, body)
	}
	names := make([]string, 0, len(envelope.Data.Providers))
	for _, p := range envelope.Data.Providers {
		names = append(names, p.Name)
	}
	return names
}

func assertProviderNames(t *testing.T, rec *httptest.ResponseRecorder, want []string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	got := decodeProviderNames(t, rec.Body.Bytes())
	if len(got) != len(want) {
		t.Fatalf("providers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("providers = %v, want %v", got, want)
		}
	}
}

func TestAuthProviders_NoneConfigured(t *testing.T) {
	t.Parallel()
	rec := getProviders(newAuthBootServer(buildProviderRegistry(&config.Config{}), true))

	assertProviderNames(t, rec, []string{})
	// The empty list must serialize as [], not null — a client telling "no
	// providers" apart from "field missing" (an old instance) needs the array
	// to actually be there.
	if !strings.Contains(rec.Body.String(), `"providers":[]`) {
		t.Fatalf("body = %s, want a literal empty providers array", rec.Body.String())
	}
}

func TestAuthProviders_GoogleOnly(t *testing.T) {
	t.Parallel()
	cfg := config.Config{OAuth: config.OAuthConfig{
		GoogleClientID:     "google-client-id",
		GoogleClientSecret: "google-client-secret",
	}}
	rec := getProviders(newAuthBootServer(buildProviderRegistry(&cfg), true))

	assertProviderNames(t, rec, []string{"google"})
}

func TestAuthProviders_GoogleAndApple(t *testing.T) {
	t.Parallel()
	cfg := config.Config{OAuth: config.OAuthConfig{
		GoogleClientID:     "google-client-id",
		GoogleClientSecret: "google-client-secret",
		AppleClientID:      "app.example.web",
		AppleTeamID:        "TEAM123456",
		AppleKeyID:         "KEY1234567",
		ApplePrivateKey:    "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----",
	}}
	rec := getProviders(newAuthBootServer(buildProviderRegistry(&cfg), true))

	assertProviderNames(t, rec, []string{"apple", "google"})
}

// Apple requires all four credentials to sign its client-secret JWT, so a
// three-of-four config must not be offered as a sign-in option: the button
// would render and the login would then fail.
func TestAuthProviders_PartialAppleOmitted(t *testing.T) {
	t.Parallel()
	cfg := config.Config{OAuth: config.OAuthConfig{
		GoogleClientID:     "google-client-id",
		GoogleClientSecret: "google-client-secret",
		AppleClientID:      "app.example.web",
		AppleTeamID:        "TEAM123456",
		AppleKeyID:         "KEY1234567",
		// ApplePrivateKey deliberately missing.
	}}
	rec := getProviders(newAuthBootServer(buildProviderRegistry(&cfg), true))

	assertProviderNames(t, rec, []string{"google"})
}

// The endpoint names providers and nothing else. Build it from a fully
// populated config whose every credential carries a recognizable marker and
// prove none of those values appears anywhere in the serialized body.
func TestAuthProviders_ResponseNeverCarriesCredentials(t *testing.T) {
	t.Parallel()
	secrets := map[string]string{
		"GoogleClientID":       "LEAK-google-client-id",
		"GoogleClientSecret":   "LEAK-google-client-secret",
		"NetSuiteClientID":     "LEAK-netsuite-client-id",
		"NetSuiteClientSecret": "LEAK-netsuite-client-secret",
		"NetSuiteAccountID":    "LEAK-1234567",
		"AppleClientID":        "LEAK-app.example.web",
		"AppleTeamID":          "LEAK-TEAM123456",
		"AppleKeyID":           "LEAK-KEY1234567",
		"ApplePrivateKey":      "LEAK-apple-private-key-pem",
	}
	cfg := config.Config{OAuth: config.OAuthConfig{
		GoogleClientID:       secrets["GoogleClientID"],
		GoogleClientSecret:   secrets["GoogleClientSecret"],
		NetSuiteClientID:     secrets["NetSuiteClientID"],
		NetSuiteClientSecret: secrets["NetSuiteClientSecret"],
		NetSuiteAccountID:    secrets["NetSuiteAccountID"],
		NetSuiteScopes:       []string{"rest_webservices", "openid"},
		AppleClientID:        secrets["AppleClientID"],
		AppleTeamID:          secrets["AppleTeamID"],
		AppleKeyID:           secrets["AppleKeyID"],
		ApplePrivateKey:      secrets["ApplePrivateKey"],
	}}
	rec := getProviders(newAuthBootServer(buildProviderRegistry(&cfg), true))

	assertProviderNames(t, rec, []string{"apple", "google", "netsuite"})
	body := rec.Body.String()
	for field, value := range secrets {
		if strings.Contains(body, value) {
			t.Errorf("response leaks OAuth.%s: body = %s", field, body)
		}
	}
}

// The sign-in screen asks before anyone has a session, so the route must
// answer an anonymous caller even when the instance boots with RequireAuth
// on the protected group — and an instance WITHOUT the route (an older
// build) keeps answering 401 there, which is exactly how a client tells
// "too old" (401) from "no providers" (200 + empty list).
func TestAuthProviders_AnonymousOnRequireAuthBoot(t *testing.T) {
	t.Parallel()
	cfg := config.Config{OAuth: config.OAuthConfig{
		GoogleClientID:     "google-client-id",
		GoogleClientSecret: "google-client-secret",
	}}
	reg := buildProviderRegistry(&cfg)

	rec := getProviders(newAuthBootServer(reg, true))
	assertProviderNames(t, rec, []string{"google"})

	old := getProviders(newAuthBootServer(reg, false))
	if old.Code != http.StatusUnauthorized {
		t.Fatalf("old-instance code = %d, want 401", old.Code)
	}
}
