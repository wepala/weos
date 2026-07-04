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

package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/wepala/weos/v3/application"
	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"github.com/labstack/echo/v4"
)

// AgentHandler exposes the in-app agent: send a message and stream the
// turn's events, answer pending tool confirmations, and read a
// conversation's history.
type AgentHandler struct {
	agent  application.ConversationalAgent
	logger entities.Logger
}

// NewAgentHandler creates the handler.
func NewAgentHandler(agent application.ConversationalAgent, logger entities.Logger) *AgentHandler {
	return &AgentHandler{agent: agent, logger: logger}
}

// SendMessage handles POST /agent/conversations/:conversationID/messages.
// The response is a server-sent event stream of entities.AgentEvent. An
// optional ?skill=<name> converses with that one skill directly instead of
// the coordinator (direct invocation for deterministic client flows).
func (h *AgentHandler) SendMessage(c echo.Context) error {
	var body struct {
		Message string `json:"message"`
	}
	if err := c.Bind(&body); err != nil || body.Message == "" {
		return respondError(c, http.StatusBadRequest, "message is required")
	}
	return h.stream(c, func(emit appagents.EventSink) error {
		return h.agent.ConverseStream(
			c.Request().Context(), c.Param("conversationID"), userIDFrom(c),
			body.Message, c.QueryParam("skill"), emit,
		)
	})
}

// Confirm handles POST /agent/conversations/:conversationID/confirmations/:callID.
// It answers a pending mutating-tool confirmation and streams the rest of
// the turn. An approval may carry a payload object that replaces the tool
// call's arguments (approve-with-edits); a present payload must be a JSON
// object.
func (h *AgentHandler) Confirm(c echo.Context) error {
	// Pointer bool: a body that omits "confirmed" must be a 400, not a
	// silent rejection of the pending call. Payload binds as raw JSON so
	// an explicit null is rejected instead of being mistaken for an
	// omitted payload (which falls back to the model-proposed args).
	var body struct {
		Confirmed *bool           `json:"confirmed"`
		Payload   json.RawMessage `json:"payload"`
	}
	const confirmUsage = "confirmed (boolean) is required; payload, when present, must be a JSON object"
	if err := c.Bind(&body); err != nil || body.Confirmed == nil {
		return respondError(c, http.StatusBadRequest, confirmUsage)
	}
	var payload map[string]any
	if len(body.Payload) > 0 {
		if err := json.Unmarshal(body.Payload, &payload); err != nil || payload == nil {
			return respondError(c, http.StatusBadRequest, confirmUsage)
		}
	}
	return h.stream(c, func(emit appagents.EventSink) error {
		return h.agent.ResumeConfirmation(
			c.Request().Context(), c.Param("conversationID"), userIDFrom(c),
			c.Param("callID"), *body.Confirmed, payload, c.QueryParam("skill"), emit,
		)
	})
}

// History handles GET /agent/conversations/:conversationID.
func (h *AgentHandler) History(c echo.Context) error {
	if !h.agent.Configured() {
		return respondError(c, http.StatusServiceUnavailable, agentNotConfiguredMsg)
	}
	history, err := h.agent.History(c.Request().Context(), c.Param("conversationID"), userIDFrom(c))
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respond(c, http.StatusOK, history)
}

const agentNotConfiguredMsg = "the in-app agent is not configured: set the Gemini API key to enable it"

// stream runs a turn and writes its events as SSE. Only the not-configured
// check (and the callers' request validation) can produce a normal JSON
// error: the SSE status line and headers are written before the turn runs,
// so any failure after that surfaces as an "error" event on the stream.
func (h *AgentHandler) stream(c echo.Context, run func(emit appagents.EventSink) error) error {
	if !h.agent.Configured() {
		return respondError(c, http.StatusServiceUnavailable, agentNotConfiguredMsg)
	}

	res := c.Response()
	res.Header().Set(echo.HeaderContentType, "text/event-stream")
	res.Header().Set("Cache-Control", "no-cache")
	res.Header().Set("Connection", "keep-alive")
	res.WriteHeader(http.StatusOK)

	emit := func(e entities.AgentEvent) {
		data, err := json.Marshal(e)
		if err != nil {
			h.logger.Error(c.Request().Context(), "failed to marshal agent event", "error", err.Error())
			return
		}
		if _, werr := fmt.Fprintf(res, "event: %s\ndata: %s\n\n", e.Type, data); werr != nil {
			// The client went away; the request ctx cancels the turn.
			h.logger.Debug(c.Request().Context(), "agent stream write failed", "error", werr.Error())
			return
		}
		res.Flush()
	}

	if err := run(emit); err != nil {
		if errors.Is(err, appagents.ErrNotConfigured) {
			emit(entities.AgentEvent{Type: entities.AgentEventError, Error: agentNotConfiguredMsg})
			return nil
		}
		h.logger.Error(c.Request().Context(), "agent turn failed", "error", err.Error())
		emit(entities.AgentEvent{Type: entities.AgentEventError, Error: "the agent could not complete this turn"})
	}
	return nil
}

// userIDFrom resolves the conversation owner from the authenticated
// identity; anonymous (dev/soft-auth) sessions share one bucket.
func userIDFrom(c echo.Context) string {
	if identity := auth.AgentFromCtx(c.Request().Context()); identity != nil && identity.AgentID != "" {
		return identity.AgentID
	}
	return "anonymous"
}
