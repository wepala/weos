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
	"sort"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/agent"
)

// testAgentContext adapts a plain context.Context to agent.ReadonlyContext so
// the toolset can be driven outside a running agent. The embedded ContextMock
// covers the agent-side methods; the context methods delegate to a real ctx
// because the MCP client uses them for cancellation.
type testAgentContext struct {
	agent.ContextMock
	ctx context.Context
}

func (t *testAgentContext) Deadline() (time.Time, bool) { return t.ctx.Deadline() }
func (t *testAgentContext) Done() <-chan struct{}       { return t.ctx.Done() }
func (t *testAgentContext) Err() error                  { return t.ctx.Err() }
func (t *testAgentContext) Value(key any) any           { return t.ctx.Value(key) }

// stubLexicalSearch satisfies application.LexicalSearch so memory_search
// registers in tests.
type stubLexicalSearch struct{}

func (stubLexicalSearch) Search(context.Context, string, int) ([]repositories.LexicalHit, string, error) {
	return nil, "", nil
}

// fullServer builds an MCP server with every tool group registered (33 tools).
func fullServer(t *testing.T) *gomcp.Server {
	t.Helper()
	server, err := NewMCPServer(
		&stubResourceTypeService{}, &stubResourceService{}, &stubKGService{active: true}, stubLexicalSearch{},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}
	return server
}

// TestNewAgentToolset_MCPParity pins the epic #397 parity guarantee: the tool
// list an in-app ADK agent sees is exactly the tool list an MCP client sees,
// because both come from the same server.
func TestNewAgentToolset_MCPParity(t *testing.T) {
	server := fullServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ts, err := NewAgentToolset(ctx, server, AgentToolsetConfig{})
	if err != nil {
		t.Fatalf("NewAgentToolset: %v", err)
	}

	adkTools, err := ts.Tools(&testAgentContext{ctx: ctx})
	if err != nil {
		t.Fatalf("list ADK tools: %v", err)
	}
	adkNames := make([]string, len(adkTools))
	for i, tl := range adkTools {
		adkNames[i] = tl.Name()
	}
	sort.Strings(adkNames)

	mcpNames := toolNames(t, server)

	if len(adkNames) != len(mcpNames) {
		t.Fatalf("ADK toolset has %d tools, MCP server has %d: %v vs %v",
			len(adkNames), len(mcpNames), adkNames, mcpNames)
	}
	for i := range mcpNames {
		if adkNames[i] != mcpNames[i] {
			t.Errorf("tool %d: ADK %q != MCP %q", i, adkNames[i], mcpNames[i])
		}
	}
}

func TestKnownToolsAndReadOnlyNames(t *testing.T) {
	server := fullServer(t)
	ctx := context.Background()

	known, err := KnownTools(server)(ctx)
	if err != nil {
		t.Fatalf("KnownTools: %v", err)
	}
	if len(known) != 33 {
		t.Fatalf("expected 33 known tools, got %d", len(known))
	}
	if !known["resource_create"] || !known["memory_recall"] {
		t.Errorf("known tools missing expected names: %v", known)
	}

	readOnly, err := ReadOnlyToolNames(ctx, server)
	if err != nil {
		t.Fatalf("ReadOnlyToolNames: %v", err)
	}
	roSet := make(map[string]bool, len(readOnly))
	for _, n := range readOnly {
		roSet[n] = true
	}
	if !roSet["memory_recall"] || !roSet["kg_search_entities"] {
		t.Errorf("read-only set missing expected names: %v", readOnly)
	}
	if roSet["resource_create"] || roSet["resource_delete"] {
		t.Errorf("mutating tools leaked into the read-only set: %v", readOnly)
	}
}

// TestNewMCPServer_ToolAnnotations is the authoritative read-only/mutating
// inventory. Every tool must carry explicit annotations; downstream consumers
// (HITL confirmation for in-app agents) key off ReadOnlyHint.
func TestNewMCPServer_ToolAnnotations(t *testing.T) {
	server := fullServer(t)
	tools := listTools(t, server)

	readOnly := map[string]bool{
		"resource_create":              false,
		"resource_get":                 true,
		"resource_list":                true,
		"resource_update":              false,
		"resource_delete":              false,
		"resource_type_create":         false,
		"resource_type_get":            true,
		"resource_type_list":           true,
		"resource_type_update":         false,
		"resource_type_delete":         false,
		"resource_type_preset_list":    true,
		"resource_type_preset_install": false,
		"resource_type_behavior_list":  true,
		"resource_type_behavior_set":   false,
		"person_create":                false,
		"person_get":                   true,
		"person_list":                  true,
		"person_update":                false,
		"person_delete":                false,
		"organization_create":          false,
		"organization_get":             true,
		"organization_list":            true,
		"organization_update":          false,
		"organization_delete":          false,
		"kg_sparql_query":              true,
		"kg_expand_entity":             true,
		"kg_search_entities":           true,
		"kg_describe_class":            true,
		"kg_list_classes":              true,
		"kg_find_path":                 true,
		"memory_recall":                true,
		"memory_search":                true,
		"playbook_record_outcome":      false,
	}

	seen := make(map[string]bool, len(tools))
	for _, tl := range tools {
		seen[tl.Name] = true
		if tl.Annotations == nil {
			t.Errorf("tool %q has no annotations", tl.Name)
			continue
		}
		want, ok := readOnly[tl.Name]
		if !ok {
			t.Errorf("tool %q is not in the annotation inventory — classify it read-only or mutating", tl.Name)
			continue
		}
		if tl.Annotations.ReadOnlyHint != want {
			t.Errorf("tool %q ReadOnlyHint = %v, want %v", tl.Name, tl.Annotations.ReadOnlyHint, want)
		}
		if !want && tl.Annotations.DestructiveHint == nil {
			t.Errorf("mutating tool %q must set DestructiveHint explicitly", tl.Name)
		}
	}
	for name := range readOnly {
		if !seen[name] {
			t.Errorf("expected tool %q was not registered", name)
		}
	}
}
