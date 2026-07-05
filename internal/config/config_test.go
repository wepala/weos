package config

import (
	"reflect"
	"testing"
	"time"
)

func TestLoadFromEnvironment_SMTP(t *testing.T) {
	t.Setenv("SMTP_HOST", "mail.example.com")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("SMTP_USERNAME", "user")
	t.Setenv("SMTP_PASSWORD", "pass")
	t.Setenv("SMTP_FROM", "noreply@example.com")

	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.SMTP.Host != "mail.example.com" {
		t.Fatalf("expected Host mail.example.com, got %s", cfg.SMTP.Host)
	}
	if cfg.SMTP.Port != "2525" {
		t.Fatalf("expected Port 2525, got %s", cfg.SMTP.Port)
	}
	if cfg.SMTP.Username != "user" {
		t.Fatalf("expected Username user, got %s", cfg.SMTP.Username)
	}
	if cfg.SMTP.Password != "pass" {
		t.Fatalf("expected Password pass, got %s", cfg.SMTP.Password)
	}
	if cfg.SMTP.From != "noreply@example.com" {
		t.Fatalf("expected From noreply@example.com, got %s", cfg.SMTP.From)
	}
}

func TestLoadFromEnvironment_SMTP_NotSet(t *testing.T) {
	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.SMTP.Host != "" {
		t.Fatalf("expected empty Host, got %s", cfg.SMTP.Host)
	}
	if cfg.SMTP.Port != "" {
		t.Fatalf("expected empty Port, got %s", cfg.SMTP.Port)
	}
}

func TestLoadFromEnvironment_NetSuiteScopes(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{"comma-separated", "rest_webservices,openid", []string{"rest_webservices", "openid"}},
		{"space-separated", "rest_webservices openid", []string{"rest_webservices", "openid"}},
		{"mixed with whitespace", " rest_webservices ,  openid\trestlets ", []string{"rest_webservices", "openid", "restlets"}},
		{"single scope", "openid", []string{"openid"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NETSUITE_SCOPES", tc.env)
			cfg := Default()
			cfg.LoadFromEnvironment()
			if !reflect.DeepEqual(cfg.OAuth.NetSuiteScopes, tc.want) {
				t.Fatalf("NetSuiteScopes = %#v, want %#v", cfg.OAuth.NetSuiteScopes, tc.want)
			}
		})
	}
}

func TestLoadFromEnvironment_NetSuiteScopes_NotSet(t *testing.T) {
	cfg := Default()
	cfg.LoadFromEnvironment()
	if cfg.OAuth.NetSuiteScopes != nil {
		t.Fatalf("expected nil NetSuiteScopes when env not set, got %#v", cfg.OAuth.NetSuiteScopes)
	}
}

