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

package cli

import (
	"github.com/wepala/weos/internal/config"
)

// CLIConfig wraps the application config with CLI-specific settings.
type CLIConfig struct {
	config.Config
	Verbose bool
}

// LoadCLIConfig creates a CLIConfig from environment and defaults.
func LoadCLIConfig() *CLIConfig {
	cfg := config.Default()
	cfg.LoadFromEnvironment()
	return &CLIConfig{
		Config: cfg,
	}
}

// UpdateFromFlags applies CLI flag overrides to the configuration.
func (c *CLIConfig) UpdateFromFlags(databaseDSN string, verbose bool) {
	if databaseDSN != "" {
		c.DatabaseDSN = databaseDSN
	}
	c.Verbose = verbose
	if verbose {
		c.LogLevel = "debug"
	}
}

// ProvideConfig converts the CLIConfig to an application Config.
func ProvideConfig() config.Config {
	c := LoadCLIConfig()
	return c.Config
}
