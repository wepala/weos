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
		// Keywords and WeOS control entries are not term definitions. A
		// control entry whose value is a string — `rdfs:subClassOf` names a
		// type — used to be expanded exactly as if it were a term, so it
		// could claim a predicate IRI in the reverse map that a real
		// property owns, and which of the two won was map order (issue
		// #522). The readers of these entries (SubClassOf, IsAbstract,
		// IsValueObject, TermAliases, AdoptedTerms) read the raw context.
		if strings.HasPrefix(key, "@") || ControlKeywords[key] {
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
	// Historical IRIs resolve too, so edges written before a term was adopted
	// stay readable (issue #513). An alias NEVER shadows a live term: the
	// current mapping is authoritative, and only an IRI nothing else claims is
	// added.
	for propName, iris := range TermAliases(ldContext) {
		for _, iri := range iris {
			if _, taken := result[iri]; taken {
				continue
			}
			result[iri] = propName
		}
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
// Abstract types serve as base types for child types (via rdfs:subClassOf).
// Each type gets its own projection table; child resources are dual-projected
// into both their own table and all ancestor tables.
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

// InlineVocabContext rewrites a JSON-LD document whose top-level @context is a
// bare string — a remote context IRI such as "https://schema.org/" — into an
// inline {"@vocab": "<that IRI>"} context. A JSON-LD parser with no network
// access (the embedded oxigraph store) cannot fetch a remote context, but a
// bare-term document expands identically against @vocab, which is how WeOS
// builds resource-type ontologies. Documents whose @context is already an
// object (or absent, or unparseable) are returned unchanged, so this is a safe
// no-op for anything but the remote-string form.
func InlineVocabContext(data json.RawMessage) json.RawMessage {
	var doc map[string]json.RawMessage
	if json.Unmarshal(data, &doc) != nil {
		return data
	}
	raw, ok := doc["@context"]
	if !ok {
		return data
	}
	var iri string
	if json.Unmarshal(raw, &iri) != nil || iri == "" {
		return data // @context is not a bare string
	}
	inlined, err := json.Marshal(map[string]string{"@vocab": iri})
	if err != nil {
		return data
	}
	doc["@context"] = inlined
	out, err := json.Marshal(doc)
	if err != nil {
		return data
	}
	return out
}

// ControlKeywords are the `@context` entries WeOS reads as control data rather
// than as term definitions.
//
// They are excluded from a resource's stored context BY NAME, not by the shape
// of their value. `rdfs:subClassOf` names a type — a slug today, but
// `"schema:Thing"` or `"urn:type:agreement"` is how someone who knows RDF would
// naturally write it, and a value-shape test cannot tell that from a term
// mapping. Copied into a resource document, any of them makes the whole
// document unparseable and the graph store rejects it, so the resource never
// reaches the knowledge graph while its API read stays healthy.
var ControlKeywords = map[string]bool{
	"rdfs:subClassOf":   true,
	"weos:valueObject":  true,
	"weos:abstract":     true,
	TermAliasesKeyword:  true,
	AdoptedTermsKeyword: true,
}

// TermAliasesKeyword names the `@context` entry that records IRIs a property's
// edges were written under BEFORE its current term was adopted (issue #513).
//
// It lives inside the context so it travels with the resource type and survives
// a reproject. It is deliberately not an `@`-keyword: ParseContext ignores it
// anyway, because its value is an object with no `@id`, so it never becomes a
// predicate of its own.
const TermAliasesKeyword = "weos:termAliases"

// AdoptedTermsKeyword names the `@context` entry listing terms an operator has
// adopted. It exists because the alias itself cannot answer "was this adopted?"
// for a PREFIX: adopting one records aliases against the properties it moves,
// not against the prefix, so looking the term up in the alias map reports a
// re-run of the same command as a term that was never held.
const AdoptedTermsKeyword = "weos:adoptedTerms"

// AdoptedTerms returns the terms already adopted for a resource type.
func AdoptedTerms(ldContext json.RawMessage) []string {
	if len(ldContext) == 0 {
		return nil
	}
	var ctx map[string]any
	if json.Unmarshal(ldContext, &ctx) != nil {
		return nil
	}
	raw, ok := ctx[AdoptedTermsKeyword].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if term, ok := item.(string); ok && term != "" {
			out = append(out, term)
		}
	}
	return out
}

// TermAliases returns the recorded historical IRIs for each property, keyed by
// property name.
//
// These exist because events are immutable. An edge is stored keyed by the IRI
// its property resolved to at write time, and `ResourceCreated` carries that
// graph, so a reproject reproduces the original key no matter what is done to
// the stored data. Adopting a term that names a different IRI would therefore
// orphan every existing edge permanently. Recording the old IRI instead lets
// both resolve, which is what makes adoption safe and reversible.
func TermAliases(ldContext json.RawMessage) map[string][]string {
	if len(ldContext) == 0 {
		return nil
	}
	var ctx map[string]any
	if json.Unmarshal(ldContext, &ctx) != nil {
		return nil
	}
	raw, ok := ctx[TermAliasesKeyword].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string][]string, len(raw))
	for property, val := range raw {
		switch v := val.(type) {
		case string:
			if v != "" {
				out[property] = []string{v}
			}
		case []any:
			var iris []string
			for _, item := range v {
				if iri, ok := item.(string); ok && iri != "" {
					iris = append(iris, iri)
				}
			}
			if len(iris) > 0 {
				out[property] = iris
			}
		}
	}
	return out
}
