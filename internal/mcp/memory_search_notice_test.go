package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// laggingSearch answers from the full-text index and reports how far that
// index is behind the event history.
type laggingSearch struct{ lag int64 }

func (laggingSearch) Search(context.Context, string, int, ...string) ([]repositories.LexicalHit, string, error) {
	return []repositories.LexicalHit{{ID: "urn:task:1", TypeSlug: "task"}}, application.LexicalModeFTS5, nil
}

func (l laggingSearch) IndexLag(context.Context) (int64, error) { return l.lag, nil }

func callMemorySearch(t *testing.T, search application.LexicalSearch) map[string]any {
	t.Helper()
	ctx := context.Background()
	server := gomcp.NewServer(&gomcp.Implementation{Name: "probe", Version: "0.1.0"}, nil)
	registerMemorySearchTool(server, search)
	cs := connectForGate(t, ctx, server)
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{
		Name: "memory_search", Arguments: map[string]any{"query": "dunmore"},
	})
	if err != nil {
		t.Fatalf("call memory_search: %v", err)
	}
	if res.IsError {
		t.Fatalf("memory_search returned a tool error: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	return out
}

// While the index refills, the memory_search answer carries a notice next to
// its mode (wm-4db1v2), so a caller can tell a partial answer from a full one.
func TestMemorySearch_CarriesARebuildingNoticeBesideTheMode(t *testing.T) {
	out := callMemorySearch(t, laggingSearch{lag: 42})
	if out["mode"] != application.LexicalModeFTS5 {
		t.Errorf("mode = %v, want %q", out["mode"], application.LexicalModeFTS5)
	}
	notice, _ := out["notice"].(string)
	if !strings.Contains(notice, "rebuilding") || !strings.Contains(notice, "42 events behind") {
		t.Errorf("notice = %q, want the rebuilding notice with the lag", notice)
	}
}

// A caught-up index adds nothing to the answer: the key is omitted.
func TestMemorySearch_NoNoticeWhenTheIndexHasCaughtUp(t *testing.T) {
	out := callMemorySearch(t, laggingSearch{lag: 0})
	if _, present := out["notice"]; present {
		t.Errorf("notice present for a caught-up index: %v", out["notice"])
	}
}
