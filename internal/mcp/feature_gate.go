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
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wepala/weos/v3/application"
)

// Gating an MCP tool on a feature (epic #480, story #484).
//
// Two gates, and they are not equally important. Filtering `tools/list` is a
// convenience for the model: it cannot propose what it cannot see, so it
// cannot hallucinate a call the instance will refuse, and the user is never
// offered a capability that does not exist for them. Refusing `tools/call` is
// the control. An implementation that filtered the listing and forgot the
// handler would have built a UI affordance and called it authorization, and
// the difference stays invisible right up until a client calls a tool by name
// from a list it cached an hour ago.
//
// So a stale tool list is never authorization. Whether the answer changed a
// second ago because an operator flipped a switch, or on its own because a
// grant's validity window closed with nothing to announce it, the call is
// resolved when it arrives.
//
// Deliberately NOT implemented here: a `tools/list_changed` notification. The
// protocol has one and the SDK can send it, and it cannot be THE mechanism.
// Grant expiry is lazy by design (#483) — a window that closes fires nothing,
// because a row that has run out is simply not counted at the next
// resolution — so for that case there is no event to hang a notification on,
// and there would not be one without a scheduler the epic does not want.
//
// Worth knowing before anyone revisits this: expiry is the ONLY unobservable
// case. An operator's flip, an account override and a grant being made or
// revoked are all writes that already invalidate caches, so a courtesy
// notification could hang on them, and it would cover the direction the
// refusal cannot help with — a capability being turned ON, where a client
// holding a stale list never re-lists and the model cannot call what it
// cannot see. Such a notification stays a courtesy: it must never become the
// thing anything relies on, because the expiry case would silently not have
// it.

// SDK method names. The Go SDK keeps its own copies unexported, so they are
// restated here rather than reached for.
const (
	methodListTools = "tools/list"
	methodCallTool  = "tools/call"
)

// FeatureGate reports whether the feature a tool names is on for the caller on
// ctx. It absorbs the three answers a call site cares about into the one it
// can act on:
//
//   - the feature is declared and resolves on or off — that answer;
//   - nobody declared the key — the tool stays where it was, because a typo in
//     one gate must not turn into a silent capability outage across an
//     instance, with a tool surface that looks deliberate;
//   - the stored state cannot be read — off, because a gate that opens on the
//     way to a database error hands out the capability at exactly the moment
//     nobody can see why.
//
// application.ToolFeatureGate builds the real one over the OpenFeature client.
// A nil gate means nothing is gated.
type FeatureGate func(ctx context.Context, featureKey string) bool

// FeatureGates indexes which feature gates each tool.
//
// This is an index of declarations, not a second place to declare. A tool's
// gate is written at its own mcp.AddTool call site, beside its name, schema
// and annotations, through AddGatedTool — a tool declared in one file and
// gated in a registry somewhere else is two things that will drift.
type FeatureGates struct {
	mu     sync.RWMutex
	byTool map[string]string
}

// NewFeatureGates builds an empty index.
func NewFeatureGates() *FeatureGates {
	return &FeatureGates{byTool: make(map[string]string)}
}

// Gate records that toolName is gated by featureKey. Prefer AddGatedTool,
// which records the gate and adds the tool in one statement so the two cannot
// be written apart.
func (g *FeatureGates) Gate(toolName, featureKey string) {
	if g == nil || toolName == "" || featureKey == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.byTool[toolName] = featureKey
}

// Of returns the feature gating toolName. The second result is false for an
// ungated tool, which is every tool that ships today bar one: nothing about
// an ungated tool changes — no lookup, no resolver, no new failure mode.
func (g *FeatureGates) Of(toolName string) (string, bool) {
	if g == nil {
		return "", false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	key, ok := g.byTool[toolName]
	return key, ok
}

func (g *FeatureGates) empty() bool {
	if g == nil {
		return true
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.byTool) == 0
}

// snapshot copies the index once, for a caller that is about to consult it for
// every tool on a page. A listing of forty tools should take one lock, not
// forty.
func (g *FeatureGates) snapshot() map[string]string {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]string, len(g.byTool))
	for tool, key := range g.byTool {
		out[tool] = key
	}
	return out
}

// AddGatedTool declares a tool and the feature that gates it in one statement.
// It is mcp.AddTool with the gate written beside the name and the annotations,
// which is the only place the two stay together.
func AddGatedTool[In, Out any](
	server *mcp.Server, gates *FeatureGates, featureKey string, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out],
) {
	gates.Gate(t.Name, featureKey)
	mcp.AddTool(server, t, h)
}

