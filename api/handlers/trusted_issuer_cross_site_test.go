package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/labstack/echo/v4"
)

const (
	doorOrigin     = "https://money.weos.cloud"
	instanceOrigin = "https://dana.money.weos.cloud"
)

// postAssertionWithHeaders posts an assertion body carrying the given headers.
// Each header name may hold several values.
func postAssertionWithHeaders(t *testing.T, h *handlers.TrustedIssuerHandler, body string, headers map[string][]string) *httptest.ResponseRecorder {
	t.Helper()
	req := newJSONRequest(http.MethodPost, "/api/auth/assert", body)
	for name, values := range headers {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	rec := httptest.NewRecorder()
	if err := h.Assert(echo.New().NewContext(req, rec)); err != nil {
		t.Fatalf("Assert returned an error instead of answering: %v", err)
	}
	return rec
}

// newOriginCheckedHandler builds the handler for an instance served both on
// the door's origin and on its own, over a verifier that accepts the
// assertion.
func newOriginCheckedHandler(t *testing.T) (*handlers.TrustedIssuerHandler, *fakeAssertionVerifier, *fakeSessionManager, *assertionLogCapture) {
	t.Helper()
	logs := &assertionLogCapture{}
	auth := signedInAuthService(t)
	verifier := &fakeAssertionVerifier{identity: acceptedIdentity()}
	sm := &fakeSessionManager{}
	h := handlers.NewTrustedIssuerHandler(handlers.TrustedIssuerHandlerConfig{
		Verifier: verifier,
		SignIn:   auth,
		Sessions: handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
			AuthService: auth, SessionManager: sm, Logger: logs,
		}),
		Logger:         logs,
		AllowedOrigins: []string{doorOrigin + "/", instanceOrigin},
	})
	return h, verifier, sm, logs
}

// A page on another site that holds a valid assertion for this audience must
// not be able to post it from a victim's browser and sign that browser in as
// someone else. The request is refused 403 before its body is read, so the
// verifier never sees the assertion and its jti is not spent.
func TestAssertRefusesARequestFromAnotherSiteBeforeReadingIt(t *testing.T) {
	cases := map[string]map[string][]string{
		"fetch metadata cross-site":             {"Sec-Fetch-Site": {"cross-site"}},
		"fetch metadata same-site":              {"Sec-Fetch-Site": {"same-site"}},
		"fetch metadata none":                   {"Sec-Fetch-Site": {"none"}},
		"fetch metadata of an unknown value":    {"Sec-Fetch-Site": {"elsewhere"}},
		"a foreign origin":                      {"Origin": {"https://attacker.example"}},
		"a foreign origin marked same-origin":   {"Origin": {"https://attacker.example"}, "Sec-Fetch-Site": {"same-origin"}},
		"the null origin of a sandboxed page":   {"Origin": {"null"}},
		"the door's host over plain http":       {"Origin": {"http://money.weos.cloud"}},
		"the door's host on another port":       {"Origin": {"https://money.weos.cloud:8443"}},
		"a host that only starts like the door": {"Origin": {"https://money.weos.cloud.attacker.example"}},
		"a parent of the instance's host":       {"Origin": {"https://weos.cloud"}},
		"two origins":                           {"Origin": {doorOrigin, "https://attacker.example"}},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			h, verifier, sm, logs := newOriginCheckedHandler(t)

			rec := postAssertionWithHeaders(t, h, assertionBody(presentedAssertion), headers)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body.String())
			}
			if handlers.CodeCrossSite != "cross-site" {
				t.Fatalf("CodeCrossSite = %q, want cross-site", handlers.CodeCrossSite)
			}
			if code := readAssertAnswer(t, rec).Code; code != handlers.CodeCrossSite {
				t.Fatalf("answer code = %q, want %q", code, handlers.CodeCrossSite)
			}
			if len(verifier.got) != 0 {
				t.Fatalf("a cross-site request reached the verifier")
			}
			if sm.createCalls != 0 || len(rec.Header().Values("Set-Cookie")) != 0 {
				t.Fatalf("a cross-site request was given a session")
			}
			warns := logs.atLevel("warn")
			if len(warns) != 1 {
				t.Fatalf("expected one warning for the refusal, got:\n%s", logs.text())
			}
			if reason, _ := field(warns[0], "reason"); reason != handlers.CodeCrossSite {
				t.Fatalf("the warning's reason = %v, want %q", reason, handlers.CodeCrossSite)
			}
			if strings.Contains(logs.text(), "elsewhere") {
				t.Fatalf("an unknown header value reached the log:\n%s", logs.text())
			}
			requireNoAssertionLogged(t, logs)
		})
	}
}

