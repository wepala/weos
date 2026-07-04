package agents

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/widgets"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// testLogger satisfies entities.Logger as a no-op.
type testLogger struct{}

func (testLogger) Debug(context.Context, string, ...any) {}
func (testLogger) Info(context.Context, string, ...any)  {}
func (testLogger) Warn(context.Context, string, ...any)  {}
func (testLogger) Error(context.Context, string, ...any) {}

// scriptedLLM answers every request with a fixed text response.
type scriptedLLM struct{ text string }

func (scriptedLLM) Name() string { return "scripted" }
func (s scriptedLLM) GenerateContent(
	context.Context, *model.LLMRequest, bool,
) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  genai.RoleModel,
				Parts: []*genai.Part{{Text: s.text}},
			},
		}, nil)
	}
}

func TestOrchestrator_NotConfigured(t *testing.T) {
	o := NewOrchestrator(nil, nil, testLogger{})
	if o.Configured() {
		t.Fatal("orchestrator without a model must report not configured")
	}
	_, err := o.Converse(context.Background(), "c1", "u1", "hello")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got: %v", err)
	}
}

func TestOrchestrator_BuildRootIncludesSkillSubAgents(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "ok"}, session.InMemoryService(), testLogger{})
	o.SetSkillSource(func(context.Context) ([]entities.SkillDefinition, error) {
		return []entities.SkillDefinition{
			{SchemaVersion: 1, Name: "skill_a", Description: "a", Instructions: "i", Mode: entities.SkillModeTask},
			{SchemaVersion: 1, Name: "skill_b", Description: "b", Instructions: "i", Mode: entities.SkillModeSingleTurn},
		}, nil
	})

	root, err := o.buildRoot(context.Background())
	if err != nil {
		t.Fatalf("buildRoot: %v", err)
	}
	subs := root.SubAgents()
	if len(subs) != 2 {
		t.Fatalf("expected 2 sub-agents, got %d", len(subs))
	}
	names := map[string]bool{subs[0].Name(): true, subs[1].Name(): true}
	if !names["skill_a"] || !names["skill_b"] {
		t.Errorf("sub-agent names = %v", names)
	}
}

func TestOrchestrator_BuildRootSkipsBrokenSkill(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "ok"}, session.InMemoryService(), testLogger{})
	o.SetSkillSource(func(context.Context) ([]entities.SkillDefinition, error) {
		return []entities.SkillDefinition{
			{SchemaVersion: 1, Name: "good", Description: "d", Instructions: "i", Mode: entities.SkillModeTask},
			// Invalid mode slips past (defensive path): builder must skip it.
			{SchemaVersion: 1, Name: "broken", Description: "d", Instructions: "i", Mode: "nope"},
		}, nil
	})

	root, err := o.buildRoot(context.Background())
	if err != nil {
		t.Fatalf("buildRoot: %v", err)
	}
	if len(root.SubAgents()) != 1 || root.SubAgents()[0].Name() != "good" {
		t.Fatalf("expected only the good skill, got %v", root.SubAgents())
	}
}

func TestOrchestrator_DirectSkillInvocation(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "ok"}, session.InMemoryService(), testLogger{})
	o.SetSkillSource(func(context.Context) ([]entities.SkillDefinition, error) {
		return []entities.SkillDefinition{
			// ModeTask on purpose: the root build must override it (a root
			// task agent would get a parentless finish_task tool).
			{SchemaVersion: 1, Name: "statement_import", Description: "d", Instructions: "i",
				Mode: entities.SkillModeTask},
		}, nil
	})

	root, err := o.buildSkillRoot(context.Background(), "statement_import")
	if err != nil {
		t.Fatalf("buildSkillRoot: %v", err)
	}
	if root.Name() != "statement_import" {
		t.Errorf("root = %q, want the skill itself", root.Name())
	}
	if len(root.SubAgents()) != 0 {
		t.Errorf("a skill root must have no sub-agents, got %d", len(root.SubAgents()))
	}

	// The seam is additive: a full turn against the skill still emits the
	// standard event sequence.
	var types []string
	err = o.ConverseStream(context.Background(), "c1", "u1", "q", "statement_import",
		func(e entities.AgentEvent) { types = append(types, e.Type) })
	if err != nil {
		t.Fatalf("ConverseStream(skill): %v", err)
	}
	if len(types) == 0 || types[len(types)-1] != entities.AgentEventDone {
		t.Errorf("event types = %v, want a done-terminated turn", types)
	}
}

