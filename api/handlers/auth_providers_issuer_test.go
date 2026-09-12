package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	"github.com/labstack/echo/v4"
)

const (
	doorIssuer   = "https://money.weos.cloud"
	doorKeyList  = "https://money.weos.cloud/door/jwks.json"
	doorAudience = "a1b2c3d4"
)

// newIssuerBootServer mounts the discovery route the way serve.go does: over
// the configured registry, with the trusted-issuer settings.
func newIssuerBootServer(cfg *config.Config) *echo.Echo {
	e := echo.New()
	api := e.Group("/api")
	handlers.MountAuthProviders(api, handlers.NewAuthProvidersHandler(
		buildProviderRegistry(cfg), handlers.WithTrustedIssuer(cfg.TrustedIssuer)))
	return e
}

// decodeProviderEntries returns every provider entry with its raw fields.
func decodeProviderEntries(t *testing.T, body []byte) []map[string]json.RawMessage {
	t.Helper()
	var envelope struct {
		Data struct {
			Providers []map[string]json.RawMessage `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v (body %s)", err, body)
	}
	return envelope.Data.Providers
}

func fieldText(t *testing.T, entry map[string]json.RawMessage, key string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(entry[key], &s); err != nil {
		t.Fatalf("field %q = %s, want a string", key, entry[key])
	}
	return s
}

func entryKeys(entry map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(entry))
	for k := range entry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func issuerEntry(t *testing.T, entries []map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	for _, entry := range entries {
		if fieldText(t, entry, "name") == handlers.TrustedIssuerProviderName {
			return entry
		}
	}
	return nil
}

func TestAuthProviders_TrustedIssuerOffersTheDoor(t *testing.T) {
	t.Parallel()
	if handlers.TrustedIssuerProviderName != "issuer" {
		t.Fatalf("TrustedIssuerProviderName = %q, want issuer", handlers.TrustedIssuerProviderName)
	}
	cfg := config.Config{TrustedIssuer: config.TrustedIssuerConfig{
		Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience,
	}}
	rec := getProviders(newIssuerBootServer(&cfg))
	assertProviderNames(t, rec, []string{"issuer"})

	entry := decodeProviderEntries(t, rec.Body.Bytes())[0]
	if got := entryKeys(entry); len(got) != 2 || got[0] != "login_url" || got[1] != "name" {
		t.Fatalf("issuer entry carries %v, want exactly login_url and name", got)
	}
	if got := fieldText(t, entry, "login_url"); got != "https://money.weos.cloud/door/start" {
		t.Fatalf("login_url = %q, want https://money.weos.cloud/door/start", got)
	}
}

func TestAuthProviders_TrustedIssuerTrailingSlashIsTrimmed(t *testing.T) {
	t.Parallel()
	for issuer, want := range map[string]string{
		"https://money.weos.cloud/":       "https://money.weos.cloud/door/start",
		"https://door.example/fleet/":     "https://door.example/fleet/door/start",
		"https://door.example/fleet":      "https://door.example/fleet/door/start",
		"https://money.weos.cloud/door/":  "https://money.weos.cloud/door/door/start",
		"https://money.weos.cloud:8443//": "https://money.weos.cloud:8443/door/start",
	} {
		cfg := config.Config{TrustedIssuer: config.TrustedIssuerConfig{
			Issuer: issuer, JWKSURL: doorKeyList, Audience: doorAudience,
		}}
		rec := getProviders(newIssuerBootServer(&cfg))
		entry := issuerEntry(t, decodeProviderEntries(t, rec.Body.Bytes()))
		if entry == nil {
			t.Fatalf("issuer %q: no issuer provider offered: %s", issuer, rec.Body.String())
		}
		if got := fieldText(t, entry, "login_url"); got != want {
			t.Fatalf("issuer %q: login_url = %q, want %q", issuer, got, want)
		}
	}
}

func TestAuthProviders_TrustedIssuerBesideOAuthProviders(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		OAuth: config.OAuthConfig{
			GoogleClientID:     "google-client-id",
			GoogleClientSecret: "google-client-secret",
		},
		TrustedIssuer: config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience},
	}
	rec := getProviders(newIssuerBootServer(&cfg))
	assertProviderNames(t, rec, []string{"google", "issuer"})

	for _, entry := range decodeProviderEntries(t, rec.Body.Bytes()) {
		if fieldText(t, entry, "name") != "google" {
			continue
		}
		if got := entryKeys(entry); len(got) != 1 {
			t.Fatalf("google entry carries %v, want only its name", got)
		}
	}
}

// The door is offered exactly when POST /api/auth/assert is mounted: an
// instance that cannot take the door's assertion has no door to offer.
func TestAuthProviders_IssuerOfferedExactlyWhenTheAssertionRouteMounts(t *testing.T) {
	t.Parallel()
	cases := map[string]config.TrustedIssuerConfig{
		"nothing configured": {},
		"all three":          {Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience},
		"no audience":        {Issuer: doorIssuer, JWKSURL: doorKeyList},
		"no key list":        {Issuer: doorIssuer, Audience: doorAudience},
		"no issuer":          {JWKSURL: doorKeyList, Audience: doorAudience},
		"only the issuer":    {Issuer: doorIssuer},
		"only the key list":  {JWKSURL: doorKeyList},
		"only the audience":  {Audience: doorAudience},
		"whitespace issuer":  {Issuer: "   ", JWKSURL: doorKeyList, Audience: doorAudience},
		"key list over http": {Issuer: doorIssuer, JWKSURL: "http://money.weos.cloud/door/jwks.json", Audience: doorAudience},
		"loopback key list":  {Issuer: doorIssuer, JWKSURL: "http://127.0.0.1:9000/jwks.json", Audience: doorAudience},
		"not a key-list URL": {Issuer: doorIssuer, JWKSURL: "jwks.json", Audience: doorAudience},
	}
	for name, settings := range cases {
		t.Run(name, func(t *testing.T) {
			mounted := handlers.MountTrustedIssuerAssertion(context.Background(), echo.New().Group("/api"),
				settings, &assertionLogCapture{},
				func() *handlers.TrustedIssuerHandler {
					h, _ := newTrustedIssuerHandler(
						&fakeAssertionVerifier{err: &trustedissuer.Refusal{Reason: trustedissuer.ReasonSignature}},
						&assertAuthService{}, &assertionLogCapture{})
					return h
				})

			cfg := config.Config{TrustedIssuer: settings}
			rec := getProviders(newIssuerBootServer(&cfg))
			if rec.Code != http.StatusOK {
				t.Fatalf("code = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			offered := issuerEntry(t, decodeProviderEntries(t, rec.Body.Bytes())) != nil
			if offered != mounted {
				t.Fatalf("issuer provider offered = %v, assertion route mounted = %v: %s", offered, mounted, rec.Body.String())
			}
			if !mounted {
				assertProviderNames(t, rec, []string{})
			}
		})
	}
}

// With no trusted-issuer option at all the handler is what it was before.
func TestAuthProviders_NoTrustedIssuerOption(t *testing.T) {
	t.Parallel()
	cfg := config.Config{TrustedIssuer: config.TrustedIssuerConfig{
		Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience,
	}}
	rec := getProviders(newAuthBootServer(buildProviderRegistry(&cfg), true))
	assertProviderNames(t, rec, []string{})
}
