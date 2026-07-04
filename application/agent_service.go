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

package application

import (
	"context"

	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/widgets"
)

// ConversationalAgent is the provider-agnostic surface of the in-app agent.
// API handlers and CLI commands consume this interface; the ADK-backed
// implementation lives in application/agents and never leaks provider types
// through it.
//
// For every method taking a ctx: it must carry the caller's identity (tool
// authorization reads it) and must be request-scoped (e.g. the HTTP request
// context) — tool sessions live exactly as long as ctx.
type ConversationalAgent interface {
	// Configured reports whether an LLM is configured; when false, the other
	// methods return a clear not-configured error and the UI should say so.
	Configured() bool
	// Converse runs one turn synchronously and returns the response as the
	// versioned widget contract (pkg/widgets).
	Converse(ctx context.Context, conversationID, userID, message string) (widgets.Response, error)
	// ConverseStream runs one turn, emitting text deltas, any pending tool
	// confirmation (input_requested), then widgets and done. A non-empty
	// skill converses with that one skill directly — built as the turn's
	// root agent, bypassing coordinator routing — so a client's flow stays
	// deterministic; empty keeps the coordinator.
	ConverseStream(ctx context.Context, conversationID, userID, message, skill string, emit appagents.EventSink) error
	// ResumeConfirmation answers a pending mutating-tool confirmation and
	// streams the rest of the turn. Pending confirmations live in the
	// durable session, so they survive refreshes and restarts. A non-nil
	// payload on an approval replaces the tool call's arguments wholesale
	// (approve-with-edits): what executes is exactly what the user
	// submitted. A nil payload runs the original arguments; a payload on a
	// rejection is ignored. skill must repeat the value the paused turn ran
	// with (the resume rebuilds the same root agent).
	ResumeConfirmation(
		ctx context.Context, conversationID, userID, callID string, confirmed bool,
		payload map[string]any, skill string, emit appagents.EventSink,
	) error
	// History returns the conversation so far, oldest first.
	History(ctx context.Context, conversationID, userID string) ([]entities.AgentMessage, error)
}
