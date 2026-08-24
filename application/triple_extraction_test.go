package application

import (
	"encoding/json"
	"testing"
)

// enrollmentContext mirrors the enrollment type's JSON-LD @context from the
// education preset. Used here to verify the BuildResourceGraph → EdgeValue
// round-trip with realistic IRI resolution.
var enrollmentContext = json.RawMessage(`{
	"@vocab": "https://schema.org/",
	"@type": "Action",
	"additionalType": "RegisterAction",
	"studentId": {"@id": "schema:participant", "@type": "@id"},
	"courseInstanceId": {"@id": "schema:object", "@type": "@id"},
	"guardianId": {"@id": "schema:agent", "@type": "@id"}
}`)

var enrollmentRefProps = []ReferencePropertyDef{
	{PropertyName: "studentId", PredicateIRI: "https://schema.org/participant", TargetType: "student"},
	{PropertyName: "courseInstanceId", PredicateIRI: "https://schema.org/object", TargetType: "course-instance"},
	{PropertyName: "guardianId", PredicateIRI: "https://schema.org/agent", TargetType: "guardian"},
}

func TestBuildResourceGraph_EdgeValue_RoundTrip(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{
		"studentId": "stu-1",
		"courseInstanceId": "ci-1",
		"guardianId": "g-1",
		"paymentCadence": "per-term",
		"agreedPrice": 200
	}`)

	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-1", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// Verify EdgeValue can extract each reference property.
	tests := []struct {
		prop string
		want string
	}{
		{"studentId", "stu-1"},
		{"courseInstanceId", "ci-1"},
		{"guardianId", "g-1"},
	}
	for _, tc := range tests {
		got := EdgeValue(graph, enrollmentContext, tc.prop)
		if got != tc.want {
			t.Errorf("EdgeValue(%q) = %q, want %q", tc.prop, got, tc.want)
		}
	}

	// Verify intrinsic properties are in the entity node (not the edges node).
	entityNode := ExtractEntityNode(graph)
	var entity map[string]any
	if err := json.Unmarshal(entityNode, &entity); err != nil {
		t.Fatalf("unmarshal entity node: %v", err)
	}
	if entity["paymentCadence"] != "per-term" {
		t.Errorf("entity node paymentCadence = %v, want per-term", entity["paymentCadence"])
	}
	if _, hasRef := entity["studentId"]; hasRef {
		t.Error("entity node should not contain reference property studentId")
	}
}

func TestBuildResourceGraph_NoRefs(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"paymentCadence": "monthly", "agreedPrice": 100}`)

	graph, err := BuildResourceGraph(data, nil, "enr-2", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// With no reference properties, @graph should have exactly 1 element.
	var doc map[string]any
	if err := json.Unmarshal(graph, &doc); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}
	graphArr, ok := doc["@graph"].([]any)
	if !ok {
		t.Fatal("expected @graph array")
	}
	if len(graphArr) != 1 {
		t.Errorf("@graph length = %d, want 1 (entity node only)", len(graphArr))
	}

	// EdgeValue should return empty for any property.
	if got := EdgeValue(graph, enrollmentContext, "studentId"); got != "" {
		t.Errorf("EdgeValue(studentId) = %q, want empty", got)
	}
}

