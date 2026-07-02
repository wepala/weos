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

	"github.com/wepala/weos/v3/pkg/widgets"
)

// ConversationalAgent is the provider-agnostic surface of the in-app agent.
// API handlers and CLI commands consume this interface; the ADK-backed
// implementation lives in application/agents and never leaks provider types
// through it.
type ConversationalAgent interface {
	// Configured reports whether an LLM is configured; when false, Converse
	// returns a clear not-configured error and the UI should say so.
	Configured() bool
	// Converse runs one turn and returns the response as the versioned
	// widget contract (pkg/widgets). conversationID identifies the durable
	// conversation (multi-turn context); ctx must carry the caller's
	// identity, which tool authorization reads, and must be request-scoped
	// (e.g. the HTTP request context) — tool sessions live exactly as long
	// as ctx.
	Converse(ctx context.Context, conversationID, userID, message string) (widgets.Response, error)
}
