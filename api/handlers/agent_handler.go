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
// The response is a server-sent event stream of entities.AgentEvent.
func (h *AgentHandler) SendMessage(c echo.Context) error {
	var body struct {
		Message string `json:"message"`
	}
	if err := c.Bind(&body); err != nil || body.Message == "" {
		return respondError(c, http.StatusBadRequest, "message is required")
	}
	return h.stream(c, func(emit appagents.EventSink) error {
		return h.agent.ConverseStream(
			c.Request().Context(), c.Param("conversationID"), userIDFrom(c), body.Message, emit,
		)
	})
}

// Confirm handles POST /agent/conversations/:conversationID/confirmations/:callID.
// It answers a pending mutating-tool confirmation and streams the rest of
// the turn.
func (h *AgentHandler) Confirm(c echo.Context) error {
	// Pointer bool: a body that omits "confirmed" must be a 400, not a
	// silent rejection of the pending call.
	var body struct {
		Confirmed *bool `json:"confirmed"`
	}
	if err := c.Bind(&body); err != nil || body.Confirmed == nil {
		return respondError(c, http.StatusBadRequest, "confirmed is required")
	}
	return h.stream(c, func(emit appagents.EventSink) error {
		return h.agent.ResumeConfirmation(
			c.Request().Context(), c.Param("conversationID"), userIDFrom(c),
			c.Param("callID"), *body.Confirmed, emit,
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

// stream runs a turn and writes its events as SSE. Errors before the first
// byte become normal JSON errors; errors mid-stream become an error event,
// because the status line is already gone.
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
