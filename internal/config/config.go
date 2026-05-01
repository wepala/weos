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

	FrontendURL         string
	BaseURL             string // Public URL for OAuth metadata/endpoints (e.g. https://example.com)
	JWTSigningKey       string // PEM-encoded RSA private key, or "auto" to generate ephemeral key
	DynamicRegistration bool   // Enable OAuth Dynamic Client Registration (RFC 7591)
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

	// BigQuery event store configuration.
	// When BigQueryProjectID is set, events are dual-written to both the primary store and BigQuery.
	BigQueryProjectID string
	BigQueryDatasetID string
	BigQueryTableID   string

	// Storage holds configuration for file storage backends.
	Storage StorageConfig
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
	return false
}

// AuthEnabled returns true when any real authentication mechanism is
// configured (OAuth provider or password endpoints). Drives whether the
// API is mounted with RequireAuth or the dev-mode SoftAuth fallback —
// without this, a password-only deployment would mount login endpoints
// on top of routes that were still effectively unauthenticated.
func (c *Config) AuthEnabled() bool {
	return c.OAuthEnabled() || c.PasswordAuthEnabled
}

// LLMConfig holds configuration for LLM providers.
type LLMConfig struct {
	// GeminiAPIKey is the API key for Google Gemini.
	GeminiAPIKey string

	// GeminiModel is the Gemini model ID to use.
	// Default: "gemini-2.0-flash"
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
			GeminiModel: "gemini-2.0-flash",
		},
		OAuth: OAuthConfig{
			DynamicRegistration: false,
		},
		Storage: StorageConfig{
			LocalPath:      "./uploads",
			S3Region:       "us-east-1",
			MaxUploadBytes: 50 << 20, // 50 MB
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

	if scopes := os.Getenv("NETSUITE_SCOPES"); scopes != "" {
		c.OAuth.NetSuiteScopes = strings.FieldsFunc(scopes, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
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

	if bqProject := os.Getenv("BIGQUERY_PROJECT_ID"); bqProject != "" {
		c.BigQueryProjectID = bqProject
	}
	if bqDataset := os.Getenv("BIGQUERY_DATASET_ID"); bqDataset != "" {
		c.BigQueryDatasetID = bqDataset
	}
	if bqTable := os.Getenv("BIGQUERY_TABLE_ID"); bqTable != "" {
		c.BigQueryTableID = bqTable
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
}
