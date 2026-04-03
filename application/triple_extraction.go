package application

import (
	"encoding/json"
	"fmt"
	"strings"

	"weos/domain/repositories"
	"weos/pkg/jsonld"
)

// ReferencePropertyDef describes a JSON Schema property that references another resource.
type ReferencePropertyDef struct {
	PropertyName    string // e.g. "invoiceId"
	PredicateIRI    string // e.g. "https://schema.org/object"
	TargetType      string // e.g. "invoice"
	DisplayProperty string // e.g. "name" — property on the target resource shown in lists
}

// ExtractReferenceProperties parses a JSON Schema and JSON-LD context to find properties
// that reference other resources (marked with x-resource-type) and their predicate IRIs.
func ExtractReferenceProperties(
	schema json.RawMessage, ldContext json.RawMessage,
) []ReferencePropertyDef {
	if len(schema) == 0 {
		return nil
	}

	var s struct {
		Properties map[string]struct {
			XResourceType    string `json:"x-resource-type"`
			XDisplayProperty string `json:"x-display-property"`
		} `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return nil
	}

	// Parse @context to resolve predicate IRIs.
	vocab, contextMap := jsonld.ParseContext(ldContext)

	var defs []ReferencePropertyDef
	for propName, prop := range s.Properties {
		if prop.XResourceType == "" {
			continue
		}
		displayProp := prop.XDisplayProperty
		if displayProp == "" {
			displayProp = "name"
		}
		predicateIRI := jsonld.ResolvePredicateIRI(propName, vocab, contextMap)
		defs = append(defs, ReferencePropertyDef{
			PropertyName:    propName,
			PredicateIRI:    predicateIRI,
			TargetType:      prop.XResourceType,
			DisplayProperty: displayProp,
		})
	}
	return defs
}

// ExtractTriplesFromData produces concrete triples from resource data using reference property
// definitions. Supports both @graph format and legacy flat format.
// Each non-empty reference property value becomes a triple.
func ExtractTriplesFromData(
	refProps []ReferencePropertyDef,
	data json.RawMessage,
	subjectID string,
) []repositories.Triple {
	if len(refProps) == 0 || len(data) == 0 {
		return nil
	}

	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}

	// Check for @graph format — extract from edges node.
	if graphArr, ok := doc["@graph"].([]any); ok && len(graphArr) > 1 {
		return extractTriplesFromEdgesNode(refProps, graphArr[1], subjectID)
	}

	// Legacy flat format — extract from property names.
	var triples []repositories.Triple
	for _, rp := range refProps {
		val, ok := doc[rp.PropertyName].(string)
		if !ok || val == "" {
			continue
		}
		triples = append(triples, repositories.Triple{
			Subject:   subjectID,
			Predicate: rp.PredicateIRI,
			Object:    val,
		})
	}
	return triples
}

// extractTriplesFromEdgesNode extracts triples from a @graph edges node.
// The edges node uses predicate IRIs as keys with {"@id": "..."} values.
func extractTriplesFromEdgesNode(
	refProps []ReferencePropertyDef,
	edgesNodeRaw any,
	subjectID string,
) []repositories.Triple {
	edgesNode, ok := edgesNodeRaw.(map[string]any)
	if !ok {
		return nil
	}

	// Build predicate IRI lookup from reference property definitions.
	predicateSet := make(map[string]bool, len(refProps))
	for _, rp := range refProps {
		predicateSet[rp.PredicateIRI] = true
	}

	var triples []repositories.Triple
	for key, val := range edgesNode {
		if key == "@id" {
			continue
		}
		if !predicateSet[key] {
			continue
		}
		// Unwrap {"@id": "..."} format.
		if ref, ok := val.(map[string]any); ok {
			if objectID, ok := ref["@id"].(string); ok && objectID != "" {
				triples = append(triples, repositories.Triple{
					Subject:   subjectID,
					Predicate: key,
					Object:    objectID,
				})
			}
		}
	}
	return triples
}

// JSON-LD context parsing and IRI expansion utilities are in pkg/jsonld.

// buildStorableContext produces a minimal valid JSON-LD @context for storage.
// Strips all property-to-predicate mappings and non-standard entries (like @type overrides).
// Only keeps @vocab and namespace prefix definitions.
// The full context with property mappings lives in the resource type definition and is
// passed to reverse-mapping functions separately.
func buildStorableContext(ldContext json.RawMessage) any {
	var ctx map[string]any
	if json.Unmarshal(ldContext, &ctx) != nil {
		return nil
	}

	clean := make(map[string]any)
	for key, val := range ctx {
		// Keep only JSON-LD keywords (@vocab) and namespace prefix strings (e.g., "foaf": "http://...").
		if strings.HasPrefix(key, "@") && key != "@type" {
			clean[key] = val
			continue
		}
		// Keep namespace prefix definitions (string values that are URIs).
		if s, ok := val.(string); ok && (strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			clean[key] = val
		}
	}

	// If only @vocab remains, simplify to just the vocab string.
	if len(clean) == 1 {
		if v, ok := clean["@vocab"]; ok {
			return v
		}
	}

	if len(clean) == 0 {
		return nil
	}
	return clean
}

// FlattenGraph converts a @graph document back to flat JSON by merging the entity node
// and edges node into a single object. Edge values are converted from {"@id": "..."} to
// their original property names using the resource type's ldContext for reverse-mapping.
// Falls back to returning data as-is if not in @graph format.
func FlattenGraph(graphData, ldContext json.RawMessage) json.RawMessage {
	var doc map[string]any
	if json.Unmarshal(graphData, &doc) != nil {
		return graphData
	}

	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) == 0 {
		return graphData // already flat
	}

	// Start with entity node properties.
	entityNode, ok := graphArr[0].(map[string]any)
	if !ok {
		return graphData
	}
	flat := make(map[string]any)
	for k, v := range entityNode {
		if k == "@context" {
			continue // don't include @context in flat output
		}
		flat[k] = v
	}

	// Merge edge values if edges node exists.
	if len(graphArr) > 1 {
		if edgesNode, ok := graphArr[1].(map[string]any); ok {
			reverseMap := jsonld.BuildReverseMap(ldContext)
			for key, val := range edgesNode {
				if key == "@id" {
					continue
				}
				propName, ok := reverseMap[key]
				if !ok {
					continue
				}
				if ref, ok := val.(map[string]any); ok {
					if id, ok := ref["@id"].(string); ok {
						flat[propName] = id
					}
				}
			}
		}
	}

	// Remove JSON-LD meta keys that shouldn't be in flat format.
	delete(flat, "@id")
	delete(flat, "@type")

	result, err := json.Marshal(flat)
	if err != nil {
		return graphData
	}
	return result
}

// BuildResourceGraph takes flat input data and separates it into a JSON-LD @graph
// with an entity node (intrinsic properties) and an edges node (resource references).
// Reference properties are identified by x-resource-type markers in the schema.
// The edges node uses JSON-LD {"@id": "..."} format for object references.
func BuildResourceGraph(
	data json.RawMessage,
	refProps []ReferencePropertyDef,
	resourceID, typeName string,
	ldContext json.RawMessage,
) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("data must be a JSON object: %w", err)
	}

	// Build set of reference property names for fast lookup.
	refPropNames := make(map[string]bool, len(refProps))
	for _, rp := range refProps {
		refPropNames[rp.PropertyName] = true
	}

	// Parse context to resolve predicate IRIs for edges and extract @type.
	vocab, contextMap := jsonld.ParseContext(ldContext)

	// Use @type from context if available (schema type name), otherwise fall back to display name.
	schemaType := typeName
	var rawCtx map[string]any
	if json.Unmarshal(ldContext, &rawCtx) == nil {
		if ct, ok := rawCtx["@type"].(string); ok && ct != "" {
			schemaType = ct
		}
	}

	// Separate intrinsic from reference properties.
	entityNode := map[string]any{
		"@id":   resourceID,
		"@type": schemaType,
	}
	edgesNode := map[string]any{
		"@id": resourceID,
	}
	hasEdges := false

	for key, val := range m {
		if strings.HasPrefix(key, "@") {
			continue // skip JSON-LD keywords from input
		}
		if refPropNames[key] {
			// Reference property → edges node with {"@id": value} format.
			if strVal, ok := val.(string); ok && strVal != "" {
				predicateIRI := jsonld.ResolvePredicateIRI(key, vocab, contextMap)
				edgesNode[predicateIRI] = map[string]any{"@id": strVal}
				hasEdges = true
			}
		} else {
			// Intrinsic property → entity node.
			entityNode[key] = val
		}
	}

	// Build the @graph array.
	graph := []any{entityNode}
	if hasEdges {
		graph = append(graph, edgesNode)
	}

	// Build the top-level document with @context and @graph.
	// The @context only needs @vocab and @type — property-to-predicate mappings
	// are not needed since the edges node already uses full predicate IRIs.
	doc := map[string]any{
		"@graph": graph,
	}
	if len(ldContext) > 0 {
		cleanCtx := buildStorableContext(ldContext)
		if cleanCtx != nil {
			doc["@context"] = cleanCtx
		}
	}

	return json.Marshal(doc)
}

// ExtractEntityNode extracts the intrinsic entity node from a JSON-LD @graph document.
// Falls back to returning the input as-is if it has no @graph key (legacy flat format).
func ExtractEntityNode(graphData json.RawMessage) json.RawMessage {
	var doc map[string]any
	if json.Unmarshal(graphData, &doc) != nil {
		return graphData
	}

	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) == 0 {
		return graphData // legacy flat format
	}

	entityNode, ok := graphArr[0].(map[string]any)
	if !ok {
		return graphData
	}

	// Inject @context from the top-level document into the entity node for standalone use.
	if ctx, exists := doc["@context"]; exists {
		entityNode["@context"] = ctx
	}

	result, err := json.Marshal(entityNode)
	if err != nil {
		return graphData
	}
	return result
}

// ExtractEdgesNode extracts the edges node (relationship references) from a JSON-LD @graph.
// Returns nil if no edges node exists or if the data is in legacy flat format.
func ExtractEdgesNode(graphData json.RawMessage) json.RawMessage {
	var doc map[string]any
	if json.Unmarshal(graphData, &doc) != nil {
		return nil
	}

	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		return nil
	}

	edgesNode, ok := graphArr[1].(map[string]any)
	if !ok {
		return nil
	}

	result, err := json.Marshal(edgesNode)
	if err != nil {
		return nil
	}
	return result
}

// EdgeValue reads a specific reference property value from a JSON-LD @graph's edges node.
// The propertyName is the original schema property name (e.g., "courseId").
// It resolves the property to its predicate IRI using the document's @context,
// then looks up that predicate in the edges node and unwraps the {"@id": "..."} value.
// Falls back to reading from flat data for legacy format.
func EdgeValue(graphData, ldContext json.RawMessage, propertyName string) string {
	var doc map[string]any
	if json.Unmarshal(graphData, &doc) != nil {
		return ""
	}

	// Check for @graph format.
	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		// Legacy flat format — read property directly.
		if val, ok := doc[propertyName].(string); ok {
			return val
		}
		return ""
	}

	edgesNode, ok := graphArr[1].(map[string]any)
	if !ok {
		return ""
	}

	// Resolve the property name to its predicate IRI using the resource type's context.
	vocab, contextMap := jsonld.ParseContext(ldContext)
	predicateIRI := jsonld.ResolvePredicateIRI(propertyName, vocab, contextMap)

	// Look up the predicate in the edges node.
	edgeVal, exists := edgesNode[predicateIRI]
	if !exists {
		return ""
	}

	// Unwrap {"@id": "..."} format.
	if m, ok := edgeVal.(map[string]any); ok {
		if id, ok := m["@id"].(string); ok {
			return id
		}
	}
	// Direct string value fallback.
	if s, ok := edgeVal.(string); ok {
		return s
	}
	return ""
}
