// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// OAuthConfig holds configuration for OAuth authentication.
//
// Provider credentials are independent — set whichever providers you
// want available to the auth registry. OAuthEnabled returns true if at
// least one provider is fully configured.
type OAuthConfig struct {
	GoogleClientID     string
	GoogleClientSecret string

	// NetSuite OAuth 2.0 (SuiteTalk REST). AccountID accepts the bare
	// account number for production (e.g. "1234567") or the underscore
	// suffix for sandboxes (e.g. "1234567_SB1") — pericarp derives the
	// auth/token endpoints from it.
	NetSuiteClientID     string
	NetSuiteClientSecret string
	NetSuiteAccountID    string
	// NetSuiteScopes overrides pericarp's default scope list. Leave nil/empty
	// to fall back to ["rest_webservices"]. Include "openid" when the binary
	// needs to call NetSuite's userinfo endpoint — without it, the token
	// exchange succeeds but the userinfo fetch returns 400 "Unable to
	// authenticate", because NetSuite's userinfo is gated behind OIDC.
	NetSuiteScopes []string

	// Sign in with Apple. ClientID is the Apple Services ID (e.g.
	// "app.finexity.web"), NOT the app bundle id. PrivateKey is the PEM
	// contents of the .p8 key downloaded from the Apple Developer portal —
	// pericarp's provider signs the client-secret JWT (ES256) from it.
	//
	// Apple delivers the authorization code via an HTTP POST (response_mode=
	// form_post), not a GET query string, so the /api/auth/callback route is
	// registered for both methods.
	//
	// KNOWN LIMITATION: Apple's callback is a cross-site top-level POST, on
	// which the browser does NOT send a SameSite=Lax cookie. pericarp currently
	// hardcodes the short-lived OAuth flow cookie to SameSite=Lax
	// (flowCookieOptions in gorilla_session_manager.go), so the callback can't
	// read the flow state and Apple sign-in won't complete in production until
	// pericarp issues that cookie SameSite=None; Secure. Tracked as upstream
	// follow-up; everything else for Apple (config, provider, form_post
	// handling, new-account signal) is in place here.
	AppleClientID   string
	AppleTeamID     string
	AppleKeyID      string
	ApplePrivateKey string

	FrontendURL         string
	BaseURL             string // Public URL for OAuth metadata/endpoints (e.g. https://example.com)
	JWTSigningKey       string // PEM-encoded RSA private key, or "auto" to generate ephemeral key
	DynamicRegistration bool   // Enable OAuth Dynamic Client Registration (RFC 7591)

	// DefaultProvider is the provider used by /api/auth/login when the
	// caller doesn't pass a provider query param (the admin SPA does
	// this). Empty means "auto-pick from the configured registry" —
	// see DefaultOAuthProvider below.
	DefaultProvider string
}

// SMTPConfig holds configuration for outbound email via SMTP.
// An SMTPSender is created only when Host is set, From is set and parses as a
// valid email address, and the configured port is accepted by the SMTP sender.
// If Port is left empty, the SMTP sender uses the default port "587" (STARTTLS);
// port "465" is rejected and will prevent SMTP from being enabled.
type SMTPConfig struct {
	Host     string // SMTP server hostname (required to enable email)
	Port     string // SMTP server port; if empty, SMTPSender uses default "587"
	Username string // SMTP auth username (optional — skips auth if empty)
	Password string // SMTP auth password
	From     string // Sender email address (required to enable email)
}

// Config holds the standard configuration used by all applications.
// Each application is responsible for providing a Config instance,
// which may be populated from environment variables, command flags, or other sources.
type Config struct {
	// DatabaseDSN is the database connection string.
	// For SQLite: "weos.db" or "file:weos.db?cache=shared&_foreign_keys=1"
	// For PostgreSQL: "host=localhost user=postgres password=postgres dbname=weos port=5432 sslmode=disable"
	DatabaseDSN string

	// LogLevel specifies the logging level.
	// Valid values: "debug", "info", "warn", "error"
	// Default: "info"
	LogLevel string

	// Server holds configuration for the HTTP server.
	Server ServerConfig

	// SessionSecret is the secret key for session cookies.
	SessionSecret string

	// PasswordAuthEnabled toggles the email + password register/login
	// endpoints. Off by default — enabling it without a non-default
	// SessionSecret is a footgun because sessions become forgeable.
	// Callers should ensure SessionSecret is set to a production-safe
	// value before enabling password authentication.
	PasswordAuthEnabled bool

	// LLM holds configuration for LLM integrations.
	LLM LLMConfig

	// OAuth holds configuration for OAuth authentication.
	OAuth OAuthConfig

	// SMTP holds configuration for outbound email.
	SMTP SMTPConfig

	// Storage holds configuration for file storage backends.
	Storage StorageConfig

	// Oxigraph holds configuration for the optional knowledge-graph projection.
	// When Oxigraph.URL is set, resource and triple events are mirrored to a
	// SPARQL endpoint alongside the existing read-models, and the
	// `knowledge-graph` MCP tool group is enabled.
	Oxigraph OxigraphConfig

	// Worker holds configuration for the background subscriber runtime that
	// runs peripheral event handlers (knowledge-graph sync, denormalization)
	// off checkpointed feeds, isolated from the synchronous write path.
	Worker WorkerConfig
}

