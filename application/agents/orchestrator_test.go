package agents

import (
	"context"
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
