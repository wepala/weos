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
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

// AgentToolsetConfig tunes how the server's tools are exposed to in-app ADK
// agents.
type AgentToolsetConfig struct {
	// RequireConfirmation forces a human-in-the-loop confirmation before
	// every tool call from this toolset executes.
	RequireConfirmation bool
	// RequireConfirmationProvider decides per call whether confirmation is
	// needed; it takes precedence over RequireConfirmation. Signature:
	// func(toolName string, toolInput any) bool.
	RequireConfirmationProvider tool.ConfirmationProvider
}

// NewAgentToolset exposes an already-configured MCP server's tools to in-app
// ADK agents over an in-memory transport. mcp.AddTool stays the single place
// a tool is declared: agents consume the very server third-party AI clients
// connect to, so tool names, schemas, and behavior cannot drift between the
// two surfaces — including tools downstream binaries add via
// RegisterMCPConfigurer, as long as the server is passed here after
// applyConfigurers ran.
//
// ctx scopes the in-memory session and is the context the server handles
// calls under. Service-layer authorization reads the caller identity from
// the handler context, so pass a context carrying the agent session's
// identity and create one toolset per authenticated session. Cancel ctx to
// disconnect the session.
func NewAgentToolset(ctx context.Context, server *mcp.Server, cfg AgentToolsetConfig) (tool.Toolset, error) {
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		return nil, fmt.Errorf("connect in-memory MCP server: %w", err)
	}
	ts, err := mcptoolset.New(mcptoolset.Config{
		Transport:                   clientTransport,
		RequireConfirmation:         cfg.RequireConfirmation,
		RequireConfirmationProvider: cfg.RequireConfirmationProvider,
	})
	if err != nil {
		return nil, fmt.Errorf("wrap MCP server as agent toolset: %w", err)
	}
	return ts, nil
}
