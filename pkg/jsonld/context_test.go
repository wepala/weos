package jsonld_test

import (
	"encoding/json"
	"testing"

	"weos/pkg/jsonld"
)

func TestSubClassOf(t *testing.T) {
	tests := []struct {
		name    string
		context json.RawMessage
		want    string
	}{
		{
			name:    "returns parent slug when rdfs:subClassOf is present",
			context: json.RawMessage(`{"@vocab":"https://valueflows.org/","@type":"Invoice","rdfs:subClassOf":"commitment"}`),
			want:    "commitment",
		},
		{
			name:    "returns empty string when rdfs:subClassOf is absent",
			context: json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Product"}`),
			want:    "",
		},
		{
			name:    "returns empty string for nil context",
			context: nil,
			want:    "",
		},
		{
			name:    "returns empty string for empty context",
			context: json.RawMessage(``),
			want:    "",
		},
		{
			name:    "returns empty string for invalid JSON",
			context: json.RawMessage(`{not valid`),
			want:    "",
		},
		{
			name:    "returns empty string when value is not a string",
			context: json.RawMessage(`{"rdfs:subClassOf":42}`),
			want:    "",
		},
		{
			name:    "returns empty string when value is an object",
			context: json.RawMessage(`{"rdfs:subClassOf":{"@id":"commitment"}}`),
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonld.SubClassOf(tt.context)
			if got != tt.want {
				t.Errorf("SubClassOf() = %q, want %q", got, tt.want)
			}
		})
	}
}
