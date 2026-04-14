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
		// Predicate value is either a single {"@id": "..."} ref or an array
		// of such refs (multi-valued reference property). Each non-empty @id
		// becomes a triple.
		switch v := val.(type) {
		case map[string]any:
			if objectID, ok := v["@id"].(string); ok && objectID != "" {
				triples = append(triples, repositories.Triple{
					Subject:   subjectID,
					Predicate: key,
					Object:    objectID,
				})
			}
		case []any:
			for _, item := range v {
				ref, ok := item.(map[string]any)
				if !ok {
					continue
				}
				objectID, ok := ref["@id"].(string)
				if !ok || objectID == "" {
					continue
				}
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

// ExtractAndStripReferences extracts reference property values from data as triples
// and removes the reference keys from the data. Returns the stripped data and
// the extracted triples (with subject left empty — caller sets it).
//
// Prefer ExtractReferenceTriples when the caller doesn't need the stripped
// data — it avoids the unmarshal/marshal round-trip that this function
// performs solely to produce the stripped output.
func ExtractAndStripReferences(
	data json.RawMessage,
	refProps []ReferencePropertyDef,
) (json.RawMessage, []repositories.Triple, error) {
	if len(refProps) == 0 {
		return data, nil, nil
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, nil, fmt.Errorf("data must be a JSON object: %w", err)
	}

	refs := collectReferenceTriples(m, refProps)
	for _, rp := range refProps {
		delete(m, rp.PropertyName)
	}

	stripped, err := json.Marshal(m)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal stripped data: %w", err)
	}
	return stripped, refs, nil
}

// ExtractReferenceTriples decodes data and pulls out reference property values
// as triples without modifying or re-marshaling the data. Use this in callers
// that already pass the original data (refs intact) into BuildResourceGraph
// and only need the triples for event sourcing — the stripped output that
// ExtractAndStripReferences produces would just be discarded.
//
// Returned triples have empty Subject; the caller sets it.
func ExtractReferenceTriples(
	data json.RawMessage,
	refProps []ReferencePropertyDef,
) ([]repositories.Triple, error) {
	if len(refProps) == 0 {
		return nil, nil
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("data must be a JSON object: %w", err)
	}
	return collectReferenceTriples(m, refProps), nil
}

// collectReferenceTriples gathers triples from an already-decoded data map.
// Shared by ExtractAndStripReferences and ExtractReferenceTriples so the
// rules for which values count as references stay in one place.
func collectReferenceTriples(
	m map[string]any,
	refProps []ReferencePropertyDef,
) []repositories.Triple {
	var refs []repositories.Triple
	for _, rp := range refProps {
		switch v := m[rp.PropertyName].(type) {
		case string:
			if v != "" {
				refs = append(refs, repositories.Triple{Predicate: rp.PredicateIRI, Object: v})
			}
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					refs = append(refs, repositories.Triple{Predicate: rp.PredicateIRI, Object: s})
				}
			}
		}
	}
	return refs
}