func TestDefaultOAuthProvider(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{
			name: "explicit override wins",
			mut: func(c *Config) {
				c.OAuth.DefaultProvider = "netsuite"
				c.OAuth.GoogleClientID = "g"
				c.OAuth.GoogleClientSecret = "s"
			},
			want: "netsuite",
		},
		{
			name: "google preferred when configured",
			mut: func(c *Config) {
				c.OAuth.GoogleClientID = "g"
				c.OAuth.GoogleClientSecret = "s"
				c.OAuth.NetSuiteClientID = "n"
				c.OAuth.NetSuiteClientSecret = "ns"
				c.OAuth.NetSuiteAccountID = "1234567"
			},
			want: "google",
		},
		{
			name: "netsuite-only deployment",
			mut: func(c *Config) {
				c.OAuth.NetSuiteClientID = "n"
				c.OAuth.NetSuiteClientSecret = "ns"
				c.OAuth.NetSuiteAccountID = "1234567"
			},
			want: "netsuite",
		},
		{
			name: "apple-only deployment",
			mut: func(c *Config) {
				c.OAuth.AppleClientID = "app.finexity.web"
				c.OAuth.AppleTeamID = "TEAM123"
				c.OAuth.AppleKeyID = "KEY123"
				c.OAuth.ApplePrivateKey = "pem"
			},
			want: "apple",
		},
		{
			name: "google wins over apple",
			mut: func(c *Config) {
				c.OAuth.GoogleClientID = "g"
				c.OAuth.GoogleClientSecret = "s"
				c.OAuth.AppleClientID = "app.finexity.web"
				c.OAuth.AppleTeamID = "TEAM123"
				c.OAuth.AppleKeyID = "KEY123"
				c.OAuth.ApplePrivateKey = "pem"
			},
			want: "google",
		},
		{
			name: "netsuite wins over apple",
			mut: func(c *Config) {
				c.OAuth.NetSuiteClientID = "n"
				c.OAuth.NetSuiteClientSecret = "ns"
				c.OAuth.NetSuiteAccountID = "1234567"
				c.OAuth.AppleClientID = "app.finexity.web"
				c.OAuth.AppleTeamID = "TEAM123"
				c.OAuth.AppleKeyID = "KEY123"
				c.OAuth.ApplePrivateKey = "pem"
			},
			want: "netsuite",
		},
		{
			name: "fallback when nothing configured",
			mut:  func(*Config) {},
			want: "google",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mut(&cfg)
			if got := cfg.DefaultOAuthProvider(); got != tc.want {
				t.Fatalf("DefaultOAuthProvider() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadFromEnvironment_DefaultOAuthProvider(t *testing.T) {
	t.Setenv("OAUTH_DEFAULT_PROVIDER", "netsuite")
	cfg := Default()
	cfg.LoadFromEnvironment()
	if cfg.OAuth.DefaultProvider != "netsuite" {
		t.Fatalf("DefaultProvider = %q, want %q", cfg.OAuth.DefaultProvider, "netsuite")
	}
}

func TestLoadFromEnvironment_OxigraphURLAutoEnables(t *testing.T) {
	t.Setenv("OXIGRAPH_URL", "http://localhost:7878")
	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.Oxigraph.URL != "http://localhost:7878" {
		t.Fatalf("URL = %q, want http://localhost:7878", cfg.Oxigraph.URL)
	}
	if !cfg.Oxigraph.Enabled {
		t.Fatal("setting OXIGRAPH_URL must auto-enable the projection")
	}
	if !cfg.Oxigraph.Active() {
		t.Fatal("Active() should be true when URL is set and auto-enabled")
	}
}

func TestLoadFromEnvironment_OxigraphEnabledFalseOverridesURL(t *testing.T) {
	// Staged-rollout path: URL configured but the projection explicitly held
	// off. OXIGRAPH_ENABLED is parsed after the URL auto-enable, so it wins.
	t.Setenv("OXIGRAPH_URL", "http://localhost:7878")
	t.Setenv("OXIGRAPH_ENABLED", "false")
	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.Oxigraph.URL != "http://localhost:7878" {
		t.Fatalf("URL = %q, want it preserved", cfg.Oxigraph.URL)
	}
	if cfg.Oxigraph.Enabled {
		t.Fatal("OXIGRAPH_ENABLED=false must override the URL auto-enable")
	}
	if cfg.Oxigraph.Active() {
		t.Fatal("Active() must be false when Enabled is overridden to false")
	}
}

func TestLoadFromEnvironment_OxigraphAccountStorePathEnablesPerAccount(t *testing.T) {
	t.Setenv("OXIGRAPH_ACCOUNT_STORE_PATH", "/data/graph")
	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.Oxigraph.AccountStorePath != "/data/graph" {
		t.Fatalf("AccountStorePath = %q, want /data/graph", cfg.Oxigraph.AccountStorePath)
	}
	if !cfg.Oxigraph.PerAccount() {
		t.Fatal("PerAccount() must be true when AccountStorePath is set")
	}
	if !cfg.Oxigraph.Active() {
		t.Fatal("Active() must be true in per-account mode")
	}
}

func TestOxigraphConfig_PerAccountAndActiveDefaults(t *testing.T) {
	var o OxigraphConfig // zero value: no per-account, not active
	if o.PerAccount() {
		t.Error("zero-value config must not be per-account")
	}
	if o.Active() {
		t.Error("zero-value config must be inactive")
	}
	// A single embedded path is active but NOT per-account.
	o.Path = "/data/graph.db"
	if o.PerAccount() {
		t.Error("a single Path must not report per-account mode")
	}
	if !o.Active() {
		t.Error("a single Path must be active")
	}
}

func TestLoadFromEnvironment_OxigraphOptions(t *testing.T) {
	t.Setenv("OXIGRAPH_URL", "http://localhost:7878")
	t.Setenv("OXIGRAPH_USERNAME", "neo")
	t.Setenv("OXIGRAPH_PASSWORD", "trinity")
	t.Setenv("OXIGRAPH_QUERY_TIMEOUT_SECONDS", "15")
	t.Setenv("OXIGRAPH_REBUILD", "true")
	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.Oxigraph.Username != "neo" || cfg.Oxigraph.Password != "trinity" {
		t.Errorf("creds = %q/%q, want neo/trinity", cfg.Oxigraph.Username, cfg.Oxigraph.Password)
	}
	if cfg.Oxigraph.QueryTimeoutSeconds != 15 {
		t.Errorf("QueryTimeoutSeconds = %d, want 15", cfg.Oxigraph.QueryTimeoutSeconds)
	}
	if !cfg.Oxigraph.Rebuild {
		t.Error("OXIGRAPH_REBUILD=true should set Rebuild")
	}
}

func TestLoadFromEnvironment_OxigraphInvalidNumericIgnored(t *testing.T) {
	// A non-numeric / non-positive timeout is ignored, leaving the default.
	t.Setenv("OXIGRAPH_QUERY_TIMEOUT_SECONDS", "not-a-number")
	cfg := Default()
	cfg.LoadFromEnvironment()
	if cfg.Oxigraph.QueryTimeoutSeconds != 0 {
		t.Errorf("invalid timeout should be ignored (kept default 0); got %d", cfg.Oxigraph.QueryTimeoutSeconds)
	}
}

func TestLoadFromEnvironment_OxigraphNotSet(t *testing.T) {
	cfg := Default()
	cfg.LoadFromEnvironment()
	if cfg.Oxigraph.URL != "" || cfg.Oxigraph.Enabled {
		t.Fatalf("unset Oxigraph env must leave URL empty and disabled; got %q/%v",
			cfg.Oxigraph.URL, cfg.Oxigraph.Enabled)
	}
	if cfg.Oxigraph.Active() {
		t.Fatal("Active() must be false when nothing is configured")
	}
}

// A whitespace-only override (common in templated env files) must not
// wipe pericarp's default scope list — empty parsing keeps Scopes nil
// so the default kicks in downstream.
func TestLoadFromEnvironment_WorkerTunables(t *testing.T) {
	// WORKER_RUN_IN_PROCESS is deliberately NOT read by LoadFromEnvironment so
	// short-lived CLI commands can't be made to start workers via the env; only
	// the serve command honors it. Set it here to prove it has no effect.
	t.Setenv("WORKER_RUN_IN_PROCESS", "true")
	t.Setenv("WORKER_BATCH_SIZE", "250")
	t.Setenv("WORKER_POLL_INTERVAL_MS", "500")
	t.Setenv("WORKER_MAX_RETRIES", "9")
	t.Setenv("WORKER_RETRY_BACKOFF_MS", "200")
	t.Setenv("WORKER_MAX_RETRY_BACKOFF_MS", "8000")
	t.Setenv("WORKER_LAG_LOG_INTERVAL_SECONDS", "60")
	cfg := Default()
	cfg.LoadFromEnvironment()

	w := cfg.Worker
	if w.RunInProcess {
		t.Error("LoadFromEnvironment must NOT read WORKER_RUN_IN_PROCESS (serve owns that decision)")
	}
	if w.BatchSize != 250 {
		t.Errorf("BatchSize = %d, want 250", w.BatchSize)
	}
	if w.PollInterval != 500*time.Millisecond {
		t.Errorf("PollInterval = %s, want 500ms", w.PollInterval)
	}
	if w.MaxRetries != 9 {
		t.Errorf("MaxRetries = %d, want 9", w.MaxRetries)
	}
	if w.RetryBackoff != 200*time.Millisecond {
		t.Errorf("RetryBackoff = %s, want 200ms", w.RetryBackoff)
	}
	if w.MaxRetryBackoff != 8*time.Second {
		t.Errorf("MaxRetryBackoff = %s, want 8s", w.MaxRetryBackoff)
	}
	if w.LagLogInterval != 60*time.Second {
		t.Errorf("LagLogInterval = %s, want 60s", w.LagLogInterval)
	}
}

// Unset/invalid backoff overrides must leave the Default() values intact so
// operators who tune nothing keep pericarp's 100ms→5s backoff curve.
func TestLoadFromEnvironment_WorkerBackoffDefaultsPreserved(t *testing.T) {
	t.Setenv("WORKER_RETRY_BACKOFF_MS", "0")        // non-positive: ignored
	t.Setenv("WORKER_MAX_RETRY_BACKOFF_MS", "nope") // non-numeric: ignored
	cfg := Default()
	cfg.LoadFromEnvironment()

	if cfg.Worker.RetryBackoff != 100*time.Millisecond {
		t.Errorf("RetryBackoff = %s, want default 100ms", cfg.Worker.RetryBackoff)
	}
	if cfg.Worker.MaxRetryBackoff != 5*time.Second {
		t.Errorf("MaxRetryBackoff = %s, want default 5s", cfg.Worker.MaxRetryBackoff)
	}
}

func TestLoadFromEnvironment_NetSuiteScopes_WhitespaceOnly(t *testing.T) {
	for _, value := range []string{" ", "\t", " , \t , ", ",,"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("NETSUITE_SCOPES", value)
			cfg := Default()
			cfg.LoadFromEnvironment()
			if cfg.OAuth.NetSuiteScopes != nil {
				t.Fatalf("expected nil NetSuiteScopes for whitespace-only env, got %#v", cfg.OAuth.NetSuiteScopes)
			}
		})
	}
}
