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
	"github.com/spf13/cobra"
)

var (
	// rootCmd represents the base command when called without any subcommands
	rootCmd = &cobra.Command{
		Use:     "weos",
		Short:   "WeOS - open source digital twin for LLMs",
		Long:    `WeOS is an open source Go application for building a digital twin — a knowledge graph of the information from the apps and devices you use, exposed to any LLM via MCP for context-rich responses.`,
		Version: "0.1.0",
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(
		&databaseDSN,
		"database-dsn",
		"",
		"Database connection string (overrides DATABASE_DSN environment variable)",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&verbose,
		"verbose",
		"v",
		false,
		"Enable verbose logging",
	)
}

var (
	databaseDSN string
	verbose     bool
	cfg         *CLIConfig
)

func initConfig() {
	cfg = LoadCLIConfig()
	cfg.UpdateFromFlags(databaseDSN, verbose)
}

// GetConfig returns the global CLI configuration.
func GetConfig() *CLIConfig {
	if cfg == nil {
		cfg = LoadCLIConfig()
	}
	return cfg
}
