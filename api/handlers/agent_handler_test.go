package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/widgets"

	"github.com/labstack/echo/v4"
)

type fakeAgent struct {
	configured bool
	history    []entities.AgentMessage
	gotMessage string
	gotCallID  string
	confirmed  bool
	gotPayload map[string]any
}

func (f *fakeAgent) Configured() bool { return f.configured }

func (f *fakeAgent) Converse(context.Context, string, string, string) (widgets.Response, error) {
	return widgets.Response{}, nil
}

func (f *fakeAgent) ConverseStream(
	_ context.Context, _, _, message string, emit appagents.EventSink,
) error {
	f.gotMessage = message
	emit(entities.AgentEvent{Type: entities.AgentEventText, Text: "hel"})
	emit(entities.AgentEvent{Type: entities.AgentEventText, Text: "lo"})
	resp := widgets.FromText("hello")
	emit(entities.AgentEvent{Type: entities.AgentEventWidgets, Widgets: &resp})
	emit(entities.AgentEvent{Type: entities.AgentEventDone})
	return nil
}

func (f *fakeAgent) ResumeConfirmation(
	_ context.Context, _, _, callID string, confirmed bool, payload map[string]any, emit appagents.EventSink,
) error {
	f.gotCallID = callID
	f.confirmed = confirmed
	f.gotPayload = payload
	emit(entities.AgentEvent{Type: entities.AgentEventDone})
	return nil
}

func (f *fakeAgent) History(context.Context, string, string) ([]entities.AgentMessage, error) {
	return f.history, nil
}

type nopLogger struct{}

func (nopLogger) Debug(context.Context, string, ...any) {}
func (nopLogger) Info(context.Context, string, ...any)  {}
func (nopLogger) Warn(context.Context, string, ...any)  {}
func (nopLogger) Error(context.Context, string, ...any) {}

func agentRequest(t *testing.T, h func(echo.Context) error, method, path, body string, params ...string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	for i := 0; i+1 < len(params); i += 2 {
		c.SetParamNames(params[i])
		c.SetParamValues(params[i+1])
	}
	if len(params) == 4 {
		c.SetParamNames(params[0], params[2])
		c.SetParamValues(params[1], params[3])
	}
	if err := h(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

func TestAgentHandler_SendMessageStreamsEvents(t *testing.T) {
	agent := &fakeAgent{configured: true}
	h := NewAgentHandler(agent, nopLogger{})

	rec := agentRequest(t, h.SendMessage, http.MethodPost,
		"/agent/conversations/c1/messages", `{"message":"hi"}`, "conversationID", "c1")

	if ct := rec.Header().Get(echo.HeaderContentType); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"event: text", "event: widgets", "event: done", `"hel"`, "hello"} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q:\n%s", want, body)
		}
	}
	if agent.gotMessage != "hi" {
		t.Errorf("message = %q", agent.gotMessage)
	}
}

func TestAgentHandler_SendMessageRequiresMessage(t *testing.T) {
	h := NewAgentHandler(&fakeAgent{configured: true}, nopLogger{})
	rec := agentRequest(t, h.SendMessage, http.MethodPost,
		"/agent/conversations/c1/messages", `{}`, "conversationID", "c1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestAgentHandler_NotConfigured(t *testing.T) {
	h := NewAgentHandler(&fakeAgent{configured: false}, nopLogger{})
	rec := agentRequest(t, h.SendMessage, http.MethodPost,
		"/agent/conversations/c1/messages", `{"message":"hi"}`, "conversationID", "c1")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestAgentHandler_ConfirmResumesTurn(t *testing.T) {
	agent := &fakeAgent{configured: true}
	h := NewAgentHandler(agent, nopLogger{})

	rec := agentRequest(t, h.Confirm, http.MethodPost,
		"/agent/conversations/c1/confirmations/call-9", `{"confirmed":true}`,
		"conversationID", "c1", "callID", "call-9")

	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Fatalf("stream missing done: %s", rec.Body.String())
	}
	if agent.gotCallID != "call-9" || !agent.confirmed {
		t.Errorf("resume args = %q %v", agent.gotCallID, agent.confirmed)
	}
}

func TestAgentHandler_ConfirmPassesEditedArgsPayload(t *testing.T) {
	agent := &fakeAgent{configured: true}
	h := NewAgentHandler(agent, nopLogger{})

	rec := agentRequest(t, h.Confirm, http.MethodPost,
		"/agent/conversations/c1/confirmations/call-9",
		`{"confirmed":true,"payload":{"interpretations":[{"line":1,"category":"groceries"}]}}`,
		"conversationID", "c1", "callID", "call-9")

	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Fatalf("stream missing done: %s", rec.Body.String())
	}
	if agent.gotPayload == nil {
		t.Fatal("payload was not passed through to ResumeConfirmation")
	}
	if _, ok := agent.gotPayload["interpretations"]; !ok {
		t.Errorf("payload = %v", agent.gotPayload)
	}
}

