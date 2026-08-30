package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/labstack/echo/v4"
)

// registryWith builds a registry holding the named providers. The values are
// nil on purpose: authLoginHandler only ever asks whether a key is present,
// which is the same question /api/auth/providers answers.
func registryWith(names ...string) authapp.OAuthProviderRegistry {
	reg := authapp.OAuthProviderRegistry{}
	for _, name := range names {
		reg[name] = nil
	}
	return reg
}

// beganSignIn stands in for pericarp's Login: it records that the wrapper
// delegated, and redirects the way a real initiated flow does.
func beganSignIn(began *bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*began = true
		http.Redirect(w, r, "https://accounts.example/authorize", http.StatusFound)
	}
}

// runLogin drives authLoginHandler against the given address and returns the
// recorded response plus whether the inner handler ran.
func runLogin(t *testing.T, target string, cfg authLoginConfig) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	began := false
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, target, http.NoBody), rec)
	if err := authLoginHandler(beganSignIn(&began), cfg)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return rec, began
}

// refusedTo reads the refusal's destination, failing the test when the
// response was not a refusal at all.
func refusedTo(t *testing.T, rec *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body = %q", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("bad Location %q: %v", loc, err)
	}
	return parsed
}

func TestAuthLoginHandler_RefusesAProviderTheInstanceDoesNotHave(t *testing.T) {
	rec, began := runLogin(t, "/api/auth/login?provider=apple",
		authLoginConfig{Registry: registryWith("google")})

	if began {
		t.Fatal("the sign-in was begun for a provider the registry does not hold")
	}
	to := refusedTo(t, rec)
	if to.Path != "/" {
		t.Errorf("refused to %q, want the sign-in screen at /", to)
	}
	if got := to.Query().Get("auth_error"); got != AuthLoginRefusalCode {
		t.Errorf("auth_error = %q, want %q", got, AuthLoginRefusalCode)
	}
}

// The whole reason this wrapper exists: what the browser is left looking at.
func TestAuthLoginHandler_RefusalIsARedirectAndNotABody(t *testing.T) {
	rec, _ := runLogin(t, "/api/auth/login?provider=apple",
		authLoginConfig{Registry: registryWith("google")})

	refusedTo(t, rec)
	if body := strings.TrimSpace(rec.Body.String()); strings.Contains(body, "{") {
		t.Errorf("refusal carried a body a browser would render: %q", body)
	}
}

func TestAuthLoginHandler_BeginsASignInTheInstanceCanComplete(t *testing.T) {
	rec, began := runLogin(t, "/api/auth/login?provider=google",
		authLoginConfig{Registry: registryWith("google")})

	if !began {
		t.Fatal("a configured provider was refused")
	}
	if loc := rec.Header().Get("Location"); loc != "https://accounts.example/authorize" {
		t.Errorf("Location = %q, want the provider's authorize URL", loc)
	}
}

func TestAuthLoginHandler_RefusesEveryProviderWhenNoneIsConfigured(t *testing.T) {
	for _, provider := range []string{"google", "apple", "netsuite"} {
		rec, began := runLogin(t, "/api/auth/login?provider="+provider,
			authLoginConfig{Registry: registryWith()})
		if began {
			t.Errorf("%s: the sign-in was begun on an instance with no providers", provider)
		}
		if got := refusedTo(t, rec).Query().Get("auth_error"); got != AuthLoginRefusalCode {
			t.Errorf("%s: auth_error = %q, want %q", provider, got, AuthLoginRefusalCode)
		}
	}
}

// A link that names no provider keeps working wherever it works today. This
// is a shipped calling convention — core's own admin sign-in and invite pages
// use it, as do weos-finance's admin and crossdeck-crm — so serve.go hands the
// wrapper config.DefaultOAuthProvider(), which resolves it exactly as it
// always has. Refusing it outright would have broken all of them on any
// deployment that never needed to set OAUTH_DEFAULT_PROVIDER.
func TestAuthLoginHandler_LetsAResolvedDefaultThrough(t *testing.T) {
	for _, target := range []string{"/api/auth/login", "/api/auth/login?provider=", "/api/auth/login?provider=%20"} {
		_, began := runLogin(t, target,
			authLoginConfig{Registry: registryWith("google"), DefaultProvider: "google"})
		if !began {
			t.Errorf("%s: a caller naming no provider was refused", target)
		}
	}
}

// The case that produced the raw JSON page. DefaultOAuthProvider() ends in an
// unconditional "google", so an instance with NOTHING configured resolves an
// absent provider to a provider it does not have. The registry check is what
// catches it: the resolution is trusted to name something, never to name
// something this instance can actually begin.
func TestAuthLoginHandler_RefusesADefaultTheInstanceCannotBegin(t *testing.T) {
	rec, began := runLogin(t, "/api/auth/login",
		authLoginConfig{Registry: authapp.OAuthProviderRegistry{}, DefaultProvider: "google"})

	if began {
		t.Fatal("an instance with no providers began a sign-in for the fallback guess")
	}
	if got := refusedTo(t, rec).Query().Get("auth_error"); got != AuthLoginRefusalCode {
		t.Errorf("auth_error = %q, want %q", got, AuthLoginRefusalCode)
	}
}

// An operator naming the default explicitly is honored in full.
func TestAuthLoginHandler_HonoursAnExplicitlyConfiguredDefault(t *testing.T) {
	_, began := runLogin(t, "/api/auth/login",
		authLoginConfig{Registry: registryWith("netsuite"), DefaultProvider: "netsuite"})

	if !began {
		t.Fatal("the configured default provider was refused")
	}
}

