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

// Package cli provides a public wrapper around the internal weos CLI for use
// by downstream services that embed the weos binary as a library.
//
// The primary CLI implementation lives in weos/internal/cli. Go's internal
// package rules prevent that package from being imported outside the weos
// module, so this thin re-export exists to give downstream binaries a stable
// public entry point.
package cli

import (
	internalcli "github.com/wepala/weos/v3/internal/cli"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"
	"go.uber.org/fx"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Execute runs the weos CLI root command.
//
// Downstream services embedding weos typically call this from main() after
// loading environment variables and calling presets.Register() for any
// custom presets they want to plug into the default registry.
func Execute() error {
	return internalcli.Execute()
}

// RegisterFxOptions appends fx options to be merged into the serve command's
// fx graph. Use this from a downstream binary's main() to plug in app-specific
// providers, invokes, or modules without forking serve.go. Must be called
// before Execute().
func RegisterFxOptions(opts ...fx.Option) {
	internalcli.RegisterFxOptions(opts...)
}

// RegisterEchoConfigurer registers a function that customizes the serve
// command's *echo.Echo after core and preset routes are wired and before the
// dynamic resource catch-all. Use this from a downstream binary's main() (or an
// init()) to add plain Echo routes without forking serve.go. Must be called
// before Execute().
func RegisterEchoConfigurer(c internalcli.EchoConfigurer) {
	internalcli.RegisterEchoConfigurer(c)
}

// MCPConfigurerDeps re-exports the dependency bundle handed to a custom
// MCP tool configurer (see RegisterMCPConfigurer).
type MCPConfigurerDeps = mcpserver.ConfigurerDeps

// RegisterMCPConfigurer registers a function that adds custom tools to
// the MCP server, on every transport (stdio and Streamable HTTP), after
// the built-in tool groups. Use this from a downstream binary's init()
// to expose a custom, non-CRUD tool — e.g. one that writes through a
// behavior pipeline rather than directly to projection tables. Must be
// called before Execute(). Mirrors RegisterEchoConfigurer for HTTP.
func RegisterMCPConfigurer(c func(server *mcp.Server, deps MCPConfigurerDeps)) {
	mcpserver.RegisterMCPConfigurer(c)
}

// MCPFeatureGates re-exports the server's feature-gate index, which a
// configurer receives as MCPConfigurerDeps.Gates.
type MCPFeatureGates = mcpserver.FeatureGates

// AddGatedTool adds a custom MCP tool and the feature that gates it, in one
// statement. It is the downstream equivalent of what a built-in tool does at
// its own call site: when the feature is off for the caller the tool is absent
// from their listing and a call to it is refused, and when it is on nothing
// about the tool differs from an ungated one.
//
// Re-exported because the gate index and the helper both live in an internal
// package, so an out-of-tree binary can receive deps.Gates but cannot name the
// type or call the helper. Without this the seam looks open and is not.
//
//	cli.RegisterMCPConfigurer(func(s *mcp.Server, deps cli.MCPConfigurerDeps) {
//	    cli.AddGatedTool(s, deps.Gates, "invoice-export", &mcp.Tool{…}, handler)
//	})
//
// The feature key must also be declared, or it is registry drift: the tool
// stays available and the instance logs it once. Declare it with
// application.AsFeatureDeclarations.
func AddGatedTool[In, Out any](
	server *mcp.Server, gates *MCPFeatureGates, featureKey string, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out],
) {
	mcpserver.AddGatedTool(server, gates, featureKey, t, h)
}
