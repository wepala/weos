package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/internal/config"

	"github.com/cucumber/godog"
)

// TestTrustedIssuerAllowlist runs the acceptance scenarios for story
// wm-63gg0.3 (epic wm-63gg0): an instance with an allowlist admits, through
// the door, only the people the allowlist names, and refuses everyone else
// with the reason allowlist before it decides whom an assertion names; and an
// instance that takes the door's assertions offers the door as a sign-in
// provider, so an expired session can get back to it.
//
// It runs on the story-2 world: the real application, with the assertion
// route mounted through handlers.MountTrustedIssuerAssertion and the providers
// route through handlers.MountAuthProviders with handlers.WithTrustedIssuer,
// as serve.go mounts both. The database survives the restart a scenario asks
// for.
func TestTrustedIssuerAllowlist(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "trusted-issuer-allowlist",
		ScenarioInitializer: initTrustedIssuerAllowlistScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/trusted_issuer_allowlist.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("trusted issuer allowlist acceptance scenarios failed")
	}
}

// alWorld is the story-2 world plus what the sign-in screen asks for.
type alWorld struct {
	*taWorld

	askedProviders bool
	providers      []map[string]json.RawMessage
	// named is the provider the last "offers the sign-in provider" step found.
	named map[string]json.RawMessage
}

func initTrustedIssuerAllowlistScenario(sc *godog.ScenarioContext) {
	w := &alWorld{taWorld: &taWorld{
		logs:     &tiLogCapture{},
		earlier:  map[string]string{},
		accounts: map[string]string{},
	}}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	// The instance
	sc.Step(`^a WeOS instance that trusts login assertions from "([^"]*)" for the audience "([^"]*)"$`, w.instanceTrusting)
	sc.Step(`^a WeOS instance with no trusted issuer configured$`, func() error {
		return w.instanceConfiguredAs(nil, nil, nil)
	})
	sc.Step(`^a WeOS instance configured with a trusted issuer and its key list but no audience$`, func() error {
		w.door = newTiDoor()
		return w.instanceConfiguredAs(ptr("https://money.weos.cloud"), ptr(w.door.server.URL+"/door/jwks.json"), nil)
	})
	sc.Step(`^the instance's allowlist names "([^"]*)"$`, func(email string) error { return w.allowlist(email) })
	sc.Step(`^the instance has no allowlist$`, func() error { return w.allowlist() })
	sc.Step(`^the instance has since been restarted with an allowlist naming only "([^"]*)"$`, func(email string) error {
		return w.allowlist(email)
	})
	sc.Step(`^Google sign-in is also configured on that instance$`, w.googleSignInConfigured)
	sc.Step(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`, w.accountWithOwner)

	// Sign-ins
	sc.Step(`^the door presents an assertion for "([^"]*)" from "([^"]*)" with the subject "([^"]*)"$`,
		func(email, provider, sub string) error { return w.present(email, "", provider, sub) })
	sc.Step(`^"([^"]*)" signed in through the door from "([^"]*)" with the subject "([^"]*)" earlier$`, w.signedInEarlier)
	sc.Step(`^someone who is not signed in asks the instance which sign-in providers it offers$`, w.askProviders)

	// Outcomes of a sign-in
	sc.Step(`^the sign-in succeeds$`, func() error { return w.last().succeeded() })
	sc.Step(`^the sign-in is refused$`, w.refused)
	sc.Step(`^the refusal names the reason "([^"]*)"$`, w.refusalNamesReason)
	sc.Step(`^the sign-in reports that it created a new account$`, func() error { return w.reportsNewAccount(true) })
	sc.Step(`^"([^"]*)" holds no session on the instance$`, w.holdsNoSession)
	sc.Step(`^the store holds no person for "([^"]*)"$`, w.storeHoldsNoPerson)
	sc.Step(`^no identity from "([^"]*)" is linked to "([^"]*)"$`, w.noIdentityLinked)

	// Outcomes of asking for the providers
	sc.Step(`^the instance offers the sign-in provider "([^"]*)" with the sign-in address "([^"]*)"$`, w.offersProviderAt)
	sc.Step(`^that provider carries nothing but its name and its sign-in address$`, w.namedCarriesOnlyNameAndAddress)
	sc.Step(`^the instance offers exactly the sign-in providers "([^"]*)" and "([^"]*)"$`, w.offersExactly)
	sc.Step(`^the "([^"]*)" provider carries no sign-in address$`, w.providerCarriesNoAddress)
	sc.Step(`^the instance offers no sign-in provider named "([^"]*)"$`, w.offersNoProviderNamed)
}

// --- the instance ---

// instanceConfiguredAs starts an instance whose trusted issuer is configured
// only as far as the given settings go; a nil setting is left out. Such an
// instance has no assertion route, so its absence is not a failure to start.
func (w *alWorld) instanceConfiguredAs(issuer, jwksURL, audience *string) error {
	w.setEnv(config.EnvTrustedIssuer, issuer)
	w.setEnv(config.EnvTrustedIssuerJWKSURL, jwksURL)
	w.setEnv(config.EnvTrustedIssuerAudience, audience)
	w.clearAmbientSettings()
	w.wantAssertRoute = false
	return w.boot()
}

func (w *alWorld) googleSignInConfigured() error {
	w.setEnv("GOOGLE_CLIENT_ID", ptr("84735016292-harborlegal.apps.googleusercontent.com"))
	w.setEnv("GOOGLE_CLIENT_SECRET", ptr("GOCSPX-harborlegal-e2e-secret"))
	return w.boot()
}

