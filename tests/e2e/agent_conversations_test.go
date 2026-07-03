package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	gormdb "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"google.golang.org/adk/v2/model"
	adkdb "google.golang.org/adk/v2/session/database"
	"google.golang.org/genai"
)

// TestAgentConversations runs the in-app conversation acceptance scenarios
// (epic #397, stories #401/#402/#403/#404) end to end over the real HTTP
// handler, orchestrator, session service, and episode recording — with only
// the LLM replaced by a scripted model, so the suite stays deterministic and
// key-free. Routing quality (which skill a live model picks) is explicitly
// out of scope here; that needs a real model and stays with the manual gate.
func TestAgentConversations(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "agent-conversations",
		ScenarioInitializer: initAgentConversationScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/agent_conversations.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("agent conversation acceptance scenarios failed")
	}
}

// e2eScriptedModel satisfies model.LLM with a fixed reply.
type e2eScriptedModel struct{ reply string }

func (e2eScriptedModel) Name() string { return "scripted" }
func (m e2eScriptedModel) GenerateContent(
	context.Context, *model.LLMRequest, bool,
) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  genai.RoleModel,
				Parts: []*genai.Part{{Text: m.reply}},
			},
		}, nil)
	}
}

// agentConversationWorld boots the real application and drives the real
// agent HTTP handler through an in-process Echo instance.
type agentConversationWorld struct {
	app    *fx.App
	tmpDir string
	e      *echo.Echo

	rts application.ResourceTypeService
	rs  application.ResourceService

	scripted     string
	lastStatus   int
	lastBody     string
	lastEvents   []entities.AgentEvent
	lastWidgets  []map[string]any
	lastMessage  string
	lastHistory  []map[string]any
	bootDeferred bool
}

func initAgentConversationScenario(sc *godog.ScenarioContext) {
	w := &agentConversationWorld{}

	// The world is shared across scenarios; reset it so no state (booted
	// services, previous responses) leaks between them.
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*w = agentConversationWorld{}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS application$`, w.aCleanApplication)
	sc.Step(`^the in-app agent is configured with a scripted model that replies:$`, w.agentWithScriptedModel)
	sc.Step(`^the in-app agent has no model configured$`, w.agentWithoutModel)
	sc.Step(`^the "([^"]*)" preset is installed on the application$`, w.presetInstalled)
	sc.Step(`^the user sends "([^"]*)" in conversation "([^"]*)"$`, w.userSends)
	sc.Step(`^the user asks for the history of conversation "([^"]*)"$`, w.userAsksHistory)
	sc.Step(`^the user answers pending confirmation "([^"]*)" in conversation "([^"]*)" with no decision$`,
		w.userAnswersWithoutDecision)
	sc.Step(`^the stream emits "([^"]*)", "([^"]*)", and "([^"]*)" events in order$`, w.streamEmitsInOrder)
	sc.Step(`^the reply contains a markdown widget with text "([^"]*)"$`, w.replyHasMarkdown)
	sc.Step(`^the reply contains a "([^"]*)" widget titled "([^"]*)"$`, w.replyHasTypedWidget)
	sc.Step(`^the reply contains a markdown widget carrying the unrenderable payload$`, w.replyHasDegradedWidget)
	sc.Step(`^the history holds (\d+) messages alternating user and agent$`, w.historyAlternates)
	sc.Step(`^the first history message is a user message saying "([^"]*)"$`, w.historyStartsWith)
	sc.Step(`^an episodic note records the exchange about "([^"]*)"$`, w.episodicNoteRecords)
	sc.Step(`^the request is rejected as a bad request$`, w.rejectedBadRequest)
	sc.Step(`^the request is rejected as unavailable$`, w.rejectedUnavailable)
	sc.Step(`^the error explains the agent is not configured$`, w.errorSaysNotConfigured)
}

// --- Boot ---

// aCleanApplication defers the actual boot until the agent-configuration
// step, which decides whether a scripted model is decorated in.
func (w *agentConversationWorld) aCleanApplication() error {
	w.bootDeferred = true
	return nil
}

func (w *agentConversationWorld) agentWithScriptedModel(reply *godog.DocString) error {
	w.scripted = strings.TrimSpace(reply.Content)
	return w.boot(true)
}

