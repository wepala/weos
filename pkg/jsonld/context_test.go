package jsonld_test

import (
	"encoding/json"
	"testing"

	"github.com/wepala/weos/pkg/jsonld"
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

func TestIsValueObject(t *testing.T) {
	tests := []struct {
		name    string
		context json.RawMessage
		want    bool
	}{
		{
			name:    "true when weos:valueObject is true",
			context: json.RawMessage(`{"@vocab":"https://w3id.org/valueflows#","weos:valueObject":true}`),
			want:    true,
		},
		{
			name:    "true when weos:valueObject is string true",
			context: json.RawMessage(`{"weos:valueObject":"true"}`),
			want:    true,
		},
		{
			name:    "false when weos:valueObject is false",
			context: json.RawMessage(`{"weos:valueObject":false}`),
			want:    false,
		},
		{
			name:    "false when weos:valueObject is absent",
			context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
			want:    false,
		},
		{
			name:    "false for nil context",
			context: nil,
			want:    false,
		},
		{
			name:    "false for empty context",
			context: json.RawMessage(``),
			want:    false,
		},
		{
			name:    "false for invalid JSON",
			context: json.RawMessage(`{not valid`),
			want:    false,
		},
		{
			name:    "false when value is a number",
			context: json.RawMessage(`{"weos:valueObject":1}`),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonld.IsValueObject(tt.context)
			if got != tt.want {
				t.Errorf("IsValueObject() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAbstract(t *testing.T) {
	tests := []struct {
		name    string
		context json.RawMessage
		want    bool
	}{
		{
			name:    "true when weos:abstract is true",
			context: json.RawMessage(`{"@vocab":"https://w3id.org/valueflows#","weos:abstract":true}`),
			want:    true,
		},
		{
			name:    "true when weos:abstract is string true",
			context: json.RawMessage(`{"weos:abstract":"true"}`),
			want:    true,
		},
		{
			name:    "false when weos:abstract is false",
			context: json.RawMessage(`{"weos:abstract":false}`),
			want:    false,
		},
		{
			name:    "false when weos:abstract is absent",
			context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
			want:    false,
		},
		{
			name:    "false for nil context",
			context: nil,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonld.IsAbstract(tt.context)
			if got != tt.want {
				t.Errorf("IsAbstract() = %v, want %v", got, tt.want)
			}
		})
	}
}
