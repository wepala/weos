package config

import "testing"

func TestTrustedIssuerConfig_IssuerID(t *testing.T) {
	cases := map[string]string{
		"https://money.weos.cloud":      "https://money.weos.cloud",
		"https://money.weos.cloud/":     "https://money.weos.cloud",
		" https://money.weos.cloud// ":  "https://money.weos.cloud",
		"https://door.example/fleet/":   "https://door.example/fleet",
		"https://money.weos.cloud:8443": "https://money.weos.cloud:8443",
		"":                              "",
		"/":                             "",
		"  ":                            "",
	}
	for raw, want := range cases {
		if got := (TrustedIssuerConfig{Issuer: raw}).IssuerID(); got != want {
			t.Fatalf("IssuerID(%q) = %q, want %q", raw, got, want)
		}
		if got := NormalizeTrustedIssuer(raw); got != want {
			t.Fatalf("NormalizeTrustedIssuer(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The setting is normalized once, at load, so every reader sees the same value.
func TestLoadFromEnvironment_TrustedIssuerTrimsTrailingSlashes(t *testing.T) {
	t.Setenv(EnvTrustedIssuer, " https://money.weos.cloud// ")
	cfg := Default()
	cfg.LoadFromEnvironment()
	if cfg.TrustedIssuer.Issuer != "https://money.weos.cloud" {
		t.Fatalf("TrustedIssuer.Issuer = %q, want https://money.weos.cloud", cfg.TrustedIssuer.Issuer)
	}
}

// A value that is only slashes is not set, so it neither configures the route
// nor counts toward making the API require a sign-in.
func TestLoadFromEnvironment_TrustedIssuerOfOnlySlashesIsNotSet(t *testing.T) {
	t.Setenv(EnvTrustedIssuer, " // ")
	t.Setenv(EnvTrustedIssuerJWKSURL, "")
	t.Setenv(EnvTrustedIssuerAudience, "")
	cfg := Default()
	cfg.LoadFromEnvironment()
	if !cfg.TrustedIssuer.Unset() {
		t.Fatalf("a slash-only TRUSTED_ISSUER counts as set: %+v", cfg.TrustedIssuer)
	}
}
