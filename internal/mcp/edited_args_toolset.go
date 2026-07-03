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
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

// functionTool is the method set the ADK flow drives a runnable tool
// through (structurally identical to ADK's internal FunctionTool — that
// interface lives in an internal package, so it is restated here).
type functionTool interface {
	tool.Tool
	Declaration() *genai.FunctionDeclaration
	Run(ctx agent.Context, args any) (map[string]any, error)
}

// requestProcessor mirrors ADK's internal RequestProcessor: tools implement
// it to pack their declarations into the outgoing LLM request.
type requestProcessor interface {
	ProcessRequest(ctx agent.Context, req *model.LLMRequest) error
}

// EditedArgsToolset wraps a toolset so that approving a pending confirmation
// with a payload substitutes that payload for the tool call's arguments
// (approve-with-edits): what executes is exactly what the user submitted,
// never the model's proposal. Stock ADK re-runs a confirmed call with its
// original arguments and ignores ToolConfirmation.Payload entirely.
func EditedArgsToolset(ts tool.Toolset) tool.Toolset {
	return &editedArgsToolset{inner: ts}
}

type editedArgsToolset struct {
	inner tool.Toolset
}

func (s *editedArgsToolset) Name() string { return s.inner.Name() }

func (s *editedArgsToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	tools, err := s.inner.Tools(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := make([]tool.Tool, len(tools))
	for i, t := range tools {
		// Only runnable, request-packing tools (every mcptoolset tool) can
		// carry a confirmation; anything else passes through untouched.
		if ft, ok := t.(interface {
			functionTool
			requestProcessor
		}); ok {
			wrapped[i] = &editedArgsTool{ft}
			continue
		}
		wrapped[i] = t
	}
	return wrapped, nil
}

// editedArgsTool delegates everything to the wrapped tool except Run, which
// substitutes user-edited arguments from an approved confirmation's payload.
type editedArgsTool struct {
	inner interface {
		functionTool
		requestProcessor
	}
}

func (t *editedArgsTool) Name() string                            { return t.inner.Name() }
func (t *editedArgsTool) Description() string                     { return t.inner.Description() }
func (t *editedArgsTool) IsLongRunning() bool                     { return t.inner.IsLongRunning() }
func (t *editedArgsTool) Declaration() *genai.FunctionDeclaration { return t.inner.Declaration() }

func (t *editedArgsTool) ProcessRequest(ctx agent.Context, req *model.LLMRequest) error {
	return t.inner.ProcessRequest(ctx, req)
}

func (t *editedArgsTool) Run(ctx agent.Context, args any) (map[string]any, error) {
	conf := ctx.ToolConfirmation()
	if conf == nil || !conf.Confirmed || conf.Payload == nil {
		// No confirmation in play (the wrapped tool decides whether to
		// request one), a rejection (the wrapped tool fails the call), or a
		// plain approval (original arguments run unchanged).
		return t.inner.Run(ctx, args)
	}
	// The payload has round-tripped through JSON (the confirmation event is
	// stored in the durable session), so an object arrives as
	// map[string]any. Anything else fails the call: the user asked for
	// edits, so silently running the model-proposed arguments instead would
	// execute something they did not approve.
	edited, ok := conf.Payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(
			"tool %q: confirmation payload must be a JSON object of tool arguments, got %T",
			t.Name(), conf.Payload,
		)
	}
	return t.inner.Run(ctx, edited)
}
