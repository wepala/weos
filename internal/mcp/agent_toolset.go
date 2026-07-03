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
	// Approve-with-edits: a confirmation payload replaces the confirmed
	// call's arguments (stock mcptoolset would re-run the original ones).
	return EditedArgsToolset(ts), nil
}

// AgentToolsetFactory returns a factory that opens a fresh toolset session
// per call. The orchestrator calls it once per conversation turn with the
// caller's authenticated context, so tool authorization always sees the
// right identity.
func AgentToolsetFactory(server *mcp.Server, cfg AgentToolsetConfig) func(context.Context) (tool.Toolset, error) {
	return func(ctx context.Context) (tool.Toolset, error) {
		return NewAgentToolset(ctx, server, cfg)
	}
}

// serverTools lists the server's registered tools over a short-lived
// in-memory session.
func serverTools(ctx context.Context, server *mcp.Server) ([]*mcp.Tool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		return nil, fmt.Errorf("connect in-memory MCP server: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "weos-internal", Version: "0.1.0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect tool-listing client: %w", err)
	}
	defer func() { _ = cs.Close() }()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	return res.Tools, nil
}

// KnownTools returns a lister of the server's registered tool names — the
// skill registry uses it to reject skills whose allowlist references a tool
// that does not exist.
func KnownTools(server *mcp.Server) func(context.Context) (map[string]bool, error) {
	return func(ctx context.Context) (map[string]bool, error) {
		tools, err := serverTools(ctx, server)
		if err != nil {
			return nil, err
		}
		names := make(map[string]bool, len(tools))
		for _, t := range tools {
			names[t.Name] = true
		}
		return names, nil
	}
}

// MutatingConfirmationProvider returns a per-call decider that requires a
// human-in-the-loop confirmation for every tool NOT annotated read-only —
// the mutation flag declared alongside each tool (#399) is what gates
// writes behind user approval in the chat.
func MutatingConfirmationProvider(ctx context.Context, server *mcp.Server) (func(string, any) bool, error) {
	tools, err := serverTools(ctx, server)
	if err != nil {
		return nil, err
	}
	readOnly := make(map[string]bool, len(tools))
	for _, t := range tools {
		readOnly[t.Name] = t.Annotations != nil && t.Annotations.ReadOnlyHint
	}
	// Fail closed: anything not explicitly annotated read-only — including
	// unannotated downstream tools and names unknown to this snapshot —
	// requires confirmation.
	return func(toolName string, _ any) bool { return !readOnly[toolName] }, nil
}

// ReadOnlyToolNames returns the names of every registered tool annotated
// read-only — the coordinator's direct toolset for requests no skill
// matches.
func ReadOnlyToolNames(ctx context.Context, server *mcp.Server) ([]string, error) {
	tools, err := serverTools(ctx, server)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, t := range tools {
		if t.Annotations != nil && t.Annotations.ReadOnlyHint {
			names = append(names, t.Name)
		}
	}
	return names, nil
}