func TestBuildResourceGraph_EmptyRefValue(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"studentId": "", "courseInstanceId": "ci-1", "paymentCadence": "monthly"}`)

	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-3", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// Empty studentId should be omitted from edges.
	if got := EdgeValue(graph, enrollmentContext, "studentId"); got != "" {
		t.Errorf("EdgeValue(studentId) = %q, want empty (was empty string)", got)
	}
	// Non-empty courseInstanceId should be present.
	if got := EdgeValue(graph, enrollmentContext, "courseInstanceId"); got != "ci-1" {
		t.Errorf("EdgeValue(courseInstanceId) = %q, want ci-1", got)
	}
}

func TestAddEdgeToGraph_Idempotent(t *testing.T) {
	t.Parallel()

	// Start with a @graph that already has an edge (as produced by
	// BuildResourceGraph when given a reference property).
	data := json.RawMessage(`{
		"studentId": "stu-1",
		"courseInstanceId": "ci-1",
		"paymentCadence": "per-term"
	}`)
	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-1", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// Re-apply the same edge (simulates Triple.Created replay during projection).
	afterReplay, err := AddEdgeToGraph(graph, "https://schema.org/participant", "stu-1", "enr-1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph: %v", err)
	}

	// The edge should still be a single {"@id": "stu-1"} map, not an array of duplicates.
	var doc map[string]any
	if err := json.Unmarshal(afterReplay, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	graphArr, ok := doc["@graph"].([]any)
	if !ok {
		t.Fatalf("@graph is %T, want []any", doc["@graph"])
	}
	if len(graphArr) < 2 {
		t.Fatalf("expected @graph with edges node, got %d elements", len(graphArr))
	}
	edges, ok := graphArr[1].(map[string]any)
	if !ok {
		t.Fatalf("edges node is %T, want map", graphArr[1])
	}
	ref, ok := edges["studentId"].(map[string]any)
	if !ok {
		t.Fatalf("expected single {@id} ref after idempotent re-add, got %T: %v",
			edges["studentId"], edges["studentId"])
	}
	if ref["@id"] != "stu-1" {
		t.Errorf("expected @id=stu-1, got %v", ref["@id"])
	}

	// Also verify EdgeValue still works after replay.
	if got := EdgeValue(afterReplay, enrollmentContext, "studentId"); got != "stu-1" {
		t.Errorf("EdgeValue after replay = %q, want stu-1", got)
	}
}

func TestAddEdgeToGraph_MultiValued(t *testing.T) {
	t.Parallel()

	// Adding a DIFFERENT object to the same predicate should still accumulate.
	data := json.RawMessage(`{
		"studentId": "stu-1",
		"paymentCadence": "monthly"
	}`)
	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-1", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// Add a second distinct student via triple replay.
	afterAdd, err := AddEdgeToGraph(graph, "https://schema.org/participant", "stu-2", "enr-1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(afterAdd, &doc); err != nil {
		t.Fatalf("unmarshal afterAdd: %v", err)
	}
	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		t.Fatalf("expected @graph with edges node, got %v", doc["@graph"])
	}
	edges, ok := graphArr[1].(map[string]any)
	if !ok {
		t.Fatalf("edges node is %T, want map", graphArr[1])
	}
	arr, ok := edges["studentId"].([]any)
	if !ok {
		t.Fatalf("expected array for the multi-valued property, got %T", edges["studentId"])
	}
	if len(arr) != 2 {
		t.Errorf("expected 2 entries, got %d", len(arr))
	}
}

// TestAddEdgeToGraph_Idempotent_ArrayBranch exercises the []any case in the
// type switch — the path that goes through containsEdgeRef. Replaying an
// entry already inside a multi-valued array must be a no-op and not grow
// the array.
func TestAddEdgeToGraph_Idempotent_ArrayBranch(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"studentId": "stu-1", "paymentCadence": "monthly"}`)
	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-1", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// Grow to a 3-element array by adding two more distinct students.
	graph, err = AddEdgeToGraph(graph, "https://schema.org/participant", "stu-2", "enr-1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph(stu-2): %v", err)
	}
	graph, err = AddEdgeToGraph(graph, "https://schema.org/participant", "stu-3", "enr-1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph(stu-3): %v", err)
	}

	// Replay an entry already present in the middle of the array.
	afterReplay, err := AddEdgeToGraph(graph, "https://schema.org/participant", "stu-2", "enr-1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph(stu-2 replay): %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(afterReplay, &doc); err != nil {
		t.Fatalf("unmarshal afterReplay: %v", err)
	}
	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		t.Fatalf("expected @graph with edges node, got %v", doc["@graph"])
	}
	edges, ok := graphArr[1].(map[string]any)
	if !ok {
		t.Fatalf("edges node is %T, want map", graphArr[1])
	}
	arr, ok := edges["studentId"].([]any)
	if !ok {
		t.Fatalf("expected array, got %T", edges["studentId"])
	}
	if len(arr) != 3 {
		t.Errorf("array length after idempotent replay = %d, want 3", len(arr))
	}
}

