package agents

import (
	"testing"

	"github.com/wepala/weos/v3/application"
)

func registryWithPreset(t *testing.T) application.PresetDefinition {
	t.Helper()
	registry := application.NewPresetRegistry()
	Register(registry)
	for _, def := range registry.List() {
		if def.Name == "agents" {
			return def
		}
	}
	t.Fatal("agents preset not registered")
	return application.PresetDefinition{}
}

func TestRegister_DefinesAgentSkillType(t *testing.T) {
	def := registryWithPreset(t)
	if len(def.Types) != 1 || def.Types[0].Slug != application.AgentSkillTypeSlug {
		t.Fatalf("expected one %q type, got %+v", application.AgentSkillTypeSlug, def.Types)
	}
	if len(def.Types[0].Fixtures) != 1 {
		t.Fatalf("expected the example skill fixture, got %d fixtures", len(def.Types[0].Fixtures))
	}
}

// TestExampleSkillFixture_LoadsAsValidSkill proves the declarative path end
// to end at the definition level: the shipped fixture parses and validates
// against the tools a full WeOS instance exposes.
func TestExampleSkillFixture_LoadsAsValidSkill(t *testing.T) {
	def := registryWithPreset(t)
	fixture := def.Types[0].Fixtures[0]

	skill, err := application.ParseSkillDefinition("urn:agent-skill:fixture", fixture)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	knownTools := map[string]bool{
		"memory_recall": true, "memory_search": true,
		"kg_search_entities": true, "kg_expand_entity": true, "kg_describe_class": true,
		"kg_list_classes": true, "kg_find_path": true, "kg_sparql_query": true,
	}
	if err := skill.Validate(knownTools); err != nil {
		t.Fatalf("fixture must be a valid v1 skill: %v", err)
	}
	if skill.Name != "knowledge_graph_researcher" {
		t.Errorf("fixture name = %q", skill.Name)
	}
}
