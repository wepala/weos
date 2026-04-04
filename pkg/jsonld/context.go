package jsonld

import (
	"encoding/json"
	"strings"
)

// ExpandIRI expands a compact IRI (e.g., "schema:object") to a full IRI using prefixes
// defined in the context, or falls back to @vocab.
func ExpandIRI(compact, vocab string, ctx map[string]any) string {
	if strings.HasPrefix(compact, "http://") || strings.HasPrefix(compact, "https://") {
		return compact
	}
	if parts := strings.SplitN(compact, ":", 2); len(parts) == 2 {
		prefix := parts[0]
		suffix := parts[1]
		if ns, ok := ctx[prefix].(string); ok {
			return ns + suffix
		}
		// "schema:" prefix conventionally maps to Schema.org
		if prefix == "schema" && vocab != "" {
			return vocab + suffix
		}
	}
	if vocab != "" {
		return vocab + compact
	}
	return compact
}

// ParseContext extracts the @vocab and per-property predicate mappings from a JSON-LD context.
// Returns the vocab IRI and a map of property name → expanded predicate IRI.
func ParseContext(ldContext json.RawMessage) (string, map[string]string) {
	contextMap := make(map[string]string)
	if len(ldContext) == 0 {
		return "", contextMap
	}

	var ctx map[string]any
	if json.Unmarshal(ldContext, &ctx) != nil {
		return "", contextMap
	}

	vocab, _ := ctx["@vocab"].(string) //nolint:errcheck // type assertion defaults to ""

	for key, val := range ctx {
		if strings.HasPrefix(key, "@") {
			continue
		}
		switch v := val.(type) {
		case string:
			contextMap[key] = ExpandIRI(v, vocab, ctx)
		case map[string]any:
			if id, ok := v["@id"].(string); ok {
				contextMap[key] = ExpandIRI(id, vocab, ctx)
			}
		}
	}
	return vocab, contextMap
}

// BuildReverseMap builds a predicate IRI → property name map from a JSON-LD context.
// This is the inverse of ParseContext's property→IRI mapping.
func BuildReverseMap(ldContext json.RawMessage) map[string]string {
	_, forward := ParseContext(ldContext)
	result := make(map[string]string, len(forward))
	for propName, iri := range forward {
		result[iri] = propName
	}
	return result
}

// SubClassOf extracts the rdfs:subClassOf value from a JSON-LD context.
// Returns the parent type slug or empty string if not declared.
func SubClassOf(ldContext json.RawMessage) string {
	if len(ldContext) == 0 {
		return ""
	}
	var ctx map[string]any
	if json.Unmarshal(ldContext, &ctx) != nil {
		return ""
	}
	if v, ok := ctx["rdfs:subClassOf"].(string); ok {
		return v
	}
	return ""
}

// IsValueObject checks whether a JSON-LD context declares "weos:valueObject": true.
// Value object types are referenced by other types' properties but don't appear in navigation.
func IsValueObject(ldContext json.RawMessage) bool {
	if len(ldContext) == 0 {
		return false
	}
	var ctx map[string]any
	if json.Unmarshal(ldContext, &ctx) != nil {
		return false
	}
	v, ok := ctx["weos:valueObject"]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	default:
		return false
	}
}

// IsAbstract checks whether a JSON-LD context declares "weos:abstract": true.
// Abstract types define shared behaviors for child types (via rdfs:subClassOf)
// but have no projection table and cannot have instances created directly.
func IsAbstract(ldContext json.RawMessage) bool {
	if len(ldContext) == 0 {
		return false
	}
	var ctx map[string]any
	if json.Unmarshal(ldContext, &ctx) != nil {
		return false
	}
	v, ok := ctx["weos:abstract"]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	default:
		return false
	}
}

// ResolvePredicateIRI resolves the predicate IRI for a property name.
// Priority: explicit context mapping > @vocab + property name.
func ResolvePredicateIRI(propName, vocab string, contextMap map[string]string) string {
	if iri, ok := contextMap[propName]; ok {
		return iri
	}
	if vocab != "" {
		return vocab + propName
	}
	return propName
}