// TestAddEdgeToGraph_MalformedExisting verifies that corruption is surfaced
// rather than silently overwritten. A bare string at an edge predicate is
// unexpected shape; the caller should learn about it.
func TestAddEdgeToGraph_MalformedExisting(t *testing.T) {
	t.Parallel()

	// Hand-craft a @graph where the edges node has a bare string at the
	// participant predicate — unexpected but syntactically valid JSON.
	graph := json.RawMessage(`{
		"@graph": [
			{"@id": "enr-1", "@type": "Enrollment"},
			{"@id": "enr-1", "https://schema.org/participant": "stu-1"}
		]
	}`)

	if _, err := AddEdgeToGraph(graph, "https://schema.org/participant", "stu-2", "enr-1"); err == nil {
		t.Error("expected error for unexpected-type edge, got nil")
	}
}

// TestAddEdgeToGraph_MalformedArrayEntry verifies error surfacing when an
// existing array contains a non-map entry.
func TestAddEdgeToGraph_MalformedArrayEntry(t *testing.T) {
	t.Parallel()

	graph := json.RawMessage(`{
		"@graph": [
			{"@id": "enr-1", "@type": "Enrollment"},
			{"@id": "enr-1", "https://schema.org/participant": [{"@id": "stu-1"}, "not-a-map"]}
		]
	}`)

	if _, err := AddEdgeToGraph(graph, "https://schema.org/participant", "stu-2", "enr-1"); err == nil {
		t.Error("expected error for malformed array entry, got nil")
	}
}

// TestBuildResourceGraph_ArrayRef pins handling of array-valued reference
// properties. Schemas in this repo (e.g. mealplanning preset) declare
// x-resource-type on array properties, and BuildResourceGraph must
// materialize each entry as a {"@id": ...} ref so downstream readers
// (EdgeValue, projection FK extraction) can see them in the edges node
// without waiting for Triple replay.
func TestBuildResourceGraph_ArrayRef(t *testing.T) {
	t.Parallel()

	// Treat studentId as an array-of-strings reference for this test.
	data := json.RawMessage(`{
		"studentId": ["stu-1", "stu-2", "stu-3"],
		"paymentCadence": "per-term"
	}`)

	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-arr", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(graph, &doc); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}
	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		t.Fatalf("expected @graph with edges node, got %v", doc["@graph"])
	}
	edges, ok := graphArr[1].(map[string]any)
	if !ok {
		t.Fatalf("edges node is %T, want map", graphArr[1])
	}
	arr, ok := edges["studentId"].([]any)
	if !ok {
		t.Fatalf("expected array of refs, got %T", edges["studentId"])
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(arr))
	}
	for i, want := range []string{"stu-1", "stu-2", "stu-3"} {
		ref, ok := arr[i].(map[string]any)
		if !ok {
			t.Errorf("entry %d is %T, want {@id} map", i, arr[i])
			continue
		}
		if got, _ := ref["@id"].(string); got != want {
			t.Errorf("entry %d @id = %q, want %q", i, got, want)
		}
	}
}

