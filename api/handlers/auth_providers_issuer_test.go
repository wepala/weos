package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	"github.com/labstack/echo/v4"
)

const (
	doorIssuer   = "https://money.weos.cloud"
	doorKeyList  = "https://money.weos.cloud/door/jwks.json"
	doorAudience = "a1b2c3d4"

	// instanceSessionSecret is a SESSION_SECRET of the instance's own. A fleet
	// instance must run with one: under core's public default the assertion
	// route is not mounted and the door is not offered.
	instanceSessionSecret = "a-session-secret-of-this-instance-alone"
)

// newIssuerBootServer mounts the discovery route the way serve.go does: over
// the configured registry, with the instance's configuration.
func newIssuerBootServer(cfg *config.Config) *echo.Echo {
	e := echo.New()
	api := e.Group("/api")
	handlers.MountAuthProviders(api, handlers.NewAuthProvidersHandler(
		buildProviderRegistry(cfg), handlers.WithTrustedIssuer(*cfg)))
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
	cfg := config.Config{SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{
		Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience,
	}}
	rec := getProviders(newIssuerBootServer(&cfg))
	assertProviderNames(t, rec, []string{"issuer"})

	entry := decodeProviderEntries(t, rec.Body.Bytes())[0]
	want := []string{"accepted_provider_keys", "joins_door_and_google_apple_by_email", "login_url", "name"}
	if got := entryKeys(entry); !slices.Equal(got, want) {
		t.Fatalf("issuer entry carries %v, want exactly %v", got, want)
	}
	if got := fieldText(t, entry, "login_url"); got != "https://money.weos.cloud/door/start" {
		t.Fatalf("login_url = %q, want https://money.weos.cloud/door/start", got)
	}
}

// The issuer entry lists the provider keys an assertion may name on this
// instance, so an issuer can tell before it sends a person whether the
// instance accepts the key it would send. An older core refuses a key it does
// not know as claims, the same reason as a missing subject, so a refusal
// cannot tell the issuer that.
func TestAuthProviders_TrustedIssuerListsTheProviderKeysAnAssertionMayName(t *testing.T) {
	t.Parallel()
	cfg := config.Config{SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{
		Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience,
	}}
	rec := getProviders(newIssuerBootServer(&cfg))
	entry := issuerEntry(t, decodeProviderEntries(t, rec.Body.Bytes()))
	if entry == nil {
		t.Fatalf("no issuer provider offered: %s", rec.Body.String())
	}
	var got []string
	if err := json.Unmarshal(entry["accepted_provider_keys"], &got); err != nil {
		t.Fatalf("accepted_provider_keys = %s, want a list of provider keys (%v)", entry["accepted_provider_keys"], err)
	}
	if !slices.Contains(got, "door") {
		t.Fatalf("accepted_provider_keys = %v, want it to list door", got)
	}
	want := application.OAuthProviderKeys()
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("accepted_provider_keys = %v, want %v: the keys the verifier accepts, sorted", got, want)
	}
}

// The issuer entry says that this core joins a door identity and a Google or
// Apple identity with the same email into one person. An older core creates a
// second, empty person instead, so an issuer offers a second sign-in method to
// an instance only when the entry says this. The field is true when present;
// an older core leaves it out.
func TestAuthProviders_TrustedIssuerSaysItJoinsADoorAndAGoogleOrAppleIdentityByEmail(t *testing.T) {
	t.Parallel()
	cfg := config.Config{SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{
		Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience,
	}}
	rec := getProviders(newIssuerBootServer(&cfg))
	entry := issuerEntry(t, decodeProviderEntries(t, rec.Body.Bytes()))
	if entry == nil {
		t.Fatalf("no issuer provider offered: %s", rec.Body.String())
	}
	raw, ok := entry["joins_door_and_google_apple_by_email"]
	if !ok {
		t.Fatalf("issuer entry has no joins_door_and_google_apple_by_email: %s", rec.Body.String())
	}
	var joins bool
	if err := json.Unmarshal(raw, &joins); err != nil || !joins {
		t.Fatalf("joins_door_and_google_apple_by_email = %s, want true (%v)", raw, err)
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
		cfg := config.Config{SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{
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
		SessionSecret: instanceSessionSecret,
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
	all3 := config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience}
	own := func(settings config.TrustedIssuerConfig) config.Config {
		return config.Config{SessionSecret: instanceSessionSecret, TrustedIssuer: settings}
	}
	cases := map[string]config.Config{
		"nothing configured": own(config.TrustedIssuerConfig{}),
		"all three":          own(all3),
		"no audience":        own(config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: doorKeyList}),
		"no key list":        own(config.TrustedIssuerConfig{Issuer: doorIssuer, Audience: doorAudience}),
		"no issuer":          own(config.TrustedIssuerConfig{JWKSURL: doorKeyList, Audience: doorAudience}),
		"only the issuer":    own(config.TrustedIssuerConfig{Issuer: doorIssuer}),
		"only the key list":  own(config.TrustedIssuerConfig{JWKSURL: doorKeyList}),
		"only the audience":  own(config.TrustedIssuerConfig{Audience: doorAudience}),
		"whitespace issuer":  own(config.TrustedIssuerConfig{Issuer: "   ", JWKSURL: doorKeyList, Audience: doorAudience}),
		"key list over http": own(config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: "http://money.weos.cloud/door/jwks.json", Audience: doorAudience}),
		"loopback key list":  own(config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: "http://127.0.0.1:9000/jwks.json", Audience: doorAudience}),
		"not a key-list URL": own(config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: "jwks.json", Audience: doorAudience}),
		"all three, default session secret": {
			SessionSecret: config.DefaultSessionSecret, TrustedIssuer: all3,
		},
		"all three, no session secret": {TrustedIssuer: all3},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			mounted := handlers.MountTrustedIssuerAssertion(context.Background(), echo.New().Group("/api"),
				cfg, &assertionLogCapture{},
				func() *handlers.TrustedIssuerHandler {
					h, _ := newTrustedIssuerHandler(
						&fakeAssertionVerifier{err: &trustedissuer.Refusal{Reason: trustedissuer.ReasonSignature}},
						&assertAuthService{}, &assertionLogCapture{})
					return h
				})

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

// Under core's public session secret the door is not offered, however
// completely the trusted issuer is configured.
func TestAuthProviders_NoDoorUnderTheDefaultSessionSecret(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.TrustedIssuer = config.TrustedIssuerConfig{Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience}
	assertProviderNames(t, getProviders(newIssuerBootServer(&cfg)), []string{})
}

// With no trusted-issuer option at all the handler is what it was before.
func TestAuthProviders_NoTrustedIssuerOption(t *testing.T) {
	t.Parallel()
	cfg := config.Config{SessionSecret: instanceSessionSecret, TrustedIssuer: config.TrustedIssuerConfig{
		Issuer: doorIssuer, JWKSURL: doorKeyList, Audience: doorAudience,
	}}
	rec := getProviders(newAuthBootServer(buildProviderRegistry(&cfg), true))
	assertProviderNames(t, rec, []string{})
}
