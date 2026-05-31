package config

import "testing"

// allAppleEnv sets every Apple credential env var to a valid-looking value.
func allAppleEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APPLE_CLIENT_ID", "app.finexity.web")
	t.Setenv("APPLE_TEAM_ID", "TEAM123")
	t.Setenv("APPLE_KEY_ID", "KEY123")
	t.Setenv("APPLE_PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END PRIVATE KEY-----")
}

func TestLoadFromEnvironment_Apple(t *testing.T) {
	allAppleEnv(t)
	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.OAuth.AppleClientID != "app.finexity.web" {
		t.Errorf("AppleClientID = %q", cfg.OAuth.AppleClientID)
	}
	if cfg.OAuth.AppleTeamID != "TEAM123" {
		t.Errorf("AppleTeamID = %q", cfg.OAuth.AppleTeamID)
	}
	if cfg.OAuth.AppleKeyID != "KEY123" {
		t.Errorf("AppleKeyID = %q", cfg.OAuth.AppleKeyID)
	}
	if !cfg.OAuth.AppleConfigured() {
		t.Error("AppleConfigured() = false, want true when all four env vars are set")
	}
	if !cfg.OAuthEnabled() {
		t.Error("OAuthEnabled() = false, want true when Apple is configured")
	}
}

// TestLoadFromEnvironment_ApplePrivateKeyUnescaping guards the single most
// regression-prone line in the Apple wiring: a .p8 key supplied as a single-line
// env var arrives with the newlines escaped as the two characters `\n`, and the
// ES256 signer can't parse the PEM block unless they're restored to real
// newlines.
func TestLoadFromEnvironment_ApplePrivateKeyUnescaping(t *testing.T) {
	t.Run("escaped newlines are restored", func(t *testing.T) {
		t.Setenv("APPLE_PRIVATE_KEY", `-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END PRIVATE KEY-----`)
		cfg := Default()
		cfg.LoadFromEnvironment()
		want := "-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END PRIVATE KEY-----"
		if cfg.OAuth.ApplePrivateKey != want {
			t.Errorf("ApplePrivateKey = %q, want %q", cfg.OAuth.ApplePrivateKey, want)
		}
	})

	t.Run("real newlines are left intact", func(t *testing.T) {
		key := "-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END PRIVATE KEY-----"
		t.Setenv("APPLE_PRIVATE_KEY", key)
		cfg := Default()
		cfg.LoadFromEnvironment()
		if cfg.OAuth.ApplePrivateKey != key {
			t.Errorf("ApplePrivateKey = %q, want it unchanged %q", cfg.OAuth.ApplePrivateKey, key)
		}
	})
}

func TestAppleConfigured(t *testing.T) {
	base := func() OAuthConfig {
		return OAuthConfig{
			AppleClientID:   "app.finexity.web",
			AppleTeamID:     "TEAM123",
			AppleKeyID:      "KEY123",
			ApplePrivateKey: "pem",
		}
	}

	if !base().AppleConfigured() {
		t.Fatal("fully populated config should be AppleConfigured")
	}

	// Each field is individually required.
	clear := []func(*OAuthConfig){
		func(c *OAuthConfig) { c.AppleClientID = "" },
		func(c *OAuthConfig) { c.AppleTeamID = "" },
		func(c *OAuthConfig) { c.AppleKeyID = "" },
		func(c *OAuthConfig) { c.ApplePrivateKey = "" },
	}
	for i, mut := range clear {
		cfg := base()
		mut(&cfg)
		if cfg.AppleConfigured() {
			t.Errorf("case %d: AppleConfigured() = true with a missing field, want false", i)
		}
	}

	if (OAuthConfig{}).AppleConfigured() {
		t.Error("empty config should not be AppleConfigured")
	}
}