func TestOrchestrator_UnknownSkillErrors(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "ok"}, session.InMemoryService(), testLogger{})
	o.SetSkillSource(func(context.Context) ([]entities.SkillDefinition, error) {
		return []entities.SkillDefinition{
			{SchemaVersion: 1, Name: "other", Description: "d", Instructions: "i",
				Mode: entities.SkillModeTask},
		}, nil
	})

	err := o.ConverseStream(context.Background(), "c1", "u1", "q", "statement_import",
		func(entities.AgentEvent) {})
	if err == nil || !strings.Contains(err.Error(), `unknown skill "statement_import"`) {
		t.Fatalf("expected unknown-skill error, got: %v", err)
	}

	// No skills loaded at all must also be a clear error, not a nil deref.
	bare := NewOrchestrator(scriptedLLM{text: "ok"}, session.InMemoryService(), testLogger{})
	err = bare.ConverseStream(context.Background(), "c1", "u1", "q", "statement_import",
		func(entities.AgentEvent) {})
	if err == nil || !strings.Contains(err.Error(), "unknown skill") {
		t.Fatalf("expected unknown-skill error with no skills loaded, got: %v", err)
	}
}

func TestConfirmationEpisodeDetail_MissingCallStaysPlain(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "ok"}, session.InMemoryService(), testLogger{})

	// No such session/call: the episode gains only the chosen payload — a
	// session read failure must never break remembering the user's choice.
	detail := o.confirmationEpisodeDetail(context.Background(), "u1", "conv-x", "call-9",
		map[string]any{"statementId": "urn:statement:1"})
	if !strings.Contains(detail, `"statementId":"urn:statement:1"`) || strings.Contains(detail, "proposed:") {
		t.Errorf("detail = %q, want chosen-only", detail)
	}
}

func TestBoundedJSON_DropsWholeLinesNeverMidJSON(t *testing.T) {
	// An oversized interpretations payload trims whole lines and stays
	// valid JSON with an omittedLines count.
	lines := make([]any, 0, 100)
	for i := 0; i < 100; i++ {
		lines = append(lines, map[string]any{"description": strings.Repeat("m", 80), "treatment": "expense"})
	}
	out := boundedJSON(map[string]any{"statementId": "urn:statement:1", "interpretations": lines}, 2000)
	if len(out) > 2000 {
		t.Fatalf("blob exceeds the cap: %d bytes", len(out))
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("capped blob must stay valid JSON: %v — %s", err, out)
	}
	if parsed["omittedLines"] == nil {
		t.Error("a trimmed blob must say how many lines were dropped")
	}

	// A non-conforming oversized value degrades to a valid marker object.
	out = boundedJSON(map[string]any{"v": strings.Repeat("x", 5000)}, 100)
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("fallback blob must stay valid JSON: %v", err)
	}
}

func TestOrchestrator_ConverseMultiTurnSharesSession(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "answer"}, session.InMemoryService(), testLogger{})

	out, err := o.Converse(context.Background(), "conv1", "user1", "first message")
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	// The scripted model answers plain text, so the contract's fallback must
	// wrap it in a single markdown widget.
	if len(out.Widgets) != 1 || out.Widgets[0].Type != widgets.TypeMarkdown ||
		!strings.Contains(out.Widgets[0].Markdown, "answer") {
		t.Errorf("Converse output = %+v, want one markdown widget containing %q", out, "answer")
	}

	// Second turn reuses the existing session — ensureSession must not fail
	// on an already-created conversation.
	if _, err := o.Converse(context.Background(), "conv1", "user1", "second message"); err != nil {
		t.Fatalf("second turn: %v", err)
	}
}

