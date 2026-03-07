package cli

import (
	"weos/internal/config"
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
