package config

import (
	"reflect"
	"testing"
)

func TestLoadFromEnvironment_TrustedIssuer(t *testing.T) {
	t.Setenv("TRUSTED_ISSUER", " https://money.weos.cloud ")
	t.Setenv("TRUSTED_ISSUER_JWKS_URL", "https://money.weos.cloud/door/jwks.json")
	t.Setenv("TRUSTED_ISSUER_AUDIENCE", "a1b2c3d4")

	cfg := Default()
	cfg.LoadFromEnvironment()

	want := TrustedIssuerConfig{
		Issuer:   "https://money.weos.cloud",
		JWKSURL:  "https://money.weos.cloud/door/jwks.json",
		Audience: "a1b2c3d4",
	}
	if cfg.TrustedIssuer != want {
		t.Fatalf("TrustedIssuer = %+v, want %+v", cfg.TrustedIssuer, want)
	}
	if !cfg.TrustedIssuer.Configured() {
		t.Fatalf("expected a fully configured trusted issuer")
	}
}

func TestLoadFromEnvironment_TrustedIssuer_NotSet(t *testing.T) {
	for _, key := range []string{"TRUSTED_ISSUER", "TRUSTED_ISSUER_JWKS_URL", "TRUSTED_ISSUER_AUDIENCE"} {
		t.Setenv(key, "")
	}
	cfg := Default()
	cfg.LoadFromEnvironment()
	if cfg.TrustedIssuer != (TrustedIssuerConfig{}) {
		t.Fatalf("expected no trusted issuer by default, got %+v", cfg.TrustedIssuer)
	}
	if cfg.TrustedIssuer.Configured() {
		t.Fatalf("an unset trusted issuer must not read as configured")
	}
}

func TestTrustedIssuerConfig_MissingKeys(t *testing.T) {
	const (
		iss = "https://money.weos.cloud"
		url = "https://money.weos.cloud/door/jwks.json"
		aud = "a1b2c3d4"
	)
	cases := map[string]struct {
		cfg  TrustedIssuerConfig
		want []string
	}{
		"none":             {TrustedIssuerConfig{}, []string{"TRUSTED_ISSUER", "TRUSTED_ISSUER_JWKS_URL", "TRUSTED_ISSUER_AUDIENCE"}},
		"all":              {TrustedIssuerConfig{Issuer: iss, JWKSURL: url, Audience: aud}, nil},
		"no audience":      {TrustedIssuerConfig{Issuer: iss, JWKSURL: url}, []string{"TRUSTED_ISSUER_AUDIENCE"}},
		"no key list":      {TrustedIssuerConfig{Issuer: iss, Audience: aud}, []string{"TRUSTED_ISSUER_JWKS_URL"}},
		"no issuer":        {TrustedIssuerConfig{JWKSURL: url, Audience: aud}, []string{"TRUSTED_ISSUER"}},
		"only audience":    {TrustedIssuerConfig{Audience: aud}, []string{"TRUSTED_ISSUER", "TRUSTED_ISSUER_JWKS_URL"}},
		"whitespace issue": {TrustedIssuerConfig{Issuer: "  ", JWKSURL: url, Audience: aud}, []string{"TRUSTED_ISSUER"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := c.cfg.MissingKeys(); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("MissingKeys() = %v, want %v", got, c.want)
			}
			if got, want := c.cfg.Configured(), len(c.want) == 0; got != want {
				t.Fatalf("Configured() = %v, want %v", got, want)
			}
		})
	}
}
