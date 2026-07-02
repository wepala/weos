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

package entities

import "github.com/wepala/weos/v3/pkg/widgets"

// Agent stream event types, in the order a turn emits them: zero or more
// text deltas, optionally an input request (the turn then waits for a
// confirmation), and finally widgets followed by done.
const (
	AgentEventText           = "text"
	AgentEventWidgets        = "widgets"
	AgentEventInputRequested = "input_requested"
	AgentEventDone           = "done"
	AgentEventError          = "error"
)

// AgentEvent is one server-sent event of an in-app agent turn.
type AgentEvent struct {
	Type string `json:"type"`
	// Text is an incremental piece of the reply (AgentEventText).
	Text string `json:"text,omitempty"`
	// Widgets is the validated final response (AgentEventWidgets).
	Widgets *widgets.Response `json:"widgets,omitempty"`
	// CallID identifies a pending tool confirmation (AgentEventInputRequested);
	// approve or reject it via the confirmations endpoint to resume.
	CallID string `json:"callId,omitempty"`
	// Tool and Args describe the mutating call awaiting approval.
	Tool string         `json:"tool,omitempty"`
	Args map[string]any `json:"args,omitempty"`
	// Hint is the model's human-readable reason for the call.
	Hint string `json:"hint,omitempty"`
	// Error carries a failure once the stream has started (AgentEventError).
	Error string `json:"error,omitempty"`
}

// AgentMessage is one turn of a conversation's history.
type AgentMessage struct {
	Role    string            `json:"role"` // "user" or "agent"
	Text    string            `json:"text,omitempty"`
	Widgets *widgets.Response `json:"widgets,omitempty"`
}