func TestAgentHandler_ConfirmWithoutPayloadPassesNil(t *testing.T) {
	agent := &fakeAgent{configured: true, gotPayload: map[string]any{"sentinel": true}}
	h := NewAgentHandler(agent, nopLogger{})

	agentRequest(t, h.Confirm, http.MethodPost,
		"/agent/conversations/c1/confirmations/call-9", `{"confirmed":true}`,
		"conversationID", "c1", "callID", "call-9")

	if agent.gotPayload != nil {
		t.Errorf("omitted payload must reach the agent as nil, got %v", agent.gotPayload)
	}
}

func TestAgentHandler_ConfirmRejectsNonObjectPayload(t *testing.T) {
	agent := &fakeAgent{configured: true}
	h := NewAgentHandler(agent, nopLogger{})

	rec := agentRequest(t, h.Confirm, http.MethodPost,
		"/agent/conversations/c1/confirmations/call-9", `{"confirmed":true,"payload":[1,2]}`,
		"conversationID", "c1", "callID", "call-9")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-object payload must be 400, got %d", rec.Code)
	}
	if agent.gotCallID != "" {
		t.Error("ResumeConfirmation must not run with a malformed payload")
	}
}

func TestAgentHandler_ConfirmRejectsNullPayload(t *testing.T) {
	agent := &fakeAgent{configured: true}
	h := NewAgentHandler(agent, nopLogger{})

	rec := agentRequest(t, h.Confirm, http.MethodPost,
		"/agent/conversations/c1/confirmations/call-9", `{"confirmed":true,"payload":null}`,
		"conversationID", "c1", "callID", "call-9")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("explicit null payload must be 400, not a silent fallback to model args; got %d", rec.Code)
	}
	if agent.gotCallID != "" {
		t.Error("ResumeConfirmation must not run with a null payload")
	}
}

func TestAgentHandler_ConfirmRequiresExplicitDecision(t *testing.T) {
	agent := &fakeAgent{configured: true}
	h := NewAgentHandler(agent, nopLogger{})

	rec := agentRequest(t, h.Confirm, http.MethodPost,
		"/agent/conversations/c1/confirmations/call-9", `{}`,
		"conversationID", "c1", "callID", "call-9")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("omitted confirmed must be 400, not a silent rejection; got %d", rec.Code)
	}
	if agent.gotCallID != "" {
		t.Error("ResumeConfirmation must not run without an explicit decision")
	}
}

func TestAgentHandler_History(t *testing.T) {
	resp := widgets.FromText("past answer")
	agent := &fakeAgent{configured: true, history: []entities.AgentMessage{
		{Role: "user", Text: "past question"},
		{Role: "agent", Widgets: &resp},
	}}
	h := NewAgentHandler(agent, nopLogger{})

	rec := agentRequest(t, h.History, http.MethodGet,
		"/agent/conversations/c1", "", "conversationID", "c1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "past question") || !strings.Contains(body, "past answer") {
		t.Errorf("history body = %s", body)
	}
}
