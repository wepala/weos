package jsonld_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/pkg/jsonld"
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

func TestInlineVocabContext(t *testing.T) {
	// A bare-string @context becomes @vocab.
	in := json.RawMessage(`{"@context":"https://schema.org/","@graph":[{"@id":"urn:x:1","name":"A"}]}`)
	out := jsonld.InlineVocabContext(in)
	if !strings.Contains(string(out), `"@vocab":"https://schema.org/"`) {
		t.Errorf("remote string @context not inlined as @vocab: %s", out)
	}
	if strings.Contains(string(out), `"@context":"https://schema.org/"`) {
		t.Errorf("bare string @context still present: %s", out)
	}
	// An object @context, an absent one, and non-JSON are left unchanged.
	obj := json.RawMessage(`{"@context":{"@vocab":"https://schema.org/"},"@id":"urn:x:1"}`)
	if string(jsonld.InlineVocabContext(obj)) != string(obj) {
		t.Error("object @context should be unchanged")
	}
	none := json.RawMessage(`{"@id":"urn:x:1"}`)
	if string(jsonld.InlineVocabContext(none)) != string(none) {
		t.Error("document without @context should be unchanged")
	}
}

// TestBuildReverseMap_TermAliases: an edge written before its term was adopted
// is keyed by the IRI the property resolved to back then. Events are immutable
// and carry that key, so a reproject reproduces it — the alias is what keeps it
// readable after adoption (issue #513).
func TestBuildReverseMap_TermAliases(t *testing.T) {
	ctx := json.RawMessage(`{
		"@vocab":"https://schema.org/",
		"fo":"http://purl.org/foodontology#",
		"recipeIngredient":"fo:hasIngredient",
		"weos:termAliases":{"recipeIngredient":["https://schema.org/recipeIngredient"]}}`)

	reverse := jsonld.BuildReverseMap(ctx)
	for iri, want := range map[string]string{
		"http://purl.org/foodontology#hasIngredient": "recipeIngredient",
		"https://schema.org/recipeIngredient":        "recipeIngredient",
	} {
		if got := reverse[iri]; got != want {
			t.Errorf("reverse[%q] = %q, want %q", iri, got, want)
		}
	}
	// The alias key itself must not become a predicate.
	if _, forward := jsonld.ParseContext(ctx); forward[jsonld.TermAliasesKeyword] != "" {
		t.Errorf("%s leaked into the forward map as a term", jsonld.TermAliasesKeyword)
	}
}

// TestBuildReverseMap_AliasNeverShadowsALiveTerm: a stale alias must not
// capture an IRI another property currently claims, or reads for the live
// property would resolve to the wrong name.
func TestBuildReverseMap_AliasNeverShadowsALiveTerm(t *testing.T) {
	ctx := json.RawMessage(`{
		"@vocab":"https://schema.org/",
		"maker":"https://schema.org/manufacturer",
		"weos:termAliases":{"supplier":["https://schema.org/manufacturer"]}}`)

	if got := jsonld.BuildReverseMap(ctx)["https://schema.org/manufacturer"]; got != "maker" {
		t.Errorf("reverse IRI = %q, want maker — the live term must win over an alias", got)
	}
}