// TestRemoveEdgeFromGraph_PreservesArrayShape pins that an array-valued
// reference property stays an array even after deletions shrink it to a
// single entry. Without this, FlattenGraph / EdgeValues would emit a
// scalar for a property that's still semantically multi-valued, depending
// purely on event-replay history.
func TestRemoveEdgeFromGraph_PreservesArrayShape(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{
		"studentId": ["stu-1", "stu-2"],
		"paymentCadence": "per-term"
	}`)
	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-rm", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// Remove one of the two refs — the result still belongs to a multi-valued
	// reference property and should be a single-element array, not a scalar.
	afterRemove, err := RemoveEdgeFromGraph(graph, "https://schema.org/participant", "stu-1")
	if err != nil {
		t.Fatalf("RemoveEdgeFromGraph: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(afterRemove, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		t.Fatalf("expected @graph with edges node, got %v", doc["@graph"])
	}
	edges, ok := graphArr[1].(map[string]any)
	if !ok {
		t.Fatalf("edges node is %T, want map", graphArr[1])
	}
	arr, ok := edges["studentId"].([]any)
	if !ok {
		t.Fatalf("predicate value is %T after removal, want []any (shape must not flip to scalar)",
			edges["studentId"])
	}
	if len(arr) != 1 {
		t.Fatalf("array len = %d, want 1", len(arr))
	}
	ref, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("remaining entry is %T, want map", arr[0])
	}
	if got, _ := ref["@id"].(string); got != "stu-2" {
		t.Errorf("remaining @id = %q, want stu-2", got)
	}

	// FlattenGraph must keep the array shape too.
	flat := FlattenGraph(afterRemove, enrollmentContext)
	var flatDoc map[string]any
	if err := json.Unmarshal(flat, &flatDoc); err != nil {
		t.Fatalf("unmarshal flat: %v", err)
	}
	flatArr, ok := flatDoc["studentId"].([]any)
	if !ok {
		t.Errorf("flattened studentId = %T, want []any (array shape must survive flatten)", flatDoc["studentId"])
	} else if len(flatArr) != 1 {
		t.Errorf("flattened studentId len = %d, want 1", len(flatArr))
	}
}

// TestEdgeValues_ArrayRef pins the read-side counterpart of array-valued
// reference properties. EdgeValue must keep working (returning the first @id
// for legacy callers that expect a single string), and EdgeValues must
// return every @id in order.
func TestEdgeValues_ArrayRef(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{
		"studentId": ["stu-1", "stu-2", "stu-3"],
		"paymentCadence": "per-term"
	}`)

	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-arr-rd", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// EdgeValue (single-string API) returns the first entry — preserves
	// behavior for callers that only ever expected a scalar ref.
	if got := EdgeValue(graph, enrollmentContext, "studentId"); got != "stu-1" {
		t.Errorf("EdgeValue(studentId) = %q, want stu-1 (first entry)", got)
	}

	// EdgeValues returns every entry.
	got := EdgeValues(graph, enrollmentContext, "studentId")
	want := []string{"stu-1", "stu-2", "stu-3"}
	if len(got) != len(want) {
		t.Fatalf("EdgeValues len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("EdgeValues[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestFlattenGraph_ArrayRef verifies that array-form edges round-trip back
// to a JSON array of ID strings under the original property name.
func TestFlattenGraph_ArrayRef(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{
		"studentId": ["stu-1", "stu-2"],
		"paymentCadence": "per-term"
	}`)

	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-arr-flat", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	flat := FlattenGraph(graph, enrollmentContext)
	var result map[string]any
	if err := json.Unmarshal(flat, &result); err != nil {
		t.Fatalf("unmarshal flat: %v", err)
	}

	arr, ok := result["studentId"].([]any)
	if !ok {
		t.Fatalf("flattened studentId = %T, want []any", result["studentId"])
	}
	if len(arr) != 2 {
		t.Fatalf("flattened studentId len = %d, want 2", len(arr))
	}
	for i, want := range []string{"stu-1", "stu-2"} {
		if got, _ := arr[i].(string); got != want {
			t.Errorf("flattened studentId[%d] = %q, want %q", i, got, want)
		}
	}
}

// TestExtractReferenceTriples mirrors the array case to ensure the lighter
// helper used by Create/Update emits one triple per array entry without
// performing the unmarshal/marshal round-trip that ExtractAndStripReferences
// does to produce a stripped payload.
func TestExtractReferenceTriples(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{
		"studentId": ["stu-1", "stu-2"],
		"courseInstanceId": "ci-1",
		"paymentCadence": "per-term"
	}`)

	refs, err := ExtractReferenceTriples(data, enrollmentRefProps)
	if err != nil {
		t.Fatalf("ExtractReferenceTriples: %v", err)
	}
	// Expect 2 student triples + 1 course-instance triple = 3.
	if len(refs) != 3 {
		t.Fatalf("got %d triples, want 3: %+v", len(refs), refs)
	}
	counts := map[string]int{}
	for _, r := range refs {
		counts[r.Predicate+"|"+r.Object]++
	}
	want := []string{
		"https://schema.org/participant|stu-1",
		"https://schema.org/participant|stu-2",
		"https://schema.org/object|ci-1",
	}
	for _, key := range want {
		if counts[key] != 1 {
			t.Errorf("triple %q count = %d, want 1", key, counts[key])
		}
	}
}

func TestBuildResourceGraph_FlattenGraph_RoundTrip(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{
		"studentId": "stu-1",
		"courseInstanceId": "ci-1",
		"guardianId": "g-1",
		"paymentCadence": "per-term"
	}`)

	graph, err := BuildResourceGraph(data, enrollmentRefProps, "enr-4", "Enrollment", enrollmentContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	flat := FlattenGraph(graph, enrollmentContext)
	var result map[string]any
	if err := json.Unmarshal(flat, &result); err != nil {
		t.Fatalf("unmarshal flattened: %v", err)
	}

	// Flattened data should have original property names with string values.
	checks := map[string]string{
		"studentId":        "stu-1",
		"courseInstanceId": "ci-1",
		"guardianId":       "g-1",
		"paymentCadence":   "per-term",
	}
	for prop, want := range checks {
		got, _ := result[prop].(string)
		if got != want {
			t.Errorf("flattened[%q] = %q, want %q", prop, got, want)
		}
	}
}

// Schema-declared x-resource-type properties and external link definitions
// should merge into a single []ReferencePropertyDef with schema winning on
// conflicts.
func TestExtractReferencePropertiesWithLinks_MergesSchemaAndExternal(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"name":{"type":"string"},
			"project":{"type":"string","x-resource-type":"project","x-display-property":"name"}
		}
	}`)
	ctx := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	external := []PresetLinkDefinition{
		{
			SourceType: "task", TargetType: "user",
			PropertyName: "assignee", DisplayProperty: "givenName",
		},
	}

	defs := ExtractReferencePropertiesWithLinks(schema, ctx, external)
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs (schema + link), got %d: %+v", len(defs), defs)
	}
	byProp := map[string]ReferencePropertyDef{}
	for _, d := range defs {
		byProp[d.PropertyName] = d
	}
	if byProp["project"].TargetType != "project" || byProp["project"].DisplayProperty != "name" {
		t.Errorf("schema-derived def unexpected: %+v", byProp["project"])
	}
	if byProp["assignee"].TargetType != "user" || byProp["assignee"].DisplayProperty != "givenName" {
		t.Errorf("link-derived def unexpected: %+v", byProp["assignee"])
	}
	if byProp["assignee"].PredicateIRI != "https://schema.org/assignee" {
		t.Errorf("expected predicate resolved via @vocab, got %q", byProp["assignee"].PredicateIRI)
	}
}