func TestOrchestrator_RecordsEpisodePerTurn(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "reply text"}, session.InMemoryService(), testLogger{})
	var got []string
	o.SetEpisodeRecorder(func(_ context.Context, conversationID, userID, message, reply string) error {
		got = []string{conversationID, userID, message, reply}
		return nil
	})

	if _, err := o.Converse(context.Background(), "conv9", "u9", "the question"); err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if len(got) != 4 || got[0] != "conv9" || got[1] != "u9" || got[2] != "the question" ||
		!strings.Contains(got[3], "reply text") {
		t.Fatalf("recorded episode = %v", got)
	}
}

func TestOrchestrator_RecorderFailureDoesNotFailTurn(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "ok"}, session.InMemoryService(), testLogger{})
	o.SetEpisodeRecorder(func(context.Context, string, string, string, string) error {
		return errors.New("memory store down")
	})
	if _, err := o.Converse(context.Background(), "c", "u", "m"); err != nil {
		t.Fatalf("recording failure must not fail the turn: %v", err)
	}
}

func TestOrchestrator_ConverseStreamEmitsTextWidgetsDone(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "streamed"}, session.InMemoryService(), testLogger{})

	var types []string
	err := o.ConverseStream(context.Background(), "c1", "u1", "q", "", func(e entities.AgentEvent) {
		types = append(types, e.Type)
	})
	if err != nil {
		t.Fatalf("ConverseStream: %v", err)
	}
	want := []string{entities.AgentEventText, entities.AgentEventWidgets, entities.AgentEventDone}
	if len(types) != len(want) {
		t.Fatalf("event types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, types[i], want[i])
		}
	}
}

func TestOrchestrator_HistoryReturnsTurns(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "the answer"}, session.InMemoryService(), testLogger{})
	if _, err := o.Converse(context.Background(), "conv-h", "u1", "the question"); err != nil {
		t.Fatalf("Converse: %v", err)
	}

	history, err := o.History(context.Background(), "conv-h", "u1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) < 2 {
		t.Fatalf("expected at least user+agent messages, got %+v", history)
	}
	if history[0].Role != "user" || history[0].Text != "the question" {
		t.Errorf("first message = %+v", history[0])
	}
	last := history[len(history)-1]
	if last.Role != "agent" || last.Widgets == nil {
		t.Errorf("last message = %+v", last)
	}
}

func TestOrchestrator_HistoryUnknownConversationIsEmpty(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "x"}, session.InMemoryService(), testLogger{})
	history, err := o.History(context.Background(), "never-started", "u1")
	if err != nil || len(history) != 0 {
		t.Fatalf("expected empty history, got %v / %v", history, err)
	}
}

// failingSessions embeds session.Service so only Get needs a real
// implementation; History must not touch the other methods.
type failingSessions struct{ session.Service }

func (failingSessions) Get(context.Context, *session.GetRequest) (*session.GetResponse, error) {
	return nil, errors.New("connection refused")
}

func TestOrchestrator_ConverseSurfacesSessionStoreFailure(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "x"}, failingSessions{}, testLogger{})
	_, err := o.Converse(context.Background(), "conv", "u1", "hi")
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected session-store failure to surface, got: %v", err)
	}
	if strings.Contains(err.Error(), "create") {
		t.Errorf("a Get failure must not be masked by a create attempt: %v", err)
	}
}

func TestOrchestrator_HistorySurfacesStoreFailure(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "x"}, failingSessions{}, testLogger{})
	_, err := o.History(context.Background(), "conv", "u1")
	if err == nil {
		t.Fatal("expected session-store failure to surface, got nil error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should wrap the store failure, got: %v", err)
	}
}

func TestOrchestrator_SkillSourceErrorSurfaces(t *testing.T) {
	o := NewOrchestrator(scriptedLLM{text: "x"}, session.InMemoryService(), testLogger{})
	o.SetSkillSource(func(context.Context) ([]entities.SkillDefinition, error) {
		return nil, errors.New("registry down")
	})
	_, err := o.Converse(context.Background(), "c", "u", "m")
	if err == nil || !strings.Contains(err.Error(), "load skills") {
		t.Fatalf("expected load-skills error, got: %v", err)
	}
}