// --- sign-in outcomes ---

func (w *alWorld) refused() error {
	last := w.last()
	if last == nil {
		return fmt.Errorf("no sign-in has been attempted")
	}
	if last.status != http.StatusUnauthorized {
		return fmt.Errorf("expected the sign-in to be refused (401), got %d: %s", last.status, last.body)
	}
	return nil
}

func (w *alWorld) refusalNamesReason(reason string) error {
	if err := w.refused(); err != nil {
		return err
	}
	if last := w.last(); last.code != reason {
		return fmt.Errorf("expected the refusal to name %q, it named %q: %s", reason, last.code, last.body)
	}
	return nil
}

// holdsNoSession requires the refused sign-in to have set no cookie, and the
// store to hold no credential through which a session could name the person.
func (w *alWorld) holdsNoSession(email string) error {
	if err := w.refused(); err != nil {
		return err
	}
	for _, c := range w.last().cookies {
		if c.Value != "" {
			return fmt.Errorf("a refused sign-in set the cookie %q", c.Name)
		}
	}
	return w.storeHoldsNoPerson(email)
}

func (w *alWorld) storeHoldsNoPerson(email string) error {
	// An unknown email is an empty result, not an error; a real error means
	// the lookup never happened and must not read as absence.
	creds, err := w.credRepo.FindByEmail(context.Background(), email)
	if err != nil {
		return fmt.Errorf("could not check whether the store holds %q: %w", email, err)
	}
	if len(creds) != 0 {
		people := map[string]bool{}
		for _, c := range creds {
			people[c.AgentID()] = true
		}
		return fmt.Errorf("the store holds %d person(s) for %q", len(people), email)
	}
	return nil
}

func (w *alWorld) noIdentityLinked(provider, email string) error {
	creds, err := w.credRepo.FindByEmail(context.Background(), email)
	if err != nil {
		return fmt.Errorf("could not read the credentials of %q: %w", email, err)
	}
	if len(creds) == 0 {
		return fmt.Errorf("the store holds nobody for %q, so a missing link proves nothing", email)
	}
	for _, c := range creds {
		if c.Provider() == provider {
			return fmt.Errorf("an identity from %q is linked to %q", provider, email)
		}
	}
	return nil
}

// --- the providers ---

// askProviders asks the way the sign-in screen does: anonymously, with no
// cookie and no token.
func (w *alWorld) askProviders() error {
	if w.server == nil {
		return fmt.Errorf("the instance has not started")
	}
	req, err := http.NewRequest(http.MethodGet, w.server.URL+"/api/auth/providers", http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /api/auth/providers: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the providers list answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var envelope struct {
		Data struct {
			Providers []map[string]json.RawMessage `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("the providers list is not the envelope: %v: %s", err, raw)
	}
	if envelope.Data.Providers == nil {
		return fmt.Errorf("the answer carries no providers list: %s", raw)
	}
	w.providers, w.askedProviders = envelope.Data.Providers, true
	return nil
}

func (w *alWorld) providerNames() []string {
	names := make([]string, 0, len(w.providers))
	for _, p := range w.providers {
		var name string
		_ = json.Unmarshal(p["name"], &name) // an entry without a name reads as ""
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (w *alWorld) provider(name string) (map[string]json.RawMessage, error) {
	if !w.askedProviders {
		return nil, fmt.Errorf("nobody has asked the instance which sign-in providers it offers")
	}
	for _, p := range w.providers {
		var got string
		if json.Unmarshal(p["name"], &got) == nil && got == name {
			return p, nil
		}
	}
	return nil, nil
}

func (w *alWorld) offersProviderAt(name, address string) error {
	p, err := w.provider(name)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("the instance offers %v, and not %q", w.providerNames(), name)
	}
	var got string
	if err := json.Unmarshal(p["login_url"], &got); err != nil || got != address {
		return fmt.Errorf("the %q provider's sign-in address is %s, want %q", name, p["login_url"], address)
	}
	w.named = p
	return nil
}

func (w *alWorld) namedCarriesOnlyNameAndAddress() error {
	if w.named == nil {
		return fmt.Errorf("no provider has been found to look at")
	}
	keys := make([]string, 0, len(w.named))
	for k := range w.named {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) != 2 || keys[0] != "login_url" || keys[1] != "name" {
		return fmt.Errorf("the provider carries %v, want only its name and its sign-in address", keys)
	}
	return nil
}

func (w *alWorld) offersExactly(a, b string) error {
	if !w.askedProviders {
		return fmt.Errorf("nobody has asked the instance which sign-in providers it offers")
	}
	want := []string{a, b}
	sort.Strings(want)
	got := w.providerNames()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		return fmt.Errorf("the instance offers %v, want exactly %v", got, want)
	}
	return nil
}

func (w *alWorld) providerCarriesNoAddress(name string) error {
	p, err := w.provider(name)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("the instance offers %v, and not %q", w.providerNames(), name)
	}
	if raw, ok := p["login_url"]; ok {
		var address string
		if json.Unmarshal(raw, &address) != nil || address != "" {
			return fmt.Errorf("the %q provider carries the sign-in address %s", name, raw)
		}
	}
	return nil
}

func (w *alWorld) offersNoProviderNamed(name string) error {
	p, err := w.provider(name)
	if err != nil {
		return err
	}
	if p != nil {
		return fmt.Errorf("the instance offers a provider named %q: %v", name, w.providerNames())
	}
	return nil
}
