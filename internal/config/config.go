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
)

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
}