func TestExtractReferencePropertiesWithLinks_SchemaWinsOnConflict(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"project":{"type":"string","x-resource-type":"project","x-display-property":"name"}
		}
	}`)
	// External link redefines "project" to point at a different target.
	external := []PresetLinkDefinition{
		{
			SourceType: "task", TargetType: "wrong-target",
			PropertyName: "project", DisplayProperty: "title",
		},
	}
	defs := ExtractReferencePropertiesWithLinks(schema, nil, external)
	if len(defs) != 1 {
		t.Fatalf("expected 1 def after dedup, got %d: %+v", len(defs), defs)
	}
	if defs[0].TargetType != "project" {
		t.Errorf("expected schema to win (project), got %+v", defs[0])
	}
}

func TestExtractReferencePropertiesWithLinks_NilSchemaReturnsExternalOnly(t *testing.T) {
	t.Parallel()
	ctx := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	external := []PresetLinkDefinition{
		{SourceType: "invoice", TargetType: "guardian", PropertyName: "guardian"},
	}
	defs := ExtractReferencePropertiesWithLinks(nil, ctx, external)
	if len(defs) != 1 || defs[0].TargetType != "guardian" {
		t.Errorf("expected single link-only def, got %+v", defs)
	}
}

func TestExtractReferencePropertiesWithLinks_LinkHonorsExplicitPredicate(t *testing.T) {
	t.Parallel()
	external := []PresetLinkDefinition{
		{
			SourceType: "invoice", TargetType: "guardian", PropertyName: "guardian",
			PredicateIRI: "https://example.org/parent-of",
		},
	}
	defs := ExtractReferencePropertiesWithLinks(nil, nil, external)
	if len(defs) != 1 || defs[0].PredicateIRI != "https://example.org/parent-of" {
		t.Errorf("expected explicit predicate preserved, got %+v", defs)
	}
}

func TestExtractReferenceProperties_StillWorksWithoutLinks(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"project":{"type":"string","x-resource-type":"project"}
		}
	}`)
	defs := ExtractReferenceProperties(schema, nil)
	if len(defs) != 1 || defs[0].TargetType != "project" {
		t.Errorf("back-compat broken: %+v", defs)
	}
}