// WorkerConfig tunes the background subscriber runtime (epic #365). Subscribers
// process the event store's global feed asynchronously off named checkpoints,
// recovering from crashes by resuming at their last committed position. These
// knobs control batching, polling, retry/backoff, and whether the workers run
// inside this process.
type WorkerConfig struct {
	// RunInProcess controls whether background subscribers run in this
	// process. The `serve` command sets it true so workers run alongside the
	// API; short-lived CLI commands leave it false so they can inspect or
	// reset checkpoints and parked events without starting processing.
	RunInProcess bool
	// BatchSize is how many events a subscriber reads per cycle. Default 100.
	BatchSize int
	// PollInterval is how long an idle subscriber waits before re-checking the
	// feed; it is also the retry delay after a failed batch (per-event poison
	// retries use RetryBackoff instead). Default 1s.
	PollInterval time.Duration
	// MaxRetries is how many times a failing handler is retried per event
	// (after the initial attempt) before the event is parked. Default 5.
	MaxRetries int
	// RetryBackoff is the first retry delay; it doubles per attempt up to
	// MaxRetryBackoff. Defaults 100ms and 5s.
	RetryBackoff    time.Duration
	MaxRetryBackoff time.Duration
	// LagLogInterval is how often each subscriber's checkpoint lag is logged.
	// Zero disables lag logging. Default 30s.
	LagLogInterval time.Duration
}

// IsPostgresDSN reports whether dsn targets PostgreSQL — a "host=" libpq DSN
// or a postgres(ql):// URI. Everything else is treated as SQLite. This is the
// single dialect predicate — every DSN-driven driver decision (e.g. the GORM
// provider's DialectorForDSN, the worker runtime's wake-mechanism choice)
// must call it so those decisions never diverge between consumers.
func IsPostgresDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "host=") ||
		strings.Contains(dsn, "postgres://") ||
		strings.Contains(dsn, "postgresql://")
}

// IsPostgres reports whether the configured DSN targets PostgreSQL.
func (c Config) IsPostgres() bool {
	return IsPostgresDSN(c.DatabaseDSN)
}

// OxigraphConfig holds configuration for the optional Oxigraph knowledge-graph
// projection. WeOS speaks to Oxigraph over HTTP using the SPARQL 1.1 protocol
// (you run `oxigraph serve` separately). The projection runs only when both
// URL and Enabled are set — see Active().
type OxigraphConfig struct {
	URL string // e.g. http://localhost:7878 — empty disables the projection
	// Path selects the EMBEDDED backend: an in-process oxigraph store opened
	// on this local directory, no external endpoint. When set it takes
	// precedence over URL (the desktop/embedded case). Requires a binary
	// built with the `oxigraph_embedded` tag; otherwise the provider logs
	// and falls back to nop, like an unreachable endpoint.
	Path string
	// Enabled gates the projection independently of URL so operators can
	// stage a rollout (set URL but keep Enabled=false). LoadFromEnvironment
	// flips Enabled=true automatically when OXIGRAPH_URL is present so the
	// common env-var path "just works"; programmatic callers (tests,
	// embedders) must set both fields explicitly.
	Enabled  bool
	Username string // optional HTTP basic auth
	Password string
	// QueryTimeout is the per-request timeout for SPARQL queries/updates.
	// Default: 10s.
	QueryTimeoutSeconds int
	// Rebuild requests a full graph rebuild on startup. The Oxigraph projector
	// now runs as a checkpointed subscriber whose initial catch-up backfills the
	// graph, so a rebuild is "reset the oxigraph checkpoint to 0" — wired to this
	// flag in story #371.
	Rebuild bool
}

