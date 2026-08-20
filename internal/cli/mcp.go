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
	"fmt"
	"strings"

	mcpserver "github.com/wepala/weos/v3/internal/mcp"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var mcpViper = viper.New()

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server",
	Long: fmt.Sprintf(`Start the WeOS MCP (Model Context Protocol) server for LLM-driven edits.

By default every tool group is registered except those that must be asked for by
name over this transport — currently "feature", which changes instance-wide state
and is left out because stdio callers are trusted without a permission check. Use --services to expose only a subset.

Available services: %s

Examples:
  weos mcp                                   # every group except the opt-in ones (default)
  weos mcp --services website,page           # only website and page tools
  weos mcp --services website --services page # same, repeated flag syntax
  MCP_SERVICES=organization weos mcp         # env var override`,
		strings.Join(mcpserver.ValidServiceNames(), ", ")),
	// This command is a long-running stdio server, not an interactive CLI. On a
	// runtime error, printing the full usage/help text would be noise on the
	// client's stderr and misleading, so suppress it and surface only the error.
	SilenceUsage: true,
	RunE:         runMCP,
}

func init() {
	mcpCmd.Flags().StringSlice("services", nil, "comma-separated list of tool groups to enable (default: all)")
	mcpViper.SetEnvPrefix("MCP")
	mcpViper.AutomaticEnv()
	_ = mcpViper.BindPFlag("services", mcpCmd.Flags().Lookup("services"))
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	services := mcpViper.GetStringSlice("services")
	if len(services) > 0 {
		if err := mcpserver.ValidateServiceNames(services); err != nil {
			return err
		}
	}
	return mcpserver.Run(services)
}