// TestAddEdgeToGraph_SameObjectUnderTwoProperties: one resource can reference
// the same target through two different relationships. Deduping across
// properties would drop the second — a vendor recorded as `maker` would block
// the same vendor arriving as `partner` (issue #515).
func TestAddEdgeToGraph_SameObjectUnderTwoProperties(t *testing.T) {
	t.Parallel()

	doc := json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/",
	    "maker":{"@id":"https://schema.org/maker","@type":"@id"},
	    "partner":{"@id":"https://schema.org/partner","@type":"@id"}},
	  "@graph":[
	    {"@id":"urn:widget:1","@type":"Widget","name":"Bolt cutter"},
	    {"@id":"urn:widget:1","maker":{"@id":"urn:vendor:acme"}}
	  ]}`)

	out, err := AddEdgeToGraph(doc, "https://schema.org/partner", "urn:vendor:acme", "urn:widget:1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph: %v", err)
	}
	edges := edgesNodeOf(t, out)
	for _, property := range []string{"maker", "partner"} {
		ref, ok := edges[property].(map[string]any)
		if !ok {
			t.Fatalf("%s is %T, want a ref — the same object under two properties must keep both",
				property, edges[property])
		}
		if id, _ := ref["@id"].(string); id != "urn:vendor:acme" {
			t.Errorf("%s = %q, want urn:vendor:acme", property, id)
		}
	}
}

// TestAddEdgeToGraph_SharedPredicateReplayDoesNotDuplicate: when several
// properties DO share a predicate, a triple cannot say which one it belongs to.
// Create emits the resource graph and its triples together, so on replay the
// edge is already in place and re-filing it under whichever property sorts
// first would attach it to the wrong one.
func TestAddEdgeToGraph_SharedPredicateReplayDoesNotDuplicate(t *testing.T) {
	t.Parallel()

	const shared = "https://api.example.org/associated"
	doc := json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/",
	    "strandId":{"@id":"` + shared + `","@type":"@id"},
	    "fileId":{"@id":"` + shared + `","@type":"@id"}},
	  "@graph":[
	    {"@id":"urn:question:1","@type":"Question"},
	    {"@id":"urn:question:1","strandId":{"@id":"urn:strand:B"},"fileId":{"@id":"urn:file:D"}}
	  ]}`)

	for _, object := range []string{"urn:strand:B", "urn:file:D"} {
		out, err := AddEdgeToGraph(doc, shared, object, "urn:question:1")
		if err != nil {
			t.Fatalf("AddEdgeToGraph(%s): %v", object, err)
		}
		edges := edgesNodeOf(t, out)
		for property, want := range map[string]string{"strandId": "urn:strand:B", "fileId": "urn:file:D"} {
			ref, ok := edges[property].(map[string]any)
			if !ok {
				t.Fatalf("replaying %s turned %s into %T, want the single ref it started as",
					object, property, edges[property])
			}
			if id, _ := ref["@id"].(string); id != want {
				t.Errorf("replaying %s moved %s to %q, want %q", object, property, id, want)
			}
		}
	}
}

func edgesNodeOf(t *testing.T, graph json.RawMessage) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(graph, &doc); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}
	arr, ok := doc["@graph"].([]any)
	if !ok || len(arr) < 2 {
		t.Fatalf("expected @graph with an edges node, got %v", doc["@graph"])
	}
	edges, ok := arr[1].(map[string]any)
	if !ok {
		t.Fatalf("edges node is %T, want map", arr[1])
	}
	return edges
}

