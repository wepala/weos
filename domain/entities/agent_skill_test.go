package entities

import (
	"strings"
	"testing"
)

func validSkill() SkillDefinition {
	return SkillDefinition{
		ID:            "urn:agent-skill:test",
		SchemaVersion: SkillSchemaVersion,
		Name:          "kg_researcher",
		Description:   "Answers questions from the knowledge graph",
		Instructions:  "Search the graph before answering.",
		Tools:         []string{"kg_search_entities"},
		Mode:          SkillModeTask,
	}
}

func TestSkillDefinition_Validate(t *testing.T) {
	knownTools := map[string]bool{"kg_search_entities": true}

	tests := []struct {
		name    string
		mutate  func(*SkillDefinition)
		wantErr string
	}{
		{"valid", func(*SkillDefinition) {}, ""},
		{"future schema version", func(d *SkillDefinition) { d.SchemaVersion = 2 }, "unsupported schemaVersion"},
		{"empty name", func(d *SkillDefinition) { d.Name = "" }, "must match"},
		{"name with spaces", func(d *SkillDefinition) { d.Name = "kg researcher" }, "must match"},
		{"name starting with digit", func(d *SkillDefinition) { d.Name = "1skill" }, "must match"},
		{"missing description", func(d *SkillDefinition) { d.Description = "" }, "description is required"},
		{"missing instructions", func(d *SkillDefinition) { d.Instructions = "" }, "instructions are required"},
		{"bad mode", func(d *SkillDefinition) { d.Mode = "chat" }, "mode"},
		{"unknown tool", func(d *SkillDefinition) { d.Tools = []string{"nope"} }, `unknown tool "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := validSkill()
			tt.mutate(&d)
			err := d.Validate(knownTools)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestSkillDefinition_Validate_NilKnownToolsSkipsToolCheck(t *testing.T) {
	d := validSkill()
	d.Tools = []string{"not_registered_anywhere"}
	if err := d.Validate(nil); err != nil {
		t.Fatalf("nil knownTools must skip the allowlist check, got: %v", err)
	}
}