// AddEdgeToGraph adds a relationship edge to a JSON-LD @graph document.
// Multi-valued predicates accumulate into an array. Re-adding an edge that
// already exists with the same (predicate, object) pair is a no-op — this
// lets the Triple.Created projection replay edges already materialized in
// Resource.Created's @graph body without duplicating them.
// If no edges node exists, one is created. If no @graph exists, the data
// is wrapped as an entity node with a new edges node.
// Returns an error if the edges node contains a value of an unexpected
// shape (not nil, not a map with @id, not a slice of such maps), since
// silently overwriting would destroy data that the caller cannot see.
func AddEdgeToGraph(
	data json.RawMessage, predicate, objectID, subjectID string,
) (json.RawMessage, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid JSON data: %w", err)
	}

	graphArr, hasGraph := doc["@graph"].([]any)
	if !hasGraph || len(graphArr) == 0 {
		// No @graph — wrap existing data as entity node + new edges node.
		entityNode := make(map[string]any)
		for k, v := range doc {
			entityNode[k] = v
		}
		edgesNode := map[string]any{
			"@id":     subjectID,
			predicate: map[string]any{"@id": objectID},
		}
		doc = map[string]any{"@graph": []any{entityNode, edgesNode}}
		if ctx, ok := entityNode["@context"]; ok {
			doc["@context"] = ctx
			delete(entityNode, "@context")
		}
		return json.Marshal(doc)
	}

	if len(graphArr) < 2 {
		// Only entity node — add edges node.
		edgesNode := map[string]any{
			"@id":     subjectID,
			predicate: map[string]any{"@id": objectID},
		}
		graphArr = append(graphArr, edgesNode)
	} else {
		// Edges node exists. Either the predicate is absent (add it), a single
		// ref (dedupe or promote to array), or an array (dedupe or append).
		// A non-map at @graph[1] is corruption — silently overwriting it would
		// destroy data the caller cannot see, so surface it as an error per
		// the documented contract.
		edgesNode, ok := graphArr[1].(map[string]any)
		if !ok {
			return nil, fmt.Errorf(
				"@graph[1] is %T, want edges map[string]any; refusing to overwrite",
				graphArr[1],
			)
		}
		newRef := map[string]any{"@id": objectID}
		switch existing := edgesNode[predicate].(type) {
		case nil:
			edgesNode[predicate] = newRef
		case map[string]any:
			id, ok := existing["@id"].(string)
			if !ok || id == "" {
				return nil, fmt.Errorf("edge at predicate %q has malformed @id; refusing to overwrite", predicate)
			}
			if id == objectID {
				break // already present — no-op
			}
			edgesNode[predicate] = []any{existing, newRef}
		case []any:
			found, err := containsEdgeRef(existing, objectID)
			if err != nil {
				return nil, fmt.Errorf("edge array at predicate %q: %w", predicate, err)
			}
			if found {
				break // already present — no-op
			}
			edgesNode[predicate] = append(existing, newRef)
		default:
			return nil, fmt.Errorf("edge at predicate %q has unexpected type %T; refusing to overwrite", predicate, existing)
		}
		graphArr[1] = edgesNode
	}

	doc["@graph"] = graphArr
	return json.Marshal(doc)
}

// containsEdgeRef reports whether any value in edgeList is a JSON-LD
// reference ({"@id": objectID}) matching objectID. Returns an error if
// any entry is not a well-formed ref map with a non-empty @id string,
// so AddEdgeToGraph can surface corruption rather than silently skip it.
func containsEdgeRef(edgeList []any, objectID string) (bool, error) {
	for i, v := range edgeList {
		m, ok := v.(map[string]any)
		if !ok {
			return false, fmt.Errorf("entry %d is %T, want {\"@id\": string}", i, v)
		}
		id, ok := m["@id"].(string)
		if !ok || id == "" {
			return false, fmt.Errorf("entry %d has malformed @id", i)
		}
		if id == objectID {
			return true, nil
		}
	}
	return false, nil
}