// The door's page, the instance's own page, and a client that is not a browser
// all reach the verifier.
func TestAssertAcceptsASameOriginRequestAndOneWithNoBrowserHeaders(t *testing.T) {
	cases := map[string]map[string][]string{
		"no browser headers":                       nil,
		"fetch metadata same-origin alone":         {"Sec-Fetch-Site": {"same-origin"}},
		"the door's origin":                        {"Origin": {doorOrigin}, "Sec-Fetch-Site": {"same-origin"}},
		"the instance's own origin":                {"Origin": {instanceOrigin}, "Sec-Fetch-Site": {"same-origin"}},
		"the door's origin without fetch metadata": {"Origin": {doorOrigin}},
		"the door's origin in capitals, port 443":  {"Origin": {"HTTPS://Money.WEOS.cloud:443"}},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			h, verifier, sm, _ := newOriginCheckedHandler(t)

			rec := postAssertionWithHeaders(t, h, assertionBody(presentedAssertion), headers)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
			}
			if len(verifier.got) != 1 || sm.createCalls != 1 {
				t.Fatalf("verifier saw %d assertions and %d sessions were made, want 1 and 1", len(verifier.got), sm.createCalls)
			}
		})
	}
}

// The constructor serve.go builds the route with allows exactly the trusted
// issuer's origin and the instance's public origin. A bad assertion from an
// allowed origin is refused by the verifier (401); from any other, it never
// gets that far (403).
func TestNewTrustedIssuerAssertionHandlerAllowsTheIssuersAndTheInstancesOrigins(t *testing.T) {
	settings := config.TrustedIssuerConfig{
		Issuer: "https://door.example/fleet/", JWKSURL: "https://door.example/fleet/jwks.json", Audience: "a1b2c3d4",
	}
	build := func(publicBaseURL string) *handlers.TrustedIssuerHandler {
		auth := signedInAuthService(t)
		logs := &assertionLogCapture{}
		return handlers.NewTrustedIssuerAssertionHandler(settings, nil, handlers.TrustedIssuerAssertionDeps{
			SignIn: auth,
			Sessions: handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
				AuthService: auth, SessionManager: &fakeSessionManager{}, Logger: logs,
			}),
			Logger:        logs,
			PublicBaseURL: publicBaseURL,
		})
	}
	cases := map[string]struct {
		publicBaseURL, origin string
		want                  int
	}{
		"the issuer's origin":                        {"https://dana.money.weos.cloud/", "https://door.example", http.StatusUnauthorized},
		"the instance's origin":                      {"https://dana.money.weos.cloud/", "https://dana.money.weos.cloud", http.StatusUnauthorized},
		"a derived local address":                    {"http://localhost:8080", "http://localhost:8080", http.StatusUnauthorized},
		"another origin":                             {"https://dana.money.weos.cloud/", "https://attacker.example", http.StatusForbidden},
		"the instance's origin with no base address": {"", "https://dana.money.weos.cloud", http.StatusForbidden},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postAssertionWithHeaders(t, build(c.publicBaseURL), assertionBody("not-an-assertion"),
				map[string][]string{"Origin": {c.origin}})
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}
