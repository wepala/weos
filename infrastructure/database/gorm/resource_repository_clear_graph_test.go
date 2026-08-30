package gorm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
)

// Issue #550, second pass. The clear added in this branch decides "the client
// cleared this property" from the ABSENCE of a column in the extracted row. An
// extraction that reads the document only partly produces the same absence, so
// the clear erases a value the document still carries.
//
// Every test in resource_repository_clear_test.go drives the LEGACY FLAT form,
// which has no edges node and therefore never meets the edge resolver. These
// tests drive the @graph form the write path actually produces, which is where
// a partial read happens.
//
// The rule the fix must satisfy: null a column ONLY when the extraction is
// known to have read the whole document. Anything less leaves the row stale,
// which is the pre-#550 behavior and is not data loss.

// courseInstanceGraph builds the @graph document ResourceService writes for a
// course instance: an entity node with the intrinsic properties, and an edges
// node keyed by edgeKey when a reference is present.
func courseInstanceGraph(id, name, edgeKey, courseID string) string {
	entity := map[string]any{"@id": id, "@type": "CourseInstance"}
	if name != "" {
		entity["name"] = name
	}
	graph := []any{entity}
	if courseID != "" {
		graph = append(graph, map[string]any{
			"@id": id, edgeKey: map[string]any{"@id": courseID},
		})
	}
	doc, err := json.Marshal(map[string]any{"@graph": graph})
	if err != nil {
		panic(err)
	}
	return string(doc)
}