// TestLegacyExpandedRecordStillMutatesAndReads: a record written before issue
// #515 keys its edges by predicate IRI, and its stored context was stripped of
// term mappings, so nothing maps the predicate to a property. Every path must
// fall back to the predicate key rather than start a second, compact edge
// beside the one already there — that would leave the same relationship
// recorded twice under two keys.
func TestLegacyExpandedRecordStillMutatesAndReads(t *testing.T) {
	t.Parallel()

	legacy := json.RawMessage(`{
	  "@context":"https://schema.org/",
	  "@graph":[
	    {"@id":"urn:widget:1","@type":"Widget","name":"Bolt cutter"},
	    {"@id":"urn:widget:1","https://schema.org/maker":{"@id":"urn:vendor:acme"}}
	  ]}`)
	// The TYPE's context still carries the mapping even though the record's
	// stored copy does not; readers are given the type's context.
	typeContext := json.RawMessage(
		`{"@vocab":"https://schema.org/","maker":{"@id":"https://schema.org/maker","@type":"@id"}}`)

	added, err := AddEdgeToGraph(legacy, "https://schema.org/maker", "urn:vendor:globex", "urn:widget:1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph: %v", err)
	}
	edges := edgesNodeOf(t, added)
	if _, wrong := edges["maker"]; wrong {
		t.Error("a compact edge was started beside the expanded one; the relationship is now recorded twice")
	}
	arr, ok := edges["https://schema.org/maker"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("expanded edge is %T with %d refs, want an array of 2",
			edges["https://schema.org/maker"], len(arr))
	}

	removed, err := RemoveEdgeFromGraph(added, "https://schema.org/maker", "urn:vendor:globex")
	if err != nil {
		t.Fatalf("RemoveEdgeFromGraph: %v", err)
	}
	if got := EdgeValues(removed, typeContext, "maker"); !sameStringSlice(got, []string{"urn:vendor:acme"}) {
		t.Errorf("after removal EdgeValues = %v, want [urn:vendor:acme]", got)
	}

	var flat map[string]any
	if err := json.Unmarshal(FlattenGraph(legacy, typeContext), &flat); err != nil {
		t.Fatalf("flatten: %v", err)
	}
	if flat["maker"] != "urn:vendor:acme" {
		t.Errorf("flattened legacy record has maker = %v, want urn:vendor:acme", flat["maker"])
	}
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLegacyRecordKeepsOneEdgeKey guards the premise this whole change rests
// on. buildStorableContext ALWAYS kept string-form terms, and real presets
// declare reference terms that way (`"recipe":"https://schema.org/isPartOf"`),
// so an old record's stored context does resolve its predicates — to a property
// name its edges are NOT stored under.
//
// Resolving from the context alone therefore wrote a SECOND key beside the
// first. Both resolved to the same property on read, so the value returned
// flipped between them at random, and a delete matched neither.
func TestLegacyRecordKeepsOneEdgeKey(t *testing.T) {
	t.Parallel()

	legacy := json.RawMessage(`{
	  "@context":{"@vocab":"https://schema.org/","recipe":"https://schema.org/isPartOf"},
	  "@graph":[
	    {"@id":"urn:meal:1","@type":"Meal"},
	    {"@id":"urn:meal:1","https://schema.org/isPartOf":{"@id":"urn:recipe:A"}}
	  ]}`)
	typeContext := json.RawMessage(`{"@vocab":"https://schema.org/","recipe":"https://schema.org/isPartOf"}`)

	added, err := AddEdgeToGraph(legacy, "https://schema.org/isPartOf", "urn:recipe:B", "urn:meal:1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph: %v", err)
	}
	edges := edgesNodeOf(t, added)
	if _, split := edges["recipe"]; split {
		t.Fatalf("the edge was split across two keys: %v", edges)
	}
	if arr, ok := edges["https://schema.org/isPartOf"].([]any); !ok || len(arr) != 2 {
		t.Fatalf("expected both refs under the one key the record already used, got %v",
			edges["https://schema.org/isPartOf"])
	}

	// Reading the same bytes must give the same answer every time.
	first := string(FlattenGraph(added, typeContext))
	for i := 0; i < 8; i++ {
		if got := string(FlattenGraph(added, typeContext)); got != first {
			t.Fatalf("read %d gave %s, first read gave %s — the value is nondeterministic", i, got, first)
		}
	}

	removed, err := RemoveEdgeFromGraph(legacy, "https://schema.org/isPartOf", "urn:recipe:A")
	if err != nil {
		t.Fatalf("RemoveEdgeFromGraph: %v", err)
	}
	if string(removed) == string(legacy) {
		t.Error("removing an edge from a legacy record was a silent no-op — " +
			"the triple goes but the canonical record keeps it forever")
	}
}

// TestExtractTriplesFromData_CompactEdges: the edges node is keyed by property
// name now, and this reader filtered on predicate IRIs, so it saw nothing at
// all in a compact document. No caller in core reaches it today, which is what
// makes it a trap rather than an outage — it is exported and documented.
func TestExtractTriplesFromData_CompactEdges(t *testing.T) {
	t.Parallel()

	ctx := json.RawMessage(`{"@vocab":"https://schema.org/","recipe":"https://schema.org/isPartOf"}`)
	schema := json.RawMessage(`{"type":"object","properties":{
		"recipe":{"type":"string","x-resource-type":"recipe"}}}`)
	refProps := ExtractReferenceProperties(schema, ctx)

	compact, err := BuildResourceGraph(
		json.RawMessage(`{"recipe":"urn:recipe:A"}`), refProps, "urn:meal:1", "Meal", ctx)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}
	triples := ExtractTriplesFromData(refProps, compact, "urn:meal:1")
	if len(triples) != 1 {
		t.Fatalf("got %d triples from a compact document, want 1", len(triples))
	}
	// The triple carries the PREDICATE whichever key held the edge.
	if triples[0].Predicate != "https://schema.org/isPartOf" {
		t.Errorf("predicate = %q, want the IRI — a triple never carries a property name",
			triples[0].Predicate)
	}
}