// A default naming a provider the registry does not hold is a
// misconfiguration; falling back past it would hide it for ever.
func TestAuthLoginHandler_RefusesADefaultThatIsNotConfigured(t *testing.T) {
	_, began := runLogin(t, "/api/auth/login",
		authLoginConfig{Registry: registryWith("google"), DefaultProvider: "apple"})

	if began {
		t.Fatal("a default naming an unregistered provider began a sign-in")
	}
}

// The native round trip: the refusal goes back to the callback address the
// caller handed in, so the app is where the reason lands.
func TestAuthLoginHandler_RefusalHonoursTheCallbackItWasHanded(t *testing.T) {
	rec, _ := runLogin(t, "/api/auth/login?provider=apple&redirect=%2Fauth%2Fcallback",
		authLoginConfig{Registry: registryWith("google")})

	to := refusedTo(t, rec)
	if to.Path != "/auth/callback" {
		t.Errorf("refused to %q, want the callback address it was handed", to)
	}
	if got := to.Query().Get("auth_error"); got != AuthLoginRefusalCode {
		t.Errorf("auth_error = %q, want %q", got, AuthLoginRefusalCode)
	}
}

// This address is the one a hostile link points at, so the refusal must never
// reach an address a completed sign-in could not.
func TestAuthLoginHandler_RefusalNeverLeavesTheInstance(t *testing.T) {
	hostile := []string{
		"https://evil.example/steal",
		"//evil.example/steal",
		`/\evil.example/steal`,
		"javascript://evil.example",
	}
	for _, redirect := range hostile {
		rec, _ := runLogin(t,
			"/api/auth/login?provider=apple&redirect="+url.QueryEscape(redirect),
			authLoginConfig{Registry: registryWith("google")})

		to := refusedTo(t, rec)
		if to.Host != "" || to.Scheme != "" || to.Path != "/" {
			t.Errorf("redirect=%q refused to %q, want the sign-in screen at /", redirect, to)
		}
	}
}

// Nothing the caller asked for is echoed back onto a page this instance
// serves — the refusal says only that something was unavailable.
func TestAuthLoginHandler_RefusalRepeatsNothingBackToTheBrowser(t *testing.T) {
	asked := `<img src=x onerror=alert(1)>`
	rec, _ := runLogin(t, "/api/auth/login?provider="+url.QueryEscape(asked),
		authLoginConfig{Registry: registryWith("google")})

	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "img") || strings.Contains(loc, "onerror") {
		t.Errorf("the refusal echoed what was asked for: %q", loc)
	}
	if strings.Contains(loc, "google") {
		t.Errorf("the refusal named a provider this instance has: %q", loc)
	}
}

// A cross-origin SPA gets the refusal at its own address, the same way
// pericarp's success path uses FrontendURL.
func TestAuthLoginHandler_RefusalUsesTheConfiguredFrontendOrigin(t *testing.T) {
	rec, _ := runLogin(t, "/api/auth/login?provider=apple",
		authLoginConfig{Registry: registryWith("google"), FrontendURL: "https://app.example/"})

	to := refusedTo(t, rec)
	if to.Scheme != "https" || to.Host != "app.example" || to.Path != "/" {
		t.Errorf("refused to %q, want the configured frontend's sign-in screen", to)
	}
	if got := to.Query().Get("auth_error"); got != AuthLoginRefusalCode {
		t.Errorf("auth_error = %q, want %q", got, AuthLoginRefusalCode)
	}
}

// The inner handler must act on the provider this wrapper checked, not on the
// one that arrived. Pericarp's Login reads the raw query value without
// trimming and falls back to its own configured default when it is empty, so
// a request delegated untouched can resolve to something the registry check
// never saw — and produce the 500 JSON page this wrapper exists to prevent.
func TestAuthLoginHandler_HandsOnTheProviderItChecked(t *testing.T) {
	for _, target := range []string{
		"/api/auth/login",
		"/api/auth/login?provider=",
		"/api/auth/login?provider=%20",
		"/api/auth/login?provider=%20google%20",
	} {
		var seen string
		inner := func(_ http.ResponseWriter, r *http.Request) {
			seen = r.URL.Query().Get("provider")
		}
		e := echo.New()
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodGet, target, http.NoBody), rec)
		if err := authLoginHandler(inner, authLoginConfig{
			Registry:        registryWith("google"),
			DefaultProvider: "google",
		})(c); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if seen != "google" {
			t.Errorf("%s: inner handler saw provider %q, want %q", target, seen, "google")
		}
	}
}

// A request that already names a usable provider is delegated untouched.
func TestAuthLoginHandler_LeavesAUsableRequestAlone(t *testing.T) {
	var seenQuery string
	inner := func(_ http.ResponseWriter, r *http.Request) { seenQuery = r.URL.RawQuery }
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(
		httptest.NewRequest(http.MethodGet, "/api/auth/login?provider=google&redirect=%2Fhome", http.NoBody),
		rec,
	)
	if err := authLoginHandler(inner, authLoginConfig{
		Registry:        registryWith("google"),
		DefaultProvider: "google",
	})(c); err != nil {
		t.Fatal(err)
	}
	if seenQuery != "provider=google&redirect=%2Fhome" {
		t.Errorf("query was rewritten unnecessarily: %q", seenQuery)
	}
}
