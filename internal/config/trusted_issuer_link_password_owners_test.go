package config

import "testing"

func TestLoadFromEnvironment_TrustedIssuerLinkPasswordOwners(t *testing.T) {
	if EnvTrustedIssuerLinkPasswordOwners != "TRUSTED_ISSUER_LINK_PASSWORD_OWNERS" {
		t.Fatalf("EnvTrustedIssuerLinkPasswordOwners = %q", EnvTrustedIssuerLinkPasswordOwners)
	}
	cases := map[string]bool{
		"":             false,
		"true":         true,
		" true ":       true,
		"1":            true,
		"false":        false,
		"not-a-bool":   false,
		"TRUE":         true,
		"   ":          false,
		"f":            false,
		"yes-please-x": false,
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv(EnvTrustedIssuerLinkPasswordOwners, value)
			cfg := Default()
			cfg.LoadFromEnvironment()
			if cfg.TrustedIssuer.LinkPasswordOwners != want {
				t.Fatalf("%s=%q: LinkPasswordOwners = %v, want %v",
					EnvTrustedIssuerLinkPasswordOwners, value, cfg.TrustedIssuer.LinkPasswordOwners, want)
			}
		})
	}
}

// The opt-in is not a trusted-issuer setting: alone it configures nothing and
// does not make the API require a sign-in.
func TestTrustedIssuerLinkPasswordOwnersAloneConfiguresNothing(t *testing.T) {
	cfg := Default()
	cfg.TrustedIssuer.LinkPasswordOwners = true
	if !cfg.TrustedIssuer.Unset() {
		t.Fatalf("the opt-in alone counts as a trusted-issuer setting: missing %v", cfg.TrustedIssuer.MissingKeys())
	}
	if cfg.AuthEnabled() {
		t.Fatalf("the opt-in alone made the API require a sign-in")
	}
}