// RemoveEdgeFromGraph removes a specific relationship edge from a JSON-LD @graph document.
// For multi-valued predicates (arrays), only the matching objectID is removed.
// If the edges node becomes empty (only @id remains), it is removed from the @graph array.
func RemoveEdgeFromGraph(
	data json.RawMessage, predicate, objectID string,
) (json.RawMessage, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid JSON data: %w", err)
	}

	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		return data, nil // no edges node — nothing to remove
	}

	edgesNode, ok := graphArr[1].(map[string]any)
	if !ok {
		return data, nil
	}

	existing, exists := edgesNode[predicate]
	if !exists {
		return data, nil
	}

	// Handle array-valued predicates: remove only the matching objectID.
	// Preserve array shape even when the result shrinks to one entry —
	// otherwise an array-valued reference property would silently "flip"
	// to a scalar after deletions, and FlattenGraph / EdgeValues would
	// emit a different shape for the same property depending on history.
	if arr, ok := existing.([]any); ok {
		filtered := make([]any, 0, len(arr))
		for _, item := range arr {
			if ref, ok := item.(map[string]any); ok {
				if id, ok := ref["@id"].(string); ok && id == objectID {
					continue // remove this one
				}
			}
			filtered = append(filtered, item)
		}
		if len(filtered) == 0 {
			delete(edgesNode, predicate)
		} else {
			edgesNode[predicate] = filtered
		}
	} else {
		delete(edgesNode, predicate)
	}

	// If only @id remains, remove the edges node entirely.
	if len(edgesNode) <= 1 {
		graphArr = graphArr[:1]
	} else {
		graphArr[1] = edgesNode
	}

	doc["@graph"] = graphArr
	return json.Marshal(doc)
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

	// Merge edge values if edges node exists. A single ref unwraps to a
	// string; an array of refs unwraps to a []string so the flat output
	// stays symmetric with the input shape that BuildResourceGraph accepted.
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
				ids := collectEdgeIDs(val)
				switch len(ids) {
				case 0:
					// Skip — no usable @id values.
				case 1:
					if _, isArr := val.([]any); isArr {
						// Preserve array shape even when only one entry survives.
						flat[propName] = ids
					} else {
						flat[propName] = ids[0]
					}
				default:
					flat[propName] = ids
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
			// Reference property → edges node. Strings become a single
			// {"@id": value} ref; arrays of strings become an array of refs
			// (schemas can declare x-resource-type on array properties, e.g.
			// the mealplanning preset). Non-string / non-array-of-strings
			// values are skipped — they aren't valid references.
			predicateIRI := jsonld.ResolvePredicateIRI(key, vocab, contextMap)
			switch v := val.(type) {
			case string:
				if v != "" {
					edgesNode[predicateIRI] = map[string]any{"@id": v}
					hasEdges = true
				}
			case []any:
				refs := make([]any, 0, len(v))
				for _, item := range v {
					if s, ok := item.(string); ok && s != "" {
						refs = append(refs, map[string]any{"@id": s})
					}
				}
				if len(refs) > 0 {
					edgesNode[predicateIRI] = refs
					hasEdges = true
				}
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
//
// For multi-valued reference properties (where the edge is stored as an array
// of refs), this returns the first @id. Use EdgeValues to read every entry.
func EdgeValue(graphData, ldContext json.RawMessage, propertyName string) string {
	values := EdgeValues(graphData, ldContext, propertyName)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// EdgeValues reads every reference value for the given property from a
// JSON-LD @graph's edges node. Returns a single-element slice for the
// scalar-edge case and one entry per ref for the array-edge case (which
// BuildResourceGraph emits when the schema declares x-resource-type on an
// array property).
//
// Falls back to reading the property directly for legacy flat data.
// Returns an empty slice when the property is absent.
func EdgeValues(graphData, ldContext json.RawMessage, propertyName string) []string {
	var doc map[string]any
	if json.Unmarshal(graphData, &doc) != nil {
		return nil
	}

	// Check for @graph format.
	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		// Legacy flat format — read property directly. Support both a single
		// string and an array of strings for symmetry with the @graph case.
		switch v := doc[propertyName].(type) {
		case string:
			if v == "" {
				return nil
			}
			return []string{v}
		case []any:
			out := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			return out
		}
		return nil
	}

	edgesNode, ok := graphArr[1].(map[string]any)
	if !ok {
		return nil
	}

	// Resolve the property name to its predicate IRI using the resource type's context.
	vocab, contextMap := jsonld.ParseContext(ldContext)
	predicateIRI := jsonld.ResolvePredicateIRI(propertyName, vocab, contextMap)

	edgeVal, exists := edgesNode[predicateIRI]
	if !exists {
		return nil
	}
	return collectEdgeIDs(edgeVal)
}

// collectEdgeIDs unwraps an edge value (single ref, ref array, or bare
// string) into the list of @id strings it contains. Centralized so EdgeValue,
// EdgeValues, and FlattenGraph all interpret the edges node identically.
func collectEdgeIDs(edgeVal any) []string {
	switch v := edgeVal.(type) {
	case map[string]any:
		if id, ok := v["@id"].(string); ok && id != "" {
			return []string{id}
		}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			ref, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := ref["@id"].(string); ok && id != "" {
				out = append(out, id)
			}
		}
		return out
	case string:
		if v != "" {
			return []string{v}
		}
	}
	return nil
}