// inventoryKey marks a context as an internal inventory of the build's tools
// rather than a caller asking what it may use.
type inventoryKey struct{}

// WithToolInventory marks ctx as an internal enumeration of every registered
// tool, which the gate does not filter.
//
// This is not a way to call a gated tool: it is honored on the listing only,
// never on a call, so nothing reached through it can execute anything a caller
// could not. It exists because three consumers at boot want the build's tool
// inventory rather than one caller's permissions — the skill registry
// validating allowlists, the read-only classification behind human-in-the-loop
// confirmation, and the coordinator's default tool names. Resolving those
// against the anonymous caller would freeze a feature's boot-time value into
// lists that outlive it, so a tool turned on afterwards would stay missing
// until a restart. Each of those lists is later intersected with the caller's
// own gated toolset, so widening them cannot widen what anybody may run.
func WithToolInventory(ctx context.Context) context.Context {
	return context.WithValue(ctx, inventoryKey{}, true)
}

func isToolInventory(ctx context.Context) bool {
	v, _ := ctx.Value(inventoryKey{}).(bool)
	return v
}

// featureGateMiddleware filters the tool listing and refuses calls to tools
// whose feature is off for the caller.
func featureGateMiddleware(gates *FeatureGates, gate FeatureGate) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if gate == nil || gates.empty() {
				return next(ctx, method, req)
			}
			switch method {
			case methodListTools:
				return filterListing(ctx, gates, gate, next, method, req)
			case methodCallTool:
				if refusal := refuseGatedCall(ctx, gates, gate, req); refusal != nil {
					return refusal, nil
				}
			}
			return next(ctx, method, req)
		}
	}
}

// filterListing removes the tools the caller's features do not reach.
//
// Filtering happens after the SDK paginated, so a page can come back shorter
// than the page size. That is correct: the cursor walks the unfiltered list,
// so every page is still visited and the caller ends up with exactly the tools
// they may use.
func filterListing(
	ctx context.Context, gates *FeatureGates, gate FeatureGate,
	next mcp.MethodHandler, method string, req mcp.Request,
) (mcp.Result, error) {
	res, err := next(ctx, method, req)
	if err != nil || isToolInventory(ctx) {
		return res, err
	}
	listing, ok := res.(*mcp.ListToolsResult)
	if !ok || listing == nil {
		return res, err
	}
	gated := gates.snapshot()
	kept := make([]*mcp.Tool, 0, len(listing.Tools))
	for _, t := range listing.Tools {
		if key, ok := gated[t.Name]; ok && !gate(ctx, key) {
			continue
		}
		kept = append(kept, t)
	}
	// The result is freshly allocated per request, so replacing its slice is
	// safe. The tools themselves are the server's own pointers and are handed
	// on untouched — a gated tool that IS shown is advertised with exactly the
	// description, schema and annotations it declared.
	listing.Tools = kept
	return listing, nil
}

// refuseGatedCall returns the refusal for a call the caller's features do not
// reach, or nil to let the call through.
//
// The refusal is a tool result with IsError set rather than a protocol error,
// so the model sees it, can tell the user the capability is not enabled, and
// stops proposing the call. It carries the refusal and nothing else — the
// handler never ran, so there is no partial result to leak.
func refuseGatedCall(
	ctx context.Context, gates *FeatureGates, gate FeatureGate, req mcp.Request,
) *mcp.CallToolResult {
	call, ok := req.(*mcp.CallToolRequest)
	if !ok || call.Params == nil {
		return nil
	}
	key, gated := gates.Of(call.Params.Name)
	if !gated || gate(ctx, key) {
		return nil
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
			"%s is not available: the %q capability is not enabled %s.",
			call.Params.Name, key, refusalScope(ctx))}},
	}
}

// refusalScope says whose answer the refusal is, because the two cases send a
// reader somewhere different.
//
// With a caller, the answer came from their account and their grants, so "for
// you" is exact and an admin can change it for them. With no caller —  the
// local stdio transport, or an anonymous request — resolution stopped at the
// instance layer, and no account override or personal grant could have
// reached it. Saying "for you" there would send a mini-me user looking for a
// grant that cannot apply on the transport they are using.
func refusalScope(ctx context.Context) string {
	if application.HasCallerIdentity(ctx) {
		return "for you"
	}
	return "on this server"
}
