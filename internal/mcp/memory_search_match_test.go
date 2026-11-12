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
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// recordingSearch keeps the query memory_search passed it.
type recordingSearch struct{ got []repositories.LexicalQuery }

func (r *recordingSearch) Search(
	_ context.Context, q repositories.LexicalQuery,
) ([]repositories.LexicalHit, string, error) {
	r.got = append(r.got, q)
	return nil, application.LexicalModeFTS5, nil
}

func callMemorySearchWith(t *testing.T, search application.LexicalSearch, args map[string]any) *gomcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	server := gomcp.NewServer(&gomcp.Implementation{Name: "probe", Version: "0.1.0"}, nil)
	registerMemorySearchTool(server, search)
	cs := connectForGate(t, ctx, server)
	res, err := cs.CallTool(ctx, &gomcp.CallToolParams{Name: "memory_search", Arguments: args})
	if err != nil {
		t.Fatalf("call memory_search: %v", err)
	}
	return res
}

func TestMemorySearch_PassesTheMatchModeAndFloor(t *testing.T) {
	cases := []struct {
		args     map[string]any
		match    repositories.MatchMode
		minMatch int
	}{
		{map[string]any{"query": "Harbor invoice"}, repositories.MatchAll, 0},
		{map[string]any{"query": "Harbor invoice", "match": "all"}, repositories.MatchAll, 0},
		{map[string]any{"query": "Harbor invoice", "match": "any"}, repositories.MatchAny, 0},
		{map[string]any{"query": "Harbor invoice", "match": "Any", "minMatch": 2}, repositories.MatchAny, 2},
	}
	for _, c := range cases {
		search := &recordingSearch{}
		if res := callMemorySearchWith(t, search, c.args); res.IsError {
			t.Fatalf("%v: tool error %+v", c.args, res.Content)
		}
		got := search.got[0]
		if got.Text != "Harbor invoice" || got.Match != c.match || got.MinMatch != c.minMatch {
			t.Errorf("%v: query = %+v", c.args, got)
		}
	}
}

// An unknown mode is refused, never quietly read as the default.
func TestMemorySearch_RefusesAnUnknownMatchMode(t *testing.T) {
	search := &recordingSearch{}
	res := callMemorySearchWith(t, search, map[string]any{"query": "Harbor", "match": "some"})
	if !res.IsError {
		t.Fatalf("memory_search accepted match \"some\": %+v", res.Content)
	}
	if len(search.got) != 0 {
		t.Errorf("search ran with an unknown mode: %+v", search.got)
	}
}

// minMatch has a meaning only in the any mode. Set with the all mode — named
// or left as the default — it is refused, never quietly ignored.
func TestMemorySearch_RefusesMinMatchWithoutTheAnyMode(t *testing.T) {
	for _, args := range []map[string]any{
		{"query": "Harbor invoice", "minMatch": 2},
		{"query": "Harbor invoice", "match": "all", "minMatch": 2},
		{"query": "Harbor invoice", "match": " ALL ", "minMatch": -1},
	} {
		search := &recordingSearch{}
		res := callMemorySearchWith(t, search, args)
		if !res.IsError {
			t.Errorf("%v: memory_search accepted minMatch without match \"any\": %+v", args, res.Content)
		}
		if len(search.got) != 0 {
			t.Errorf("%v: search ran: %+v", args, search.got)
		}
	}
}