func (w *agentConversationWorld) agentWithoutModel() error {
	return w.boot(false)
}

func (w *agentConversationWorld) boot(scripted bool) error {
	if !w.bootDeferred {
		return fmt.Errorf("boot ordering: 'a clean WeOS application' must run first")
	}
	cfg := config.Default()
	dir, err := os.MkdirTemp("", "weos-agent-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	cfg.DatabaseDSN = filepath.Join(dir, "test.db")
	cfg.LogLevel = "error"

	opts := []fx.Option{
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&w.rts, &w.rs),
	}
	var agent application.ConversationalAgent
	if scripted {
		// Everything is the real application; only the LLM is swapped. The
		// session service is the same database-backed one production wires
		// (see infrastructure/agents.ProvideOrchestrator), over the test DSN,
		// so these scenarios exercise the durable-session path. The module's
		// own fx.Invoke wiring (skill source, episode recorder) applies to
		// this decorated orchestrator.
		reply := w.scripted
		opts = append(opts, fx.Decorate(
			func(_ *appagents.Orchestrator, logger entities.Logger) (*appagents.Orchestrator, error) {
				sessions, err := adkdb.NewSessionService(gormdb.DialectorForDSN(cfg.DatabaseDSN))
				if err != nil {
					return nil, fmt.Errorf("create e2e session service: %w", err)
				}
				if err := adkdb.AutoMigrate(sessions); err != nil {
					return nil, fmt.Errorf("migrate e2e session tables: %w", err)
				}
				return appagents.NewOrchestrator(e2eScriptedModel{reply: reply}, sessions, logger), nil
			},
		))
	}
	opts = append(opts, fx.Populate(&agent))

	app := fx.New(opts...)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app

	// The same routes serve.go registers, minus auth (identity-less callers
	// land in the anonymous bucket, which is what these scenarios exercise).
	w.e = echo.New()
	h := handlers.NewAgentHandler(agent, noopE2ELogger{})
	w.e.POST("/api/agent/conversations/:conversationID/messages", h.SendMessage)
	w.e.POST("/api/agent/conversations/:conversationID/confirmations/:callID", h.Confirm)
	w.e.GET("/api/agent/conversations/:conversationID", h.History)
	return nil
}

func (w *agentConversationWorld) presetInstalled(ctx context.Context, name string) error {
	if w.rts == nil {
		return fmt.Errorf("application not booted — configure the agent first")
	}
	if _, err := w.rts.InstallPreset(ctx, name, true); err != nil {
		return fmt.Errorf("failed to install %q preset: %w", name, err)
	}
	return nil
}

// --- Actions ---

func (w *agentConversationWorld) do(method, path, body string) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	w.e.ServeHTTP(rec, req)
	w.lastStatus = rec.Code
	w.lastBody = rec.Body.String()
	w.parseEvents()
}

func (w *agentConversationWorld) parseEvents() {
	w.lastEvents = nil
	w.lastWidgets = nil
	for _, line := range strings.Split(w.lastBody, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var e entities.AgentEvent
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e) != nil {
			continue
		}
		w.lastEvents = append(w.lastEvents, e)
		if e.Type == entities.AgentEventWidgets && e.Widgets != nil {
			for _, widget := range e.Widgets.Widgets {
				raw, _ := json.Marshal(widget)
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				w.lastWidgets = append(w.lastWidgets, m)
			}
		}
	}
}

func (w *agentConversationWorld) userSends(message, conversation string) error {
	w.lastMessage = message
	body, _ := json.Marshal(map[string]string{"message": message})
	w.do(http.MethodPost, "/api/agent/conversations/"+conversation+"/messages", string(body))
	return nil
}

func (w *agentConversationWorld) userAsksHistory(conversation string) error {
	w.do(http.MethodGet, "/api/agent/conversations/"+conversation, "")
	w.lastHistory = nil
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(w.lastBody), &envelope); err != nil {
		return fmt.Errorf("history response is not an envelope: %s", w.lastBody)
	}
	w.lastHistory = envelope.Data
	return nil
}

func (w *agentConversationWorld) userAnswersWithoutDecision(callID, conversation string) error {
	w.do(http.MethodPost,
		"/api/agent/conversations/"+conversation+"/confirmations/"+callID, `{}`)
	return nil
}