// Active reports whether the Oxigraph projection should run. Both URL and
// Enabled must be set. LoadFromEnvironment auto-sets Enabled when
// OXIGRAPH_URL is present; programmatic callers must set both explicitly.
func (o OxigraphConfig) Active() bool {
	return o.URL != "" && o.Enabled
}

// StorageConfig holds configuration for pluggable file storage backends.
// At most one cloud backend (GCS or S3) may be configured. If both are set,
// the application will log a warning and use GCS as primary.
type StorageConfig struct {
	// LocalPath is the local filesystem directory for uploads.
	// Default: "./uploads"
	LocalPath string

	// GCSBucket is the Google Cloud Storage bucket name.
	// When set, the GCS backend is activated as the primary storage.
	GCSBucket string

	// S3Bucket is the AWS S3 bucket name.
	// When set (and GCSBucket is empty), the S3 backend is activated as the primary storage.
	S3Bucket string

	// S3Region is the AWS region for the S3 bucket.
	// Default: "us-east-1"
	S3Region string

	// MaxUploadBytes is the maximum allowed upload size in bytes.
	// Default: 50 MB (52428800)
	MaxUploadBytes int64
}

// OAuthEnabled returns true when at least one OAuth provider is fully
// configured. The auth registry is gated on this so a binary without
// any provider creds doesn't expose half-wired login routes.
func (c *Config) OAuthEnabled() bool {
	if c.OAuth.GoogleClientID != "" && c.OAuth.GoogleClientSecret != "" {
		return true
	}
	if c.OAuth.NetSuiteClientID != "" && c.OAuth.NetSuiteClientSecret != "" && c.OAuth.NetSuiteAccountID != "" {
		return true
	}
	if c.OAuth.AppleConfigured() {
		return true
	}
	return false
}

// AppleConfigured reports whether all four Apple sign-in credentials are set.
// Apple requires every field (Services ID, team, key id, and the .p8 key) to
// sign the client-secret JWT, so a partial config is treated as "off".
func (c OAuthConfig) AppleConfigured() bool {
	return c.AppleClientID != "" && c.AppleTeamID != "" && c.AppleKeyID != "" && c.ApplePrivateKey != ""
}

// AuthEnabled returns true when any real authentication mechanism is
// configured (OAuth provider or password endpoints). Drives whether the
// API is mounted with RequireAuth or the dev-mode SoftAuth fallback —
// without this, a password-only deployment would mount login endpoints
// on top of routes that were still effectively unauthenticated.
func (c *Config) AuthEnabled() bool {
	return c.OAuthEnabled() || c.PasswordAuthEnabled
}

// DefaultOAuthProvider returns the provider name to use when the caller
// of /api/auth/login doesn't pass a `provider` query param (the admin
// SPA does this). Honors OAUTH_DEFAULT_PROVIDER when set, otherwise
// auto-picks from the configured registry — preferring google for
// backward-compat, then netsuite. Falls back to "google" so the
// behavior is unchanged when nothing is configured.
func (c *Config) DefaultOAuthProvider() string {
	if c.OAuth.DefaultProvider != "" {
		return c.OAuth.DefaultProvider
	}
	if c.OAuth.GoogleClientID != "" && c.OAuth.GoogleClientSecret != "" {
		return "google"
	}
	if c.OAuth.NetSuiteClientID != "" && c.OAuth.NetSuiteClientSecret != "" && c.OAuth.NetSuiteAccountID != "" {
		return "netsuite"
	}
	if c.OAuth.AppleConfigured() {
		return "apple"
	}
	return "google"
}

// LLMConfig holds configuration for LLM providers.
type LLMConfig struct {
	// GeminiAPIKey is the API key for Google Gemini.
	GeminiAPIKey string

	// GeminiModel is the Gemini model ID to use.
	// Default: "gemini-2.5-flash"
	GeminiModel string
}

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	// Port is the port the HTTP server listens on.
	// Default: 8080
	Port int

	// Host is the host address the HTTP server binds to.
	// Default: "0.0.0.0"
	Host string
}

