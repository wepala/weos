package agents

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"

	"google.golang.org/adk/v2/model"
)

// fakeLLM satisfies model.LLM without any network.
type fakeLLM struct{}

func (fakeLLM) Name() string { return "fake" }
func (fakeLLM) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(func(*model.LLMResponse, error) bool) {}
}

func skillDef() entities.SkillDefinition {
	return entities.SkillDefinition{
		ID:            "urn:agent-skill:test",
		SchemaVersion: entities.SkillSchemaVersion,
		Name:          "researcher",
		Description:   "Finds things in the graph",
		Instructions:  "Search before answering.",
		Tools:         []string{"kg_search_entities"},
		Mode:          entities.SkillModeTask,
	}
}

func TestBuildSkillSubAgent(t *testing.T) {
	a, err := BuildSkillSubAgent(skillDef(), fakeLLM{}, nil)
	if err != nil {
		t.Fatalf("BuildSkillSubAgent: %v", err)
	}
	if a.Name() != "researcher" {
		t.Errorf("agent name = %q, want researcher", a.Name())
	}
}

// A sub-agent is always a Chat transfer target — the declared task/single_turn
// mode is overridden, not applied — so a single_turn-declared skill still
// builds (as a Chat sub-agent) rather than the RunNode-dispatched node that
// would fail delegation at runtime.
func TestBuildSkillSubAgent_OverridesDeclaredMode(t *testing.T) {
	def := skillDef()
	def.Mode = entities.SkillModeSingleTurn
	if _, err := BuildSkillSubAgent(def, fakeLLM{}, nil); err != nil {
		t.Fatalf("BuildSkillSubAgent single_turn: %v", err)
	}
}

func TestSkillToolNames_IncludesMemoryDefaults(t *testing.T) {
	def := skillDef()
	def.Tools = []string{"kg_search_entities", "memory_recall"} // one overlap with the defaults
	names := skillToolNames(def)

	want := map[string]bool{
		"kg_search_entities": true, "memory_recall": true,
		"memory_search": true, "playbook_record_outcome": true,
	}
	if len(names) != len(want) {
		t.Fatalf("tool names = %v, want exactly %v (deduplicated)", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected tool %q", n)
		}
	}
}

func TestBuildSkillSubAgent_RejectsInvalidDefinition(t *testing.T) {
	def := skillDef()
	def.Instructions = ""
	_, err := BuildSkillSubAgent(def, fakeLLM{}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid skill definition") {
		t.Fatalf("expected invalid-definition error, got: %v", err)
	}
}
