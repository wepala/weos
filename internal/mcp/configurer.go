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

package mcp

import (
	"log/slog"

	"github.com/wepala/weos/v3/application"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP tool extension point. The built-in tool groups (person, resource,
// knowledge-graph, …) cover generic CRUD, but a downstream binary may
// need a custom, non-CRUD tool — e.g. a write-gated import that must run
// through a behavior pipeline rather than write resources directly. This
// mirrors RegisterEchoConfigurer for HTTP routes: a binary registers a
// configurer in init(), and it runs against the server on every
// transport (stdio and Streamable HTTP) after the built-in groups.

// ConfigurerDeps exposes the services a custom MCP tool needs. It is the
// subset of the MCP server's wiring that is safe and useful to hand to
// downstream tools. Logger may be nil (configurers should fall back to a
// default). Its destination depends on the transport: the stdio path
// supplies a stderr logger (so it can't corrupt the stdout protocol
// channel), while the HTTP path passes through the caller-provided
// logger, which may write elsewhere — a tool needing a specific
// destination should not assume one.
type ConfigurerDeps struct {
	ResourceService     application.ResourceService
	ResourceTypeService application.ResourceTypeService
	Logger              *slog.Logger
}

// MCPConfigurer registers additional tools on the server after the
// built-in groups are added.
type MCPConfigurer func(server *mcp.Server, deps ConfigurerDeps)

// customConfigurers holds the configurers registered by downstream
// binaries via RegisterMCPConfigurer.
var customConfigurers []MCPConfigurer

// RegisterMCPConfigurer registers a configurer to run against the MCP
// server on every transport. Call from an init() in the downstream
// binary, the same way custom presets and Echo configurers are wired.
// nil configurers are ignored.
func RegisterMCPConfigurer(c MCPConfigurer) {
	if c != nil {
		customConfigurers = append(customConfigurers, c)
	}
}

// applyConfigurers runs every registered configurer against the server.
// Called by each transport's constructor after the built-in groups are
// registered, so a custom tool is available identically over stdio and
// HTTP.
func applyConfigurers(server *mcp.Server, deps ConfigurerDeps) {
	for _, c := range customConfigurers {
		c(server, deps)
	}
}