// Validate checks that the configuration is valid.
// Returns an error if any required fields are missing or invalid.
func (c *Config) Validate() error {
	if c.DatabaseDSN == "" {
		return ErrMissingDatabaseDSN
	}

	if c.LogLevel != "" {
		validLevels := map[string]bool{
			"debug": true,
			"info":  true,
			"warn":  true,
			"error": true,
		}
		if !validLevels[c.LogLevel] {
			return ErrInvalidLogLevel
		}
	}

	return nil
}

// Default returns a Config with default values for local development.
func Default() Config {
	return Config{
		DatabaseDSN: "weos.db",
		LogLevel:    "info",
		Server: ServerConfig{
			Port: 8080,
			Host: "0.0.0.0",
		},
		SessionSecret: "change-me-in-production",
		LLM: LLMConfig{
			GeminiModel: "gemini-2.5-flash",
		},
		OAuth: OAuthConfig{
			DynamicRegistration: false,
		},
		Storage: StorageConfig{
			LocalPath:      "./uploads",
			S3Region:       "us-east-1",
			MaxUploadBytes: 50 << 20, // 50 MB
		},
		Worker: WorkerConfig{
			RunInProcess:    false,
			BatchSize:       100,
			PollInterval:    time.Second,
			MaxRetries:      5,
			RetryBackoff:    100 * time.Millisecond,
			MaxRetryBackoff: 5 * time.Second,
			LagLogInterval:  30 * time.Second,
		},
	}
}

