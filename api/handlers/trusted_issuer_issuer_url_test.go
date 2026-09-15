package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	"github.com/labstack/echo/v4"
)

func refusingHandler() *handlers.TrustedIssuerHandler {
	h, _ := newTrustedIssuerHandler(
		&fakeAssertionVerifier{err: &trustedissuer.Refusal{Reason: trustedissuer.ReasonSignature}},
		&assertAuthService{}, &assertionLogCapture{})
	return h
}

// TRUSTED_ISSUER is published as the base of the door's sign-in address, so an
// issuer that address cannot be built from — not https, no host, a query, a
// fragment, user information — mounts nothing, offers no door, and says why.
func TestMountTrustedIssuerAssertionRefusesAnIssuerTheSignInAddressCannotBeBuiltFrom(t *testing.T) {
	cases := map[string]struct {
		issuer   string
		loginURL string // "" when nothing is mounted
	}{
		"https":                     {"https://money.weos.cloud", "https://money.weos.cloud/door/start"},
		"https with a path":         {"https://door.example/fleet", "https://door.example/fleet/door/start"},
		"a trailing slash":          {"https://money.weos.cloud/", "https://money.weos.cloud/door/start"},
		"loopback over plain http":  {"http://127.0.0.1:9443", "http://127.0.0.1:9443/door/start"},
		"localhost over plain http": {"http://localhost:9443/", "http://localhost:9443/door/start"},
		"plain http":                {"http://money.weos.cloud", ""},
		"no scheme":                 {"money.weos.cloud", ""},
		"no host":                   {"https://", ""},
		"a query":                   {"https://door.example?tenant=x", ""},
		"an empty query":            {"https://door.example?", ""},
		"a fragment":                {"https://door.example#start", ""},
		"user information":          {"https://ops:hunter2@door.example", ""},
		"another scheme":            {"ftp://door.example", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// LinkPasswordOwners keeps the owner-proof warning, which
			// TestMountTrustedIssuerAssertion covers, out of the lines counted here.
			cfg := config.Config{SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{
				Issuer: c.issuer, JWKSURL: doorKeyList, Audience: doorAudience, LinkPasswordOwners: true,
			}}
			logs := &assertionLogCapture{}
			mounted := handlers.MountTrustedIssuerAssertion(context.Background(), echo.New().Group("/api"), cfg, logs, refusingHandler)
			if want := c.loginURL != ""; mounted != want {
				t.Fatalf("mounted = %v, want %v:\n%s", mounted, want, logs.text())
			}

			entry := issuerEntry(t, decodeProviderEntries(t, getProviders(newIssuerBootServer(&cfg)).Body.Bytes()))
			if (entry != nil) != mounted {
				t.Fatalf("issuer provider offered = %v, route mounted = %v", entry != nil, mounted)
			}
			if mounted {
				if got := fieldText(t, entry, "login_url"); got != c.loginURL {
					t.Fatalf("login_url = %q, want %q", got, c.loginURL)
				}
				if n := len(logs.atLevel("warn")) + len(logs.atLevel("error")); n != 0 {
					t.Fatalf("a mounted route logged %d warnings or errors:\n%s", n, logs.text())
				}
				return
			}
			warns := logs.atLevel("warn")
			if len(warns) != 1 || !strings.Contains(warns[0].msg, "TRUSTED_ISSUER") {
				t.Fatalf("expected one boot warning naming TRUSTED_ISSUER, got:\n%s", logs.text())
			}
			if strings.Contains(logs.text(), "hunter2") {
				t.Fatalf("the boot log carries the issuer's user information:\n%s", logs.text())
			}
			if len(logs.atLevel("info")) != 0 {
				t.Fatalf("an unmounted route logged an info line:\n%s", logs.text())
			}
		})
	}
}

// Every boot line about a trusted issuer — mounted, partly configured, an
// unusable address, the public session secret — says plainly that the API is
// locked, so an operator who adds the settings before the door serves the
// instance knows why every page answers 401.
func TestEveryTrustedIssuerBootLineSaysTheAPIIsLocked(t *testing.T) {
	all3 := config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience}
	cases := map[string]config.Config{
		// The opt-in leaves the owner-proof warning out: that line is about who
		// may sign in, not about the lock, and TestMountTrustedIssuerAssertion covers it.
		"mounted":              {SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience, LinkPasswordOwners: true}},
		"partly configured":    {SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{Issuer: doorIssuer}},
		"an unusable issuer":   {SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{Issuer: "http://door.example", JWKSURL: doorKeyList, Audience: doorAudience}},
		"an unusable key list": {SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: "http://door.example/jwks.json", Audience: doorAudience}},
		"the public secret":    {SessionSecret: config.DefaultSessionSecret, TrustedIssuer: all3},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			logs := &assertionLogCapture{}
			handlers.MountTrustedIssuerAssertion(context.Background(), echo.New().Group("/api"), cfg, logs, refusingHandler)
			lines := append(append(logs.atLevel("info"), logs.atLevel("warn")...), logs.atLevel("error")...)
			if len(lines) != 1 {
				t.Fatalf("expected exactly one boot line, got:\n%s", logs.text())
			}
			consequence, _ := field(lines[0], "consequence")
			text, _ := consequence.(string)
			if !strings.HasPrefix(text, "the API is locked: every API route requires a sign-in") {
				t.Fatalf("boot line %q says %q, want it to say the API is locked", lines[0].msg, text)
			}
			if name == "mounted" && !strings.Contains(text, "until an assertion") {
				t.Fatalf("the mounted line %q does not say an assertion unlocks the API", text)
			}
		})
	}

	logs := &assertionLogCapture{}
	handlers.MountTrustedIssuerAssertion(context.Background(), echo.New().Group("/api"), config.Config{}, logs, refusingHandler)
	if logged := logs.text(); logged != "" {
		t.Fatalf("an instance with no trusted-issuer setting logged:\n%s", logged)
	}
}