// setupGraphProjectionTest mirrors setupReferenceProjectionTest but gives the
// course-instance type a real JSON-LD context, so an edges key CAN resolve
// through @vocab and the unresolvable case is a genuine drift rather than an
// absent context.
func setupGraphProjectionTest(t *testing.T) (*ResourceRepository, *capturingLogger, context.Context) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	logger := &capturingLogger{}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	ldCtx := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	courseSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"}}}`)
	ciSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"courseId":{"type":"string","x-resource-type":"course"}}}`)

	if err := pm.EnsureTable(ctx, "course", courseSchema, ldCtx); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "course-instance", ciSchema, ldCtx); err != nil {
		t.Fatal(err)
	}
	return &ResourceRepository{db: db, projMgr: pm, logger: logger}, logger, ctx
}

// seedGraphCourseInstance saves a course and a course instance that references
// it, both in the @graph form, and asserts the projection started populated.
func seedGraphCourseInstance(
	ctx context.Context, t *testing.T, repo *ResourceRepository, id, courseID string,
) {
	t.Helper()
	course := makeTestResource(t, courseID, "course",
		fmt.Sprintf(`{"@graph":[{"@id":%q,"@type":"Course","name":"Coding"}]}`, courseID))
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResource(t, id, "course-instance",
		courseInstanceGraph(id, "Easter Camp", "courseId", courseID))
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save course instance: %v", err)
	}
	row := readProjectionRow(t, repo, "course_instances", id)
	if got := fmt.Sprint(row["course_id"]); got != courseID {
		t.Fatalf("seed course_id = %v, want %s", row["course_id"], courseID)
	}
	if got := fmt.Sprint(row["course_id_display"]); got != "Coding" {
		t.Fatalf("seed course_id_display = %v, want Coding", row["course_id_display"])
	}
}

// --- the clear still works on the @graph path ---

// TestUpdateProjection_GraphEdges_ClearedReference_NullsFKColumn is the #550
// claim on the shape the write path really produces. The reference leaves the
// edges node, so the FK and its display sibling must both be NULL.
func TestUpdateProjection_GraphEdges_ClearedReference_NullsFKColumn(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupGraphProjectionTest(t)
	const id = "urn:course-instance:g1"
	seedGraphCourseInstance(ctx, t, repo, id, "urn:course:g1")

	cleared := makeTestResource(t, id, "course-instance",
		courseInstanceGraph(id, "Easter Camp", "", ""))
	if err := repo.Update(ctx, cleared); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", id)
	if row["course_id"] != nil {
		t.Errorf("course_id = %v, want nil after the reference left the edges node", row["course_id"])
	}
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil in the same write", row["course_id_display"])
	}
}

// TestUpdateProjection_GraphEdges_VocabKeyedReference_Survives holds the
// pre-#515 storage form. An edges key written as a predicate IRI still
// resolves through @vocab, so the reference is read and must NOT be cleared.
func TestUpdateProjection_GraphEdges_VocabKeyedReference_Survives(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupGraphProjectionTest(t)
	const id = "urn:course-instance:g2"
	seedGraphCourseInstance(ctx, t, repo, id, "urn:course:g2")

	kept := makeTestResource(t, id, "course-instance",
		courseInstanceGraph(id, "Easter Camp", "https://schema.org/courseId", "urn:course:g2"))
	if err := repo.Update(ctx, kept); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", id)
	if got := fmt.Sprint(row["course_id"]); got != "urn:course:g2" {
		t.Errorf("course_id = %v, want the reference the @vocab key names", row["course_id"])
	}
}

// --- the boundary: a partly-read document clears nothing ---

// TestUpdateProjection_UnresolvableEdgeKey_KeepsReference is finding 1a.
//
// EdgeProperty returns ok=false for an IRI that no term, alias or @vocab names,
// so the FK never reaches the row ALTHOUGH THE DOCUMENT STILL CARRIES IT. Read
// as a clear, that nulls a live reference. A stored @context can drift — that
// is why resource_type_adopt tells the operator to run `worker reproject`,
// which drives this exact path over the whole history.
func TestUpdateProjection_UnresolvableEdgeKey_KeepsReference(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupGraphProjectionTest(t)
	const id = "urn:course-instance:g3"
	seedGraphCourseInstance(ctx, t, repo, id, "urn:course:g3")

	drifted := makeTestResource(t, id, "course-instance",
		courseInstanceGraph(id, "Easter Camp", "https://other.example/courseId", "urn:course:g3"))
	if err := repo.Update(ctx, drifted); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", id)
	if got := fmt.Sprint(row["course_id"]); got != "urn:course:g3" {
		t.Errorf("course_id = %v, want the reference the document still carries", row["course_id"])
	}
	if got := fmt.Sprint(row["course_id_display"]); got != "Coding" {
		t.Errorf("course_id_display = %v, want Coding", row["course_id_display"])
	}
}

// TestUpdateProjection_UnresolvableEdgeKey_IsLogged pins the diagnostic.
// Context drift must be findable in the log, not silent. Article XI.
func TestUpdateProjection_UnresolvableEdgeKey_IsLogged(t *testing.T) {
	t.Parallel()
	repo, logger, ctx := setupGraphProjectionTest(t)
	const id = "urn:course-instance:g4"
	seedGraphCourseInstance(ctx, t, repo, id, "urn:course:g4")

	drifted := makeTestResource(t, id, "course-instance",
		courseInstanceGraph(id, "Easter Camp", "https://other.example/courseId", "urn:course:g4"))
	if err := repo.Update(ctx, drifted); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !logger.mentions("https://other.example/courseId") {
		t.Errorf("no log entry names the unresolved edge key; entries: %v", logger.entries())
	}
}

// TestUpdateProjection_UnusableEdgeValue_KeepsReference is the third face of
// the same class. The edges key resolves, but its value carries no usable
// reference, so no column value comes out of it. The row is short of the FK for
// a reason that is not a clear, and the rule is the same: keep the old value.
func TestUpdateProjection_UnusableEdgeValue_KeepsReference(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupGraphProjectionTest(t)
	const id = "urn:course-instance:g8"
	seedGraphCourseInstance(ctx, t, repo, id, "urn:course:g8")

	// An edges node whose value is an object with no @id at all.
	malformed := makeTestResource(t, id, "course-instance", fmt.Sprintf(
		`{"@graph":[{"@id":%q,"@type":"CourseInstance","name":"Easter Camp"},`+
			`{"@id":%q,"courseId":{"label":"Coding"}}]}`, id, id))
	if err := repo.Update(ctx, malformed); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", id)
	if got := fmt.Sprint(row["course_id"]); got != "urn:course:g8" {
		t.Errorf("course_id = %v, want the reference the row already held", row["course_id"])
	}
}

// TestUpdateProjection_UnparsableData_KeepsEveryColumn is finding 1b.
//
// ExtractFlatColumns returns silently when the document does not parse, so the
// row carries no property at all. Read as a clear, that erases EVERY declared
// column — a parse failure escalates from "the row goes stale" to "the row is
// erased".
func TestUpdateProjection_UnparsableData_KeepsEveryColumn(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupGraphProjectionTest(t)
	const id = "urn:course-instance:g5"
	seedGraphCourseInstance(ctx, t, repo, id, "urn:course:g5")

	broken := makeTestResource(t, id, "course-instance", `{"@graph":[{"@id":`)
	if err := repo.Update(ctx, broken); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", id)
	if got := fmt.Sprint(row["name"]); got != "Easter Camp" {
		t.Errorf("name = %v, want Easter Camp — an unreadable document clears nothing", row["name"])
	}
	if got := fmt.Sprint(row["course_id"]); got != "urn:course:g5" {
		t.Errorf("course_id = %v, want the reference kept", row["course_id"])
	}
	if got := fmt.Sprint(row["course_id_display"]); got != "Coding" {
		t.Errorf("course_id_display = %v, want Coding", row["course_id_display"])
	}
}

// TestUpdateProjection_UnparsableData_IsLogged pins the diagnostic for a
// document that does not parse. Article XI.
func TestUpdateProjection_UnparsableData_IsLogged(t *testing.T) {
	t.Parallel()
	repo, logger, ctx := setupGraphProjectionTest(t)
	const id = "urn:course-instance:g6"
	seedGraphCourseInstance(ctx, t, repo, id, "urn:course:g6")

	broken := makeTestResource(t, id, "course-instance", `{"@graph":[{"@id":`)
	if err := repo.Update(ctx, broken); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if !logger.mentions(id) {
		t.Errorf("no log entry names the unreadable resource; entries: %v", logger.entries())
	}
}

// --- finding 3: an activated link FK on the wholesale path ---

// TestUpdateProjection_ActivatedLinkFK_ClearedByWholesaleUpdate decides an open
// question: EnsureTable puts a link-activated FK in the DECLARED set, so a
// wholesale update that omits it nulls it. The counter-claim is that a link
// property is not in the type's schema, so a schema-driven client never sends
// one and every update would wipe it.
//
// The test asserts what the CANONICAL store does, then requires the projection
// to agree with it. That is the whole contract of this change, so whichever way
// the canonical store goes, the projection must follow.
func TestUpdateProjection_ActivatedLinkFK_ClearedByWholesaleUpdate(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupGraphProjectionTest(t)
	const id = "urn:course-instance:g7"

	// Activate a link the course-instance schema does not declare.
	if err := repo.projMgr.RegisterLink(ctx, repositories.LinkReference{
		SourceSlug: "course-instance", PropertyName: "sponsorId", TargetSlug: "course",
	}); err != nil {
		t.Fatalf("RegisterLink: %v", err)
	}

	sponsor := makeTestResource(t, "urn:course:g7", "course",
		`{"@graph":[{"@id":"urn:course:g7","@type":"Course","name":"Coding"}]}`)
	if err := repo.Save(ctx, sponsor); err != nil {
		t.Fatalf("Save sponsor: %v", err)
	}
	// A document that carries the link reference in its edges node — exactly
	// what BuildResourceGraph writes, because referencePropsFor merges
	// link-declared references with the schema-declared ones.
	ci := makeTestResource(t, id, "course-instance",
		courseInstanceGraph(id, "Easter Camp", "sponsorId", "urn:course:g7"))
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save course instance: %v", err)
	}
	seeded := readProjectionRow(t, repo, "course_instances", id)
	if got := fmt.Sprint(seeded["sponsor_id"]); got != "urn:course:g7" {
		t.Fatalf("seed sponsor_id = %v, want urn:course:g7", seeded["sponsor_id"])
	}

	// The update a schema-driven client sends: no link property at all.
	cleared := makeTestResource(t, id, "course-instance",
		courseInstanceGraph(id, "Easter Camp", "", ""))
	if err := repo.Update(ctx, cleared); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// The canonical store is the arbiter. It holds the same document, so it
	// no longer carries the link either.
	var canonical models.Resource
	if err := repo.db.Where("id = ?", id).Take(&canonical).Error; err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if wantGone := !containsKey(t, canonical.Data, "sponsorId"); !wantGone {
		t.Fatalf("canonical data still carries sponsorId: %s", canonical.Data)
	}

	row := readProjectionRow(t, repo, "course_instances", id)
	if row["sponsor_id"] != nil {
		t.Errorf("sponsor_id = %v, want nil — the canonical document dropped the link",
			row["sponsor_id"])
	}
	if row["sponsor_id_display"] != nil {
		t.Errorf("sponsor_id_display = %v, want nil", row["sponsor_id_display"])
	}
}

// containsKey reports whether key appears anywhere in a JSON-LD document.
func containsKey(t *testing.T, data string, key string) bool {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		t.Fatalf("parse canonical data: %v", err)
	}
	nodes, _ := doc["@graph"].([]any)
	for _, n := range nodes {
		if node, ok := n.(map[string]any); ok {
			if _, found := node[key]; found {
				return true
			}
		}
	}
	_, found := doc[key]
	return found
}

// capturingLogger keeps every formatted log line, FIELDS INCLUDED so a test can assert that a
// silent condition became a visible one.
type capturingLogger struct {
	mu   sync.Mutex
	logs []string
}

func (l *capturingLogger) record(msg string, fields ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, fmt.Sprint(append([]any{msg}, fields...)...))
}

func (l *capturingLogger) Debug(_ context.Context, msg string, f ...any) { l.record(msg, f...) }
func (l *capturingLogger) Info(_ context.Context, msg string, f ...any)  { l.record(msg, f...) }
func (l *capturingLogger) Warn(_ context.Context, msg string, f ...any)  { l.record(msg, f...) }
func (l *capturingLogger) Error(_ context.Context, msg string, f ...any) { l.record(msg, f...) }

func (l *capturingLogger) entries() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.logs...)
}

func (l *capturingLogger) mentions(substr string) bool {
	for _, e := range l.entries() {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}