// LoadFromEnvironment loads configuration values from environment variables.
// This should be called after creating a Config to populate values from the environment.
func (c *Config) LoadFromEnvironment() {
	if dsn := os.Getenv("DATABASE_DSN"); dsn != "" {
		c.DatabaseDSN = dsn
	}

	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		c.LogLevel = logLevel
	}

	if portStr := os.Getenv("SERVER_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
			c.Server.Port = port
		}
	}

	if host := os.Getenv("SERVER_HOST"); host != "" {
		c.Server.Host = host
	}

	if secret := os.Getenv("SESSION_SECRET"); secret != "" {
		c.SessionSecret = secret
	}

	if v := os.Getenv("PASSWORD_AUTH_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.PasswordAuthEnabled = enabled
		}
	}

	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		c.LLM.GeminiAPIKey = apiKey
	}

	if model := os.Getenv("GEMINI_MODEL"); model != "" {
		c.LLM.GeminiModel = model
	}

	if clientID := os.Getenv("GOOGLE_CLIENT_ID"); clientID != "" {
		c.OAuth.GoogleClientID = clientID
	}

	if clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET"); clientSecret != "" {
		c.OAuth.GoogleClientSecret = clientSecret
	}

	if clientID := os.Getenv("NETSUITE_CLIENT_ID"); clientID != "" {
		c.OAuth.NetSuiteClientID = clientID
	}

	if clientSecret := os.Getenv("NETSUITE_CLIENT_SECRET"); clientSecret != "" {
		c.OAuth.NetSuiteClientSecret = clientSecret
	}

	if accountID := os.Getenv("NETSUITE_ACCOUNT_ID"); accountID != "" {
		c.OAuth.NetSuiteAccountID = accountID
	}

	if scopes := os.Getenv("NETSUITE_SCOPES"); strings.TrimSpace(scopes) != "" {
		parsed := strings.FieldsFunc(scopes, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
		// Only override pericarp's default scope list if parsing actually
		// produced something — a whitespace-only override (common in
		// templated env files) would otherwise wipe the defaults.
		if len(parsed) > 0 {
			c.OAuth.NetSuiteScopes = parsed
		}
	}

	if clientID := os.Getenv("APPLE_CLIENT_ID"); clientID != "" {
		c.OAuth.AppleClientID = clientID
	}

	if teamID := os.Getenv("APPLE_TEAM_ID"); teamID != "" {
		c.OAuth.AppleTeamID = teamID
	}

	if keyID := os.Getenv("APPLE_KEY_ID"); keyID != "" {
		c.OAuth.AppleKeyID = keyID
	}

	if privateKey := os.Getenv("APPLE_PRIVATE_KEY"); privateKey != "" {
		// .p8 contents are multi-line PEM. When supplied through a single-line
		// env var (the usual case for CI/secret managers) the newlines arrive
		// escaped as the two characters "\n" — restore them so the ES256
		// signer can parse the PEM block.
		c.OAuth.ApplePrivateKey = strings.ReplaceAll(privateKey, "\\n", "\n")
	}

	if frontendURL := os.Getenv("FRONTEND_URL"); frontendURL != "" {
		c.OAuth.FrontendURL = frontendURL
	}

	if baseURL := os.Getenv("BASE_URL"); baseURL != "" {
		c.OAuth.BaseURL = baseURL
	}

	if jwtKey := os.Getenv("JWT_SIGNING_KEY"); jwtKey != "" {
		c.OAuth.JWTSigningKey = jwtKey
	}

	if dynReg := os.Getenv("OAUTH_DYNAMIC_REGISTRATION"); dynReg != "" {
		if enabled, err := strconv.ParseBool(dynReg); err == nil {
			c.OAuth.DynamicRegistration = enabled
		}
	}

	if provider := os.Getenv("OAUTH_DEFAULT_PROVIDER"); provider != "" {
		c.OAuth.DefaultProvider = provider
	}

	if smtpHost := os.Getenv("SMTP_HOST"); smtpHost != "" {
		c.SMTP.Host = smtpHost
	}
	if smtpPort := os.Getenv("SMTP_PORT"); smtpPort != "" {
		c.SMTP.Port = smtpPort
	}
	if smtpUser := os.Getenv("SMTP_USERNAME"); smtpUser != "" {
		c.SMTP.Username = smtpUser
	}
	if smtpPass := os.Getenv("SMTP_PASSWORD"); smtpPass != "" {
		c.SMTP.Password = smtpPass
	}
	if smtpFrom := os.Getenv("SMTP_FROM"); smtpFrom != "" {
		c.SMTP.From = smtpFrom
	}

	if v := os.Getenv("STORAGE_LOCAL_PATH"); v != "" {
		c.Storage.LocalPath = v
	}
	if v := os.Getenv("STORAGE_GCS_BUCKET"); v != "" {
		c.Storage.GCSBucket = v
	}
	if v := os.Getenv("STORAGE_S3_BUCKET"); v != "" {
		c.Storage.S3Bucket = v
	}
	if v := os.Getenv("STORAGE_S3_REGION"); v != "" {
		c.Storage.S3Region = v
	}
	if v := os.Getenv("STORAGE_MAX_UPLOAD_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			c.Storage.MaxUploadBytes = n
		}
	}

	if v := os.Getenv("OXIGRAPH_URL"); v != "" {
		c.Oxigraph.URL = v
		c.Oxigraph.Enabled = true
	}
	if v := os.Getenv("OXIGRAPH_STORE_PATH"); v != "" {
		c.Oxigraph.Path = v
	}
	if v := os.Getenv("OXIGRAPH_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Oxigraph.Enabled = b
		}
	}
	if v := os.Getenv("OXIGRAPH_USERNAME"); v != "" {
		c.Oxigraph.Username = v
	}
	if v := os.Getenv("OXIGRAPH_PASSWORD"); v != "" {
		c.Oxigraph.Password = v
	}
	if v := os.Getenv("OXIGRAPH_QUERY_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Oxigraph.QueryTimeoutSeconds = n
		}
	}
	if v := os.Getenv("OXIGRAPH_REBUILD"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Oxigraph.Rebuild = b
		}
	}

	c.loadWorkerFromEnvironment()
}

// loadWorkerFromEnvironment reads the WORKER_* tunables. It deliberately does
// NOT read WORKER_RUN_IN_PROCESS: whether this process runs the background
// subscribers is decided per-command (the serve command opts in via
// loadServeConfig), not globally here. Reading it here would let any
// short-lived CLI command that builds the Fx graph (resource, person, …)
// accidentally start workers when the var is set in a shared environment.
func (c *Config) loadWorkerFromEnvironment() {
	if v := os.Getenv("WORKER_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Worker.BatchSize = n
		}
	}
	if v := os.Getenv("WORKER_POLL_INTERVAL_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Worker.PollInterval = time.Duration(n) * time.Millisecond
		}
	}
	if v := os.Getenv("WORKER_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.Worker.MaxRetries = n
		}
	}
	if v := os.Getenv("WORKER_RETRY_BACKOFF_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Worker.RetryBackoff = time.Duration(n) * time.Millisecond
		}
	}
	if v := os.Getenv("WORKER_MAX_RETRY_BACKOFF_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Worker.MaxRetryBackoff = time.Duration(n) * time.Millisecond
		}
	}
	if v := os.Getenv("WORKER_LAG_LOG_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.Worker.LagLogInterval = time.Duration(n) * time.Second
		}
	}
}
