package presets_test

import (
	"encoding/json"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
)

func TestScratch_SilentPasses(t *testing.T) {
	cases := []struct {
		name string
		pt   application.PresetResourceType
	}{
		{"malformed context", application.PresetResourceType{
			Slug:    "food-item",
			Context: json.RawMessage(`{"@vocab":"https://schema.org/",`),
			Schema:  json.RawMessage(`{"type":"object","properties":{"spiciness":{"type":"string"},"status":{"type":"string"}}}`),
		}},
		{"malformed schema", application.PresetResourceType{
			Slug:    "food-item",
			Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
			Schema:  json.RawMessage(`{"type":"object","properties":{"spiciness":{`),
		}},
		{"no vocab", application.PresetResourceType{
			Slug:    "food-item",
			Context: json.RawMessage(`{"@type":"Thing"}`),
			Schema:  json.RawMessage(`{"type":"object","properties":{"spiciness":{"type":"string"},"status":{"type":"string"}}}`),
		}},
		{"schema prefix inside a house vocab type", application.PresetResourceType{
			Slug:    "food-item",
			Context: json.RawMessage(`{"@vocab":"https://weos.io/vocab/meal-planning#","status":"schema:status","spiciness":"schema:spiciness"}`),
			Schema:  json.RawMessage(`{"type":"object","properties":{"spiciness":{"type":"string"},"status":{"type":"string"}}}`),
		}},
		{"nested property under items", application.PresetResourceType{
			Slug:    "food-item",
			Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
			Schema:  json.RawMessage(`{"type":"object","properties":{"lines":{"type":"array","items":{"type":"object","properties":{"spiciness":{"type":"string"}}}}}}`),
		}},
	}
	for _, c := range cases {
		got := presets.PublishedVocabularyViolations([]application.PresetResourceType{c.pt})
		t.Logf("%-45s -> %d violations %v", c.name, len(got), got)
		t.Logf("%-45s    ContextGuardViolations: %v", c.name, presets.ContextGuardViolations([]application.PresetResourceType{c.pt}))
	}
}
