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

	mcpserver "github.com/wepala/weos/internal/mcp"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var mcpViper = viper.New()

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server",
	Long: fmt.Sprintf(`Start the WeOS MCP (Model Context Protocol) server for LLM-driven edits.

By default all tool groups are registered. Use --services to expose only a subset.

Available services: %s

Examples:
  weos mcp                                   # all services (default)
  weos mcp --services website,page           # only website and page tools
  weos mcp --services website --services page # same, repeated flag syntax
  MCP_SERVICES=organization weos mcp         # env var override`,
		strings.Join(mcpserver.ValidServiceNames(), ", ")),
	RunE: runMCP,
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