// TestBuildStorableContext_KeywordsAndCollapse: `@type` at the top level of a
// @context is a keyword redefinition and invalid JSON-LD 1.1, and a
// vocab-only context must still reduce to the bare string it always did.
func TestBuildStorableContext_KeywordsAndCollapse(t *testing.T) {
	t.Parallel()

	stored := buildStorableContext(json.RawMessage(
		`{"@vocab":"https://schema.org/","@type":"Meal","recipe":"https://schema.org/isPartOf"}`))
	terms, ok := stored.(map[string]any)
	if !ok {
		t.Fatalf("stored context is %T, want an object", stored)
	}
	if _, kept := terms["@type"]; kept {
		t.Error("@type was stored inside the @context; that is a keyword redefinition, " +
			"and the entity node already carries the resource's own @type")
	}
	if terms["recipe"] != "https://schema.org/isPartOf" {
		t.Errorf("the term mapping was dropped: %v", terms)
	}

	if got := buildStorableContext(json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Meal"}`)); got != "https://schema.org/" {
		t.Errorf("a vocab-only context stored as %#v, want the bare string it reduced to before", got)
	}
}

// TestTermlessReferenceKeepsOneEdgeKey covers the shape issue #510 is about and
// the one most types have: a reference property with NO explicit `@context`
// term, resolving through `@vocab`.
//
// BuildResourceGraph stores it under the property name. A triple carries the
// `@vocab`-derived predicate. If the two are resolved by different rules the
// write lands in a second key beside the first, so the same statement is
// recorded twice and a delete removes only one of them — the reference stays
// on the resource after the user clears it.
//
// The stored context here is the bare string form, which is what
// buildStorableContext produces whenever only `@vocab` survives. Parsed as a
// map it yields nothing, so this case is blind unless that form is handled.
func TestTermlessReferenceKeepsOneEdgeKey(t *testing.T) {
	t.Parallel()

	typeContext := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Meal"}`)
	schema := json.RawMessage(`{"type":"object","properties":{
		"name":{"type":"string"},
		"recipeId":{"type":"string","x-resource-type":"recipe"}}}`)
	refProps := ExtractReferenceProperties(schema, typeContext)

	stored, err := BuildResourceGraph(
		json.RawMessage(`{"name":"Dinner","recipeId":"urn:recipe:A"}`),
		refProps, "urn:meal:1", "Meal", typeContext)
	if err != nil {
		t.Fatalf("BuildResourceGraph: %v", err)
	}

	// Create emits the resource graph AND its triples, so this arrives with the
	// edge already in place.
	replayed, err := AddEdgeToGraph(stored, "https://schema.org/recipeId", "urn:recipe:A", "urn:meal:1")
	if err != nil {
		t.Fatalf("AddEdgeToGraph: %v", err)
	}
	edges := edgesNodeOf(t, replayed)
	if _, split := edges["https://schema.org/recipeId"]; split {
		t.Fatalf("the replayed triple started a second key beside the stored one: %v", edges)
	}
	if triples := ExtractTriplesFromData(refProps, replayed, "urn:meal:1"); len(triples) != 1 {
		t.Errorf("got %d triples, want 1 — a duplicated key states the same fact twice", len(triples))
	}

	removed, err := RemoveEdgeFromGraph(replayed, "https://schema.org/recipeId", "urn:recipe:A")
	if err != nil {
		t.Fatalf("RemoveEdgeFromGraph: %v", err)
	}
	var flat map[string]any
	if err := json.Unmarshal(FlattenGraph(removed, typeContext), &flat); err != nil {
		t.Fatalf("flatten: %v", err)
	}
	if _, survived := flat["recipeId"]; survived {
		t.Errorf("the reference survived its delete: %v", flat)
	}
}