// --- Outcomes ---

func (w *agentConversationWorld) streamEmitsInOrder(a, b, c string) error {
	want := []string{a, b, c}
	got := make([]string, 0, len(w.lastEvents))
	for _, e := range w.lastEvents {
		got = append(got, e.Type)
	}
	idx := 0
	for _, t := range got {
		if idx < len(want) && t == want[idx] {
			idx++
		}
	}
	if idx != len(want) {
		return fmt.Errorf("expected events %v in order, got %v", want, got)
	}
	return nil
}

func (w *agentConversationWorld) replyHasMarkdown(text string) error {
	for _, widget := range w.lastWidgets {
		if widget["type"] == "markdown" && strings.Contains(fmt.Sprint(widget["markdown"]), text) {
			return nil
		}
	}
	return fmt.Errorf("no markdown widget containing %q in %v (body: %s)", text, w.lastWidgets, w.lastBody)
}

func (w *agentConversationWorld) replyHasTypedWidget(widgetType, title string) error {
	for _, widget := range w.lastWidgets {
		if widget["type"] == widgetType && widget["title"] == title {
			return nil
		}
	}
	return fmt.Errorf("no %q widget titled %q in %v", widgetType, title, w.lastWidgets)
}

func (w *agentConversationWorld) replyHasDegradedWidget() error {
	for _, widget := range w.lastWidgets {
		if widget["type"] == "markdown" && strings.Contains(fmt.Sprint(widget["markdown"]), "hologram") {
			return nil
		}
	}
	return fmt.Errorf("expected the unknown widget degraded to markdown, got %v", w.lastWidgets)
}

func (w *agentConversationWorld) historyAlternates(count int) error {
	if len(w.lastHistory) != count {
		return fmt.Errorf("expected %d history messages, got %d: %v", count, len(w.lastHistory), w.lastHistory)
	}
	for i, m := range w.lastHistory {
		want := "user"
		if i%2 == 1 {
			want = "agent"
		}
		if m["role"] != want {
			return fmt.Errorf("message %d role = %v, want %s", i, m["role"], want)
		}
	}
	return nil
}

func (w *agentConversationWorld) historyStartsWith(text string) error {
	if len(w.lastHistory) == 0 {
		return fmt.Errorf("history is empty")
	}
	first := w.lastHistory[0]
	if first["role"] != "user" || !strings.Contains(fmt.Sprint(first["text"]), text) {
		return fmt.Errorf("first history message = %v, want user message %q", first, text)
	}
	return nil
}

func (w *agentConversationWorld) episodicNoteRecords(ctx context.Context, text string) error {
	page, err := w.rs.List(ctx, "note", "", 50, repositories.SortOptions{})
	if err != nil {
		return fmt.Errorf("list notes: %w", err)
	}
	for _, res := range page.Data {
		if strings.Contains(string(res.Data()), text) {
			return nil
		}
	}
	return fmt.Errorf("no episodic note mentions %q (%d notes)", text, len(page.Data))
}

func (w *agentConversationWorld) rejectedBadRequest() error {
	if w.lastStatus != http.StatusBadRequest {
		return fmt.Errorf("expected 400, got %d: %s", w.lastStatus, w.lastBody)
	}
	return nil
}

func (w *agentConversationWorld) rejectedUnavailable() error {
	if w.lastStatus != http.StatusServiceUnavailable {
		return fmt.Errorf("expected 503, got %d: %s", w.lastStatus, w.lastBody)
	}
	return nil
}

func (w *agentConversationWorld) errorSaysNotConfigured() error {
	if !strings.Contains(strings.ToLower(w.lastBody), "not configured") {
		return fmt.Errorf("expected a not-configured explanation, got: %s", w.lastBody)
	}
	return nil
}

func (w *agentConversationWorld) teardown() {
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(stopCtx)
	}
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
	}
}

// noopE2ELogger keeps handler logging quiet in scenarios.
type noopE2ELogger struct{}

func (noopE2ELogger) Debug(context.Context, string, ...any) {}
func (noopE2ELogger) Info(context.Context, string, ...any)  {}
func (noopE2ELogger) Warn(context.Context, string, ...any)  {}
func (noopE2ELogger) Error(context.Context, string, ...any) {}
