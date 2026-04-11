package gorm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/infrastructure/models"
)

func setupDualProjectionTest(t *testing.T) (
	*ResourceRepository, *projectionManager, context.Context,
) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	parentCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	parentSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	childCtx := json.RawMessage(
		`{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`)
	childSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},"interestRate":{"type":"number"}}}`)

	if err := pm.EnsureTable(ctx, "instrument", parentSchema, parentCtx); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "loan", childSchema, childCtx); err != nil {
		t.Fatal(err)
	}

	repo := &ResourceRepository{db: db, projMgr: pm, logger: &testLogger{}}
	return repo, pm, ctx
}

func makeTestResource(t *testing.T, id, typeSlug, dataJSON string) *entities.Resource {
	t.Helper()
	e := &entities.Resource{}
	if err := e.Restore(
		id, typeSlug, "active",
		json.RawMessage(dataJSON),
		"user-1", "acct-1",
		time.Now(), 1,
	); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return e
}

func TestDualProjection_SavePopulatesBothTables(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupDualProjectionTest(t)

	entity := makeTestResource(t, "urn:loan:001", "loan",
		`{"name":"Home Loan","interestRate":3.5}`)

	if err := repo.Save(ctx, entity); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify loan exists in its own table.
	var loanCount int64
	repo.db.Table("loans").Count(&loanCount)
	if loanCount != 1 {
		t.Fatalf("expected 1 row in loans, got %d", loanCount)
	}

	// Verify loan also projected into ancestor table.
	var instrCount int64
	repo.db.Table("instruments").Count(&instrCount)
	if instrCount != 1 {
		t.Fatalf("expected 1 row in instruments, got %d", instrCount)
	}

	// Verify ancestor row has parent-schema columns but NOT child-only columns.
	var instrRow map[string]any
	repo.db.Table("instruments").Where("id = ?", "urn:loan:001").Take(&instrRow)
	if fmt.Sprint(instrRow["name"]) != "Home Loan" {
		t.Fatalf("ancestor name = %v, want 'Home Loan'", instrRow["name"])
	}
	// interest_rate column should NOT exist in ancestor table.
	if repo.db.Migrator().HasColumn("instruments", "interest_rate") {
		t.Fatal("ancestor table should NOT have child-specific interest_rate column")
	}
}

func TestDualProjection_UpdatePropagates(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupDualProjectionTest(t)

	entity := makeTestResource(t, "urn:loan:002", "loan",
		`{"name":"Car Loan","interestRate":5.0}`)
	if err := repo.Save(ctx, entity); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Update the resource with new data.
	updated := makeTestResource(t, "urn:loan:002", "loan",
		`{"name":"Updated Car Loan","interestRate":4.5}`)
	if err := repo.Update(ctx, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Verify ancestor table has updated name.
	var instrRow map[string]any
	repo.db.Table("instruments").Where("id = ?", "urn:loan:002").Take(&instrRow)
	if fmt.Sprint(instrRow["name"]) != "Updated Car Loan" {
		t.Fatalf("ancestor name = %v, want 'Updated Car Loan'", instrRow["name"])
	}
}

func TestDualProjection_DeleteRemovesFromBothTables(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupDualProjectionTest(t)

	entity := makeTestResource(t, "urn:loan:003", "loan",
		`{"name":"Delete Me","interestRate":2.0}`)
	if err := repo.Save(ctx, entity); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify rows exist in both tables before delete.
	var loanCount, instrCount int64
	repo.db.Table("loans").Count(&loanCount)
	repo.db.Table("instruments").Count(&instrCount)
	if loanCount != 1 || instrCount != 1 {
		t.Fatalf("pre-delete: loans=%d instruments=%d, want 1,1", loanCount, instrCount)
	}

	if err := repo.Delete(ctx, "urn:loan:003"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify rows removed from both tables.
	repo.db.Table("loans").Count(&loanCount)
	repo.db.Table("instruments").Count(&instrCount)
	if loanCount != 0 {
		t.Fatalf("post-delete: loans=%d, want 0", loanCount)
	}
	if instrCount != 0 {
		t.Fatalf("post-delete: instruments=%d, want 0", instrCount)
	}
}

// setupReferenceProjectionTest creates a repo with a Course and CourseInstance
// schema where course-instance.courseId references course. Returns the repo and
// a fresh context for display-column behavior tests.
func setupReferenceProjectionTest(t *testing.T) (*ResourceRepository, context.Context) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	courseSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"}}}`)
	ciSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"courseId":{"type":"string","x-resource-type":"course"}}}`)

	if err := pm.EnsureTable(ctx, "course", courseSchema, nil); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "course-instance", ciSchema, nil); err != nil {
		t.Fatal(err)
	}
	return &ResourceRepository{db: db, projMgr: pm, logger: &testLogger{}}, ctx
}

func TestSaveToProjection_PopulatesDisplayColumnOnCreate(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:001", "course",
		`{"name":"Coding"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}

	ci := makeTestResource(t, "urn:course-instance:001", "course-instance",
		`{"name":"Easter Camp","courseId":"urn:course:001"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save course instance: %v", err)
	}

	var row map[string]any
	if err := repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:001").
		Take(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got := fmt.Sprint(row["course_id_display"]); got != "Coding" {
		t.Errorf("course_id_display = %v, want Coding", row["course_id_display"])
	}
}

func TestSaveToProjection_MissingReferencedRow_FallsBackToCanonical(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Manually insert a canonical resources row for the parent without its
	// projection row, simulating the event-replay ordering case where the
	// child's event reaches the repository before the parent's projection
	// has been written.
	parentData := `{"@graph":[{"@id":"urn:course:orphan","@type":"Course","name":"Ghost Course"}]}`
	parent := makeTestResource(t, "urn:course:orphan", "course", parentData)
	parentModel := models.FromResource(parent)
	if err := repo.db.Create(parentModel).Error; err != nil {
		t.Fatalf("insert canonical parent: %v", err)
	}

	// Child save should fall back to (b) and still populate the display value.
	ci := makeTestResource(t, "urn:course-instance:002", "course-instance",
		`{"name":"Replay CI","courseId":"urn:course:orphan"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:002").Take(&row)
	if got := fmt.Sprint(row["course_id_display"]); got != "Ghost Course" {
		t.Errorf("course_id_display = %v, want Ghost Course (via canonical fallback)", row["course_id_display"])
	}
}

func TestSaveToProjection_UnknownReference_LeavesDisplayNull(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Child references a course that does not exist in either projection
	// or canonical tables. Save must succeed; display stays NULL.
	ci := makeTestResource(t, "urn:course-instance:003", "course-instance",
		`{"name":"Dangling CI","courseId":"urn:course:nonexistent"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:003").Take(&row)
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil", row["course_id_display"])
	}
}

func TestUpdateProjection_RepopulatesDisplayWhenFKChanges(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	c1 := makeTestResource(t, "urn:course:a1", "course", `{"name":"Alpha"}`)
	c2 := makeTestResource(t, "urn:course:b2", "course", `{"name":"Bravo"}`)
	if err := repo.Save(ctx, c1); err != nil {
		t.Fatalf("Save c1: %v", err)
	}
	if err := repo.Save(ctx, c2); err != nil {
		t.Fatalf("Save c2: %v", err)
	}

	ci := makeTestResource(t, "urn:course-instance:upd1", "course-instance",
		`{"name":"Rebinding","courseId":"urn:course:a1"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	var row1 map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:upd1").Take(&row1)
	if got := fmt.Sprint(row1["course_id_display"]); got != "Alpha" {
		t.Fatalf("initial display = %v, want Alpha", row1["course_id_display"])
	}

	// Rebind via Update path.
	rebound := makeTestResource(t, "urn:course-instance:upd1", "course-instance",
		`{"name":"Rebinding","courseId":"urn:course:b2"}`)
	if err := repo.Update(ctx, rebound); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var row2 map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:upd1").Take(&row2)
	if got := fmt.Sprint(row2["course_id_display"]); got != "Bravo" {
		t.Errorf("post-update display = %v, want Bravo", row2["course_id_display"])
	}
}

func TestUpdateData_RepopulatesDisplayWhenFKChanges(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	c1 := makeTestResource(t, "urn:course:p1", "course", `{"name":"Piano"}`)
	c2 := makeTestResource(t, "urn:course:g2", "course", `{"name":"Guitar"}`)
	if err := repo.Save(ctx, c1); err != nil {
		t.Fatalf("Save c1: %v", err)
	}
	if err := repo.Save(ctx, c2); err != nil {
		t.Fatalf("Save c2: %v", err)
	}

	ci := makeTestResource(t, "urn:course-instance:ud1", "course-instance",
		`{"name":"UpdateData CI","courseId":"urn:course:p1"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	// Partial update path: only the FK field is provided.
	patch := json.RawMessage(`{"courseId":"urn:course:g2"}`)
	if err := repo.UpdateData(ctx, "urn:course-instance:ud1", patch, 2); err != nil {
		t.Fatalf("UpdateData: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:ud1").Take(&row)
	if got := fmt.Sprint(row["course_id_display"]); got != "Guitar" {
		t.Errorf("post-UpdateData display = %v, want Guitar", row["course_id_display"])
	}
}

func TestDualProjection_AncestorUsesOwnSchema(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	parentCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	parentSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	childCtx := json.RawMessage(
		`{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`)
	childSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},"interestRate":{"type":"number"}}}`)

	if err := pm.EnsureTable(ctx, "instrument", parentSchema, parentCtx); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "loan", childSchema, childCtx); err != nil {
		t.Fatal(err)
	}

	// Ancestor table: has "name" but NOT "interest_rate".
	if db.Migrator().HasColumn("instruments", "interest_rate") {
		t.Fatal("ancestor table should NOT have child-specific column")
	}
	if !db.Migrator().HasColumn("instruments", "name") {
		t.Fatal("ancestor table should have 'name' from its own schema")
	}

	// Child table: has both.
	if !db.Migrator().HasColumn("loans", "interest_rate") {
		t.Fatal("child table should have interest_rate")
	}
	if !db.Migrator().HasColumn("loans", "name") {
		t.Fatal("child table should have name")
	}
}

// TestUpdateData_ClearsDisplayWhenFKIsNulled verifies the null-FK branch of
// populateDisplayColumns. A patch that sets courseId to null must also null
// the sibling course_id_display — otherwise the UI renders a ghost name for a
// link that no longer exists.
func TestUpdateData_ClearsDisplayWhenFKIsNulled(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:c1", "course", `{"name":"Ceramics"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:nul1", "course-instance",
		`{"name":"Nullable","courseId":"urn:course:c1"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	// Verify display is populated before the clear.
	var pre map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:nul1").Take(&pre)
	if fmt.Sprint(pre["course_id_display"]) != "Ceramics" {
		t.Fatalf("pre-clear display = %v, want Ceramics", pre["course_id_display"])
	}

	// Null out the FK via a partial patch. The display column must be cleared
	// atomically so no stale value survives.
	patch := json.RawMessage(`{"courseId":null}`)
	if err := repo.UpdateData(ctx, "urn:course-instance:nul1", patch, 2); err != nil {
		t.Fatalf("UpdateData: %v", err)
	}

	var post map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:nul1").Take(&post)
	if post["course_id"] != nil {
		t.Errorf("course_id = %v, want nil", post["course_id"])
	}
	if post["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (stale display must be cleared)", post["course_id_display"])
	}
}

// TestPopulateDisplayColumns_RespectsCallerProvidedValue — when the row
// already carries a non-empty display value the helper must not overwrite
// it (letting behaviors inject a curated name without having a real parent
// row to read from).
func TestPopulateDisplayColumns_RespectsCallerProvidedValue(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Intentionally do NOT create the referenced course row; we want to prove
	// the helper skips the lookup entirely when a display is pre-populated.
	row := map[string]any{
		"id":                "urn:course-instance:pre1",
		"type_slug":         "course-instance",
		"status":            "active",
		"sequence_no":       1,
		"course_id":         "urn:course:ghost",
		"course_id_display": "Pre-Seeded Name",
	}
	repo.populateDisplayColumns(ctx, "course-instance", row, nil)
	if row["course_id_display"] != "Pre-Seeded Name" {
		t.Errorf("course_id_display = %v, want 'Pre-Seeded Name' (caller value must be respected)",
			row["course_id_display"])
	}
}

// TestFindFlatByID_ReturnsErrNoProjectionTable verifies the sentinel contract
// advertised in the interface docstring — callers use errors.Is to detect this
// case and fall back to the canonical entity path.
func TestFindFlatByID_ReturnsErrNoProjectionTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	repo := &ResourceRepository{db: db, projMgr: pm, logger: &testLogger{}}

	_, err := repo.FindFlatByID(context.Background(), "nonexistent-type", "urn:course:x")
	if !errors.Is(err, repositories.ErrNoProjectionTable) {
		t.Errorf("err = %v, want wrapping repositories.ErrNoProjectionTable", err)
	}
}

// TestFindFlatByID_ReturnsErrNotFound verifies missing-row detection —
// the projection table exists but the id isn't there.
func TestFindFlatByID_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	_, err := repo.FindFlatByID(ctx, "course", "urn:course:missing")
	if !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("err = %v, want wrapping repositories.ErrNotFound", err)
	}
}

// TestFindFlatByID_ReturnsCamelCaseKeys verifies the snake_case → camelCase
// conversion on the returned row, matching the list response shape. A
// regression that swapped the converter would break the frontend silently.
func TestFindFlatByID_ReturnsCamelCaseKeys(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:named", "course", `{"name":"Named"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:fb1", "course-instance",
		`{"name":"CamelCase","courseId":"urn:course:named"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	row, err := repo.FindFlatByID(ctx, "course-instance", "urn:course-instance:fb1")
	if err != nil {
		t.Fatalf("FindFlatByID: %v", err)
	}
	// FK and its display sibling must both arrive as camelCase.
	if _, ok := row["courseId"]; !ok {
		t.Errorf("missing courseId key in %+v", row)
	}
	if got := fmt.Sprint(row["courseIdDisplay"]); got != "Named" {
		t.Errorf("courseIdDisplay = %v, want Named", row["courseIdDisplay"])
	}
	// snake_case keys must NOT leak through.
	if _, ok := row["course_id"]; ok {
		t.Errorf("row leaked snake_case key course_id: %+v", row)
	}
}

// TestForwardReference_ReRegistrationOverwritesDisplayProperty — a schema edit
// that changes x-display-property from "name" to "title" must take effect on
// the next EnsureTable. Before the dedup fix the second registration was
// silently dropped, leaving populateDisplayColumns reading from the wrong field.
func TestForwardReference_ReRegistrationOverwritesDisplayProperty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	v1 := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"courseId":{"type":"string","x-resource-type":"course"}}}`)
	v2 := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"courseId":{"type":"string","x-resource-type":"course","x-display-property":"title"}}}`)

	if err := pm.EnsureTable(ctx, "course-instance", v1, nil); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "course-instance", v2, nil); err != nil {
		t.Fatal(err)
	}

	fwd := pm.ForwardReferences("course-instance")
	if len(fwd) != 1 {
		t.Fatalf("expected 1 forward ref after re-registration, got %d: %+v", len(fwd), fwd)
	}
	if fwd[0].DisplayProperty != "title" {
		t.Errorf("DisplayProperty = %q, want %q (schema edit was silently dropped)",
			fwd[0].DisplayProperty, "title")
	}
	// Reverse side should also reflect the new property.
	revs := pm.ReverseReferences("course")
	if len(revs) != 1 || revs[0].DisplayProperty != "title" {
		t.Errorf("reverse refs for course = %+v, want one entry DisplayProperty=title", revs)
	}
}

// recordingLogger captures Error/Warn calls so display-lookup tests can
// assert that real failures are surfaced via logs (the safety property of the
// log+continue policy).
type recordingLogger struct {
	testLogger
	errors []string
	warns  []string
}

func (l *recordingLogger) Error(_ context.Context, msg string, _ ...any) {
	l.errors = append(l.errors, msg)
}
func (l *recordingLogger) Warn(_ context.Context, msg string, _ ...any) {
	l.warns = append(l.warns, msg)
}

// TestSaveToProjection_DisplayLookupError_LogsAndPersists is the load-bearing
// safety test for the log+continue durability policy. A reference target with
// corrupt JSON-LD makes lookupDisplayValue return an error; populateDisplayColumns
// must log the error and persist the row anyway with a NULL display column. A
// regression that re-introduced "abort the write on lookup error" would strand
// the canonical row and break the eventual-consistency contract Save advertises.
func TestSaveToProjection_DisplayLookupError_LogsAndPersists(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	logger := &recordingLogger{}
	pm := &projectionManager{db: db, logger: logger}
	ctx := context.Background()

	courseSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"}}}`)
	ciSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"courseId":{"type":"string","x-resource-type":"course"}}}`)
	if err := pm.EnsureTable(ctx, "course", courseSchema, nil); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "course-instance", ciSchema, nil); err != nil {
		t.Fatal(err)
	}
	repo := &ResourceRepository{db: db, projMgr: pm, logger: logger}

	// Insert a corrupt canonical resources row directly (bypassing Save) so
	// lookupDisplayFromCanonical's json.Unmarshal returns an error. We do NOT
	// create a projection row for the parent — the projection lookup will
	// miss and we fall through to the canonical path that hits the corrupt blob.
	corruptParent := makeTestResource(t, "urn:course:corrupt", "course", `not json at all{`)
	parentModel := models.FromResource(corruptParent)
	if err := db.Create(parentModel).Error; err != nil {
		t.Fatalf("insert corrupt parent: %v", err)
	}

	// Save the child. The lookup should fail (corrupt JSON-LD), get logged, and
	// the write must still succeed with course_id_display NULL.
	ci := makeTestResource(t, "urn:course-instance:corrupt-ref", "course-instance",
		`{"name":"Tolerant","courseId":"urn:course:corrupt"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save must not abort on display lookup error: %v", err)
	}

	// Row exists; display is NULL.
	var row map[string]any
	if err := db.Table("course_instances").Where("id = ?", "urn:course-instance:corrupt-ref").
		Take(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (NULL on lookup failure)", row["course_id_display"])
	}

	// Operator-visibility check: the failure must have been logged at Error level
	// so it shows up in production telemetry.
	if len(logger.errors) == 0 {
		t.Errorf("expected at least one Error log for the failed display lookup, got %d", len(logger.errors))
	}
}

// TestPopulateDisplayColumns_NonStringFK_LogsAndSkips covers the schema-drift
// branch: if extractFlatColumns ever produces a non-string value for an FK,
// the helper should warn (not crash) and leave the display alone.
func TestPopulateDisplayColumns_NonStringFK_LogsAndSkips(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	logger := &recordingLogger{}
	pm := &projectionManager{db: db, logger: logger}
	ctx := context.Background()

	ciSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"courseId":{"type":"string","x-resource-type":"course"}}}`)
	if err := pm.EnsureTable(ctx, "course-instance", ciSchema, nil); err != nil {
		t.Fatal(err)
	}
	repo := &ResourceRepository{db: db, projMgr: pm, logger: logger}

	row := map[string]any{
		"id":        "urn:course-instance:typo",
		"course_id": 12345, // not a string — represents schema drift
	}
	repo.populateDisplayColumns(ctx, "course-instance", row, nil)

	if _, ok := row["course_id_display"]; ok {
		t.Errorf("course_id_display should not have been set on non-string FK, got %v", row["course_id_display"])
	}
	if len(logger.warns) == 0 {
		t.Errorf("expected at least one Warn log for non-string FK, got %d", len(logger.warns))
	}
}

// TestSaveToProjection_DualProjectionAncestor_SkipsMissingDisplayColumn covers
// the previously-untested HasColumn skip branch in populateDisplayColumns.
// Setup: parent type "instrument" has a "name" field only; child type "loan"
// adds an x-resource-type ref to "person". Saving a loan must populate the
// display on the loans table while leaving the instruments ancestor row
// untouched on the missing display column.
func TestSaveToProjection_DualProjectionAncestor_SkipsMissingDisplayColumn(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	// Person type — referenced by loans but not by instruments.
	personSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"}}}`)
	if err := pm.EnsureTable(ctx, "person", personSchema, nil); err != nil {
		t.Fatal(err)
	}

	// Ancestor "instrument" — has "name" only, no x-resource-type ref.
	instrumentCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	instrumentSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"}}}`)
	if err := pm.EnsureTable(ctx, "instrument", instrumentSchema, instrumentCtx); err != nil {
		t.Fatal(err)
	}

	// Child "loan" — subClassOf instrument, has x-resource-type:person ref.
	loanCtx := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`)
	loanSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"interestRate":{"type":"number"},` +
		`"ownerId":{"type":"string","x-resource-type":"person"}}}`)
	if err := pm.EnsureTable(ctx, "loan", loanSchema, loanCtx); err != nil {
		t.Fatal(err)
	}

	// Sanity: ancestor table must NOT have an owner_id_display column —
	// that's the precise scenario the HasColumn skip protects against.
	if db.Migrator().HasColumn("instruments", "owner_id_display") {
		t.Fatal("ancestor 'instruments' should not have owner_id_display column")
	}

	repo := &ResourceRepository{db: db, projMgr: pm, logger: &testLogger{}}

	// Save the parent person so display lookup hits projection path (a).
	person := makeTestResource(t, "urn:person:alice", "person", `{"name":"Alice"}`)
	if err := repo.Save(ctx, person); err != nil {
		t.Fatalf("Save person: %v", err)
	}

	// Save a loan. Without the HasColumn skip this would crash trying to
	// write owner_id_display into the instruments ancestor table.
	loan := makeTestResource(t, "urn:loan:dual", "loan",
		`{"name":"Mortgage","interestRate":3.5,"ownerId":"urn:person:alice"}`)
	if err := repo.Save(ctx, loan); err != nil {
		t.Fatalf("Save loan (HasColumn skip should prevent ancestor crash): %v", err)
	}

	// Child row has display populated.
	var loanRow map[string]any
	if err := db.Table("loans").Where("id = ?", "urn:loan:dual").Take(&loanRow).Error; err != nil {
		t.Fatalf("read loan: %v", err)
	}
	if fmt.Sprint(loanRow["owner_id_display"]) != "Alice" {
		t.Errorf("loan owner_id_display = %v, want Alice", loanRow["owner_id_display"])
	}

	// Ancestor row exists with the parent-schema columns only.
	var instrRow map[string]any
	if err := db.Table("instruments").Where("id = ?", "urn:loan:dual").Take(&instrRow).Error; err != nil {
		t.Fatalf("read instrument ancestor row: %v", err)
	}
	if fmt.Sprint(instrRow["name"]) != "Mortgage" {
		t.Errorf("ancestor name = %v, want Mortgage", instrRow["name"])
	}
}

// makeTestResourceForAccount restores a Resource with an explicit account_id,
// for tests that exercise cross-account scoping.
func makeTestResourceForAccount(t *testing.T, id, typeSlug, dataJSON, accountID string) *entities.Resource {
	t.Helper()
	e := &entities.Resource{}
	if err := e.Restore(
		id, typeSlug, "active",
		json.RawMessage(dataJSON),
		"user-x", accountID,
		time.Now(), 1,
	); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return e
}

// TestSaveToProjection_CrossAccountReference_DisplayStaysNull is the security
// regression test for the cross-account display leak. A user in account A
// references a course in account B by id. The lookup must NOT denormalize the
// other account's name into the user's row, even though the FK is technically
// resolvable. The display column must end up NULL.
func TestSaveToProjection_CrossAccountReference_DisplayStaysNull(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Course owned by account B.
	otherCourse := makeTestResourceForAccount(t, "urn:course:other", "course",
		`{"name":"Secret Course"}`, "acct-B")
	if err := repo.Save(ctx, otherCourse); err != nil {
		t.Fatalf("Save other course: %v", err)
	}

	// Child written by a user in account A referencing account B's course.
	ci := makeTestResourceForAccount(t, "urn:course-instance:leak1", "course-instance",
		`{"name":"Tenant A","courseId":"urn:course:other"}`, "acct-A")
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	var row map[string]any
	if err := repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:leak1").
		Take(&row).Error; err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (cross-account lookup must not leak the name)",
			row["course_id_display"])
	}
}

// TestSaveToProjection_CrossAccountCanonicalFallback_DisplayStaysNull mirrors
// the above for the canonical fallback path. The referenced course only
// exists in the canonical resources table (no projection row), forcing the
// canonical-fallback branch — which must also enforce account scoping.
func TestSaveToProjection_CrossAccountCanonicalFallback_DisplayStaysNull(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Insert canonical row directly without a projection row, in account B.
	otherCourse := makeTestResourceForAccount(t, "urn:course:canon-other", "course",
		`{"@graph":[{"@id":"urn:course:canon-other","@type":"Course","name":"Hidden"}]}`, "acct-B")
	otherModel := models.FromResource(otherCourse)
	if err := repo.db.Create(otherModel).Error; err != nil {
		t.Fatalf("insert canonical other: %v", err)
	}

	// Child in account A references account B's course.
	ci := makeTestResourceForAccount(t, "urn:course-instance:leak2", "course-instance",
		`{"name":"Tenant A","courseId":"urn:course:canon-other"}`, "acct-A")
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:leak2").Take(&row)
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (canonical fallback must enforce account scope)",
			row["course_id_display"])
	}
}

// TestSaveToProjection_SameAccountReference_DisplayPopulated is the
// counter-test to the cross-account guards: when caller and target are in
// the same account, the lookup must still succeed.
func TestSaveToProjection_SameAccountReference_DisplayPopulated(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResourceForAccount(t, "urn:course:sameacct", "course",
		`{"name":"Yoga"}`, "acct-shared")
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResourceForAccount(t, "urn:course-instance:sameacct", "course-instance",
		`{"name":"Morning","courseId":"urn:course:sameacct"}`, "acct-shared")
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:sameacct").Take(&row)
	if fmt.Sprint(row["course_id_display"]) != "Yoga" {
		t.Errorf("course_id_display = %v, want Yoga (same-account lookup must succeed)",
			row["course_id_display"])
	}
}

// TestUpdateProjection_RebindToUnknownReference_ClearsDisplay covers the
// ghost-name regression: when the FK is rebound from a valid to an unknown
// reference, the prior display value must NOT survive in the database. The
// row map's display column must be set to nil so the UPDATE clears it.
func TestUpdateProjection_RebindToUnknownReference_ClearsDisplay(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:initial", "course", `{"name":"Initial"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:rebind", "course-instance",
		`{"name":"Rebind","courseId":"urn:course:initial"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	// Confirm baseline display is populated before the rebind.
	var pre map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:rebind").Take(&pre)
	if fmt.Sprint(pre["course_id_display"]) != "Initial" {
		t.Fatalf("baseline display = %v, want Initial", pre["course_id_display"])
	}

	// Rebind to a course that does not exist anywhere.
	rebound := makeTestResource(t, "urn:course-instance:rebind", "course-instance",
		`{"name":"Rebind","courseId":"urn:course:vanished"}`)
	if err := repo.Update(ctx, rebound); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var post map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:rebind").Take(&post)
	if post["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (unresolved rebind must clear stale display)",
			post["course_id_display"])
	}
}

// TestUpdateProjection_RebindOnLookupError_ClearsDisplay verifies that even
// when the lookup *errors* (e.g. corrupt JSON-LD), the prior display value is
// cleared instead of persisting as a ghost. Combined with the log+continue
// policy, this means: an FK rebind to a corrupt target = NULL display + log,
// not stale "Initial" + log.
func TestUpdateProjection_RebindOnLookupError_ClearsDisplay(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()
	courseSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	ciSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"courseId":{"type":"string","x-resource-type":"course"}}}`)
	if err := pm.EnsureTable(ctx, "course", courseSchema, nil); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "course-instance", ciSchema, nil); err != nil {
		t.Fatal(err)
	}
	logger := &recordingLogger{}
	repo := &ResourceRepository{db: db, projMgr: pm, logger: logger}

	// Baseline: child references a real, healthy course.
	good := makeTestResource(t, "urn:course:good", "course", `{"name":"Good"}`)
	if err := repo.Save(ctx, good); err != nil {
		t.Fatal(err)
	}
	ci := makeTestResource(t, "urn:course-instance:err-rebind", "course-instance",
		`{"name":"Was-Good","courseId":"urn:course:good"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatal(err)
	}

	// Insert a corrupt canonical row for the new target — no projection row.
	corrupt := makeTestResource(t, "urn:course:bad", "course", `not json{`)
	if err := db.Create(models.FromResource(corrupt)).Error; err != nil {
		t.Fatal(err)
	}

	// Rebind. The lookup will error (json parse failure) → row map should
	// still set the display to nil so the UPDATE clears the stale "Good".
	rebound := makeTestResource(t, "urn:course-instance:err-rebind", "course-instance",
		`{"name":"Was-Good","courseId":"urn:course:bad"}`)
	if err := repo.Update(ctx, rebound); err != nil {
		t.Fatalf("Update must not abort on lookup error: %v", err)
	}

	var row map[string]any
	db.Table("course_instances").Where("id = ?", "urn:course-instance:err-rebind").Take(&row)
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (lookup error must clear stale display)",
			row["course_id_display"])
	}
	if len(logger.errors) == 0 {
		t.Errorf("expected at least one Error log for the failed lookup")
	}
}

// makeTestResourceForAgent restores a Resource with explicit created_by AND
// account_id, for tests that exercise the per-agent visibility scoping.
func makeTestResourceForAgent(t *testing.T, id, typeSlug, dataJSON, createdBy, accountID string) *entities.Resource {
	t.Helper()
	e := &entities.Resource{}
	if err := e.Restore(
		id, typeSlug, "active",
		json.RawMessage(dataJSON),
		createdBy, accountID,
		time.Now(), 1,
	); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return e
}

// TestSaveToProjection_OtherAgentSameAccount_DisplayStaysNull verifies the
// per-agent visibility check: a writer in the same account as the referenced
// resource but who is NOT the creator (and has no explicit read grant) must
// not see the referenced resource's name denormalized into their own
// projection row. This is the deeper version of the cross-account check —
// shared account, different agents.
func TestSaveToProjection_OtherAgentSameAccount_DisplayStaysNull(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Course created by user-alice in account A.
	private := makeTestResourceForAgent(t, "urn:course:alice", "course",
		`{"name":"Alice's Private Course"}`, "user-alice", "acct-A")
	if err := repo.Save(ctx, private); err != nil {
		t.Fatalf("Save private course: %v", err)
	}

	// Bob is in the same account but did not create the course and has no
	// explicit permission grant. He references it from his own course-instance.
	bobsCI := makeTestResourceForAgent(t, "urn:course-instance:bobs", "course-instance",
		`{"name":"Bob's CI","courseId":"urn:course:alice"}`, "user-bob", "acct-A")
	if err := repo.Save(ctx, bobsCI); err != nil {
		t.Fatalf("Save bob ci: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:bobs").Take(&row)
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (Bob can't see Alice's private course)",
			row["course_id_display"])
	}
}

// TestSaveToProjection_OtherAgentWithReadGrant_DisplayPopulated verifies the
// other half of the visibility check: when an explicit read grant exists for
// the writer in resource_permissions, the lookup succeeds and the display is
// populated. This is the same access boundary applyVisibilityScope enforces
// for list/get queries.
func TestSaveToProjection_OtherAgentWithReadGrant_DisplayPopulated(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Course created by user-alice in account A.
	shared := makeTestResourceForAgent(t, "urn:course:shared", "course",
		`{"name":"Shared Yoga"}`, "user-alice", "acct-A")
	if err := repo.Save(ctx, shared); err != nil {
		t.Fatalf("Save shared course: %v", err)
	}

	// Grant Bob explicit "read" permission on the course. The schema mirrors
	// the resource_permissions row applyVisibilityScope checks.
	if err := repo.db.Table("resource_permissions").Create(map[string]any{
		"id":          "perm-bob-shared",
		"resource_id": "urn:course:shared",
		"agent_id":    "user-bob",
		"actions":     `["read"]`,
		"granted_by":  "user-alice",
		"granted_at":  time.Now(),
	}).Error; err != nil {
		t.Fatalf("grant permission: %v", err)
	}

	// Bob saves a course-instance referencing the shared course.
	bobsCI := makeTestResourceForAgent(t, "urn:course-instance:bobs-shared", "course-instance",
		`{"name":"Bob's Shared CI","courseId":"urn:course:shared"}`, "user-bob", "acct-A")
	if err := repo.Save(ctx, bobsCI); err != nil {
		t.Fatalf("Save bob ci: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:bobs-shared").Take(&row)
	if fmt.Sprint(row["course_id_display"]) != "Shared Yoga" {
		t.Errorf("course_id_display = %v, want Shared Yoga (read grant should allow display lookup)",
			row["course_id_display"])
	}
}

// TestSaveToProjection_CanonicalFallback_OtherAgent_DisplayStaysNull mirrors
// the visibility check for the canonical-fallback path. The referenced course
// only exists in the canonical resources table; the writer is not the creator;
// the lookup must miss.
func TestSaveToProjection_CanonicalFallback_OtherAgent_DisplayStaysNull(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Insert canonical row only — no projection row — created by Alice.
	private := makeTestResourceForAgent(t, "urn:course:canon-alice", "course",
		`{"@graph":[{"@id":"urn:course:canon-alice","@type":"Course","name":"Alice Canon"}]}`,
		"user-alice", "acct-A")
	if err := repo.db.Create(models.FromResource(private)).Error; err != nil {
		t.Fatalf("insert canonical: %v", err)
	}

	bobsCI := makeTestResourceForAgent(t, "urn:course-instance:canon-bob", "course-instance",
		`{"name":"Canon Bob","courseId":"urn:course:canon-alice"}`, "user-bob", "acct-A")
	if err := repo.Save(ctx, bobsCI); err != nil {
		t.Fatalf("Save bob ci: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:canon-bob").Take(&row)
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (canonical fallback must enforce per-agent visibility)",
			row["course_id_display"])
	}
}

// TestSaveToProjection_CanonicalFallback_WrongTargetType_DisplayStaysNull
// guards against the canonical-fallback type-confusion bug. A child writes
// an FK that points at an entity of the wrong type (urn:product:xyz under
// the courseId column). The projection-path lookup misses naturally because
// it queries the "courses" table by id, but the canonical resources table
// holds every type — without an explicit type-slug check the lookup would
// extract the product's display property and persist it under
// course_id_display, breaking referential integrity.
func TestSaveToProjection_CanonicalFallback_WrongTargetType_DisplayStaysNull(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Insert a canonical row of the WRONG type ("product") at an id that the
	// child is going to use as if it were a course. No projection row, so the
	// lookup falls through to canonical.
	wrongType := makeTestResource(t, "urn:product:wrong", "product",
		`{"@graph":[{"@id":"urn:product:wrong","@type":"Product","name":"Stolen Name"}]}`)
	if err := repo.db.Create(models.FromResource(wrongType)).Error; err != nil {
		t.Fatalf("insert wrong-type canonical: %v", err)
	}

	// Child stores the wrong-type id under courseId — schema drift / malicious input.
	ci := makeTestResource(t, "urn:course-instance:wrong-type", "course-instance",
		`{"name":"Drift","courseId":"urn:product:wrong"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:wrong-type").Take(&row)
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (wrong-type FK must not denormalize the other entity)",
			row["course_id_display"])
	}
}

// TestEnsureTable_SchemaEditRemovesXResourceType_ClearsStaleRefs verifies
// the clear-and-rebuild behavior of registerReverseReferences. A schema is
// first registered with an x-resource-type property, then re-registered with
// the property removed. The reverse-ref bucket for the old target type and
// the forward-ref bucket for the referencing type must both lose the stale
// entry — otherwise propagation would still try to write display values into
// columns the new schema no longer has.
func TestEnsureTable_SchemaEditRemovesXResourceType_ClearsStaleRefs(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	v1 := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},` +
		`"courseId":{"type":"string","x-resource-type":"course"}}}`)
	v2 := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"}}}`)

	if err := pm.EnsureTable(ctx, "course-instance", v1, nil); err != nil {
		t.Fatal(err)
	}
	// Sanity: v1 produces a forward and a reverse ref.
	if got := len(pm.ForwardReferences("course-instance")); got != 1 {
		t.Fatalf("after v1: forward refs = %d, want 1", got)
	}
	if got := len(pm.ReverseReferences("course")); got != 1 {
		t.Fatalf("after v1: reverse refs for course = %d, want 1", got)
	}

	// v2 drops courseId entirely.
	if err := pm.EnsureTable(ctx, "course-instance", v2, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(pm.ForwardReferences("course-instance")); got != 0 {
		t.Errorf("after v2: forward refs = %d, want 0 (stale entry not cleared)", got)
	}
	if got := len(pm.ReverseReferences("course")); got != 0 {
		t.Errorf("after v2: reverse refs for course = %d, want 0 (stale entry not cleared)", got)
	}
}

// TestEnsureTable_SchemaEditRepointsXResourceType_ClearsStaleTarget verifies
// the more subtle case where an x-resource-type is repointed at a different
// target. v1 has courseId → course; v2 has courseId → module. The "course"
// reverse-ref bucket must drop its entry, and a new entry must appear under
// "module".
func TestEnsureTable_SchemaEditRepointsXResourceType_ClearsStaleTarget(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	v1 := json.RawMessage(`{"type":"object","properties":{` +
		`"courseId":{"type":"string","x-resource-type":"course"}}}`)
	v2 := json.RawMessage(`{"type":"object","properties":{` +
		`"courseId":{"type":"string","x-resource-type":"module"}}}`)

	if err := pm.EnsureTable(ctx, "course-instance", v1, nil); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "course-instance", v2, nil); err != nil {
		t.Fatal(err)
	}

	if got := len(pm.ReverseReferences("course")); got != 0 {
		t.Errorf("after repoint: reverse refs for course = %d, want 0 (stale target not cleared)", got)
	}
	revs := pm.ReverseReferences("module")
	if len(revs) != 1 || revs[0].ReferencingTypeSlug != "course-instance" {
		t.Errorf("after repoint: reverse refs for module = %+v, want one entry for course-instance", revs)
	}
	fwd := pm.ForwardReferences("course-instance")
	if len(fwd) != 1 || fwd[0].TargetTypeSlug != "module" {
		t.Errorf("after repoint: forward refs = %+v, want one entry pointing at module", fwd)
	}
}

// TestUpdateData_LoadsScopeFromCanonicalRow verifies the visibility-scope
// loading path in updateDataInProjection. The partial UpdateData patch
// doesn't carry account_id/created_by, but the existing row owner must
// still constrain the display lookup. We construct a scenario where:
//   - the existing course-instance is owned by user-bob in account A;
//   - the patch rebinds courseId to a course owned by user-alice in account A
//     (same account, different agent — Bob can't read Alice's course);
//   - UpdateData should load Bob's owner scope from the canonical row and
//     pass it to populateDisplayColumns, which then refuses to populate
//     course_id_display.
func TestUpdateData_LoadsScopeFromCanonicalRow(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Alice's private course.
	alice := makeTestResourceForAgent(t, "urn:course:alice-priv", "course",
		`{"name":"Alice Private"}`, "user-alice", "acct-A")
	if err := repo.Save(ctx, alice); err != nil {
		t.Fatalf("Save alice: %v", err)
	}

	// Bob's course-instance, initially with no FK so no display to clear.
	bobsCI := makeTestResourceForAgent(t, "urn:course-instance:bob-update", "course-instance",
		`{"name":"Bob Update"}`, "user-bob", "acct-A")
	if err := repo.Save(ctx, bobsCI); err != nil {
		t.Fatalf("Save bob ci: %v", err)
	}

	// Patch the FK to point at Alice's private course. Bob has no read
	// permission — display lookup must miss.
	patch := json.RawMessage(`{"courseId":"urn:course:alice-priv"}`)
	if err := repo.UpdateData(ctx, "urn:course-instance:bob-update", patch, 2); err != nil {
		t.Fatalf("UpdateData: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:bob-update").Take(&row)
	if row["course_id"] != "urn:course:alice-priv" {
		t.Errorf("course_id = %v, want urn:course:alice-priv (FK update should still happen)", row["course_id"])
	}
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil (Bob can't see Alice's private course via UpdateData)",
			row["course_id_display"])
	}
}

// TestUpdateData_LoadsScopeFromCanonicalRow_AllowsCreatorMatch is the
// counter-test: when the canonical row's owner matches the FK target's
// owner, UpdateData populates the display normally. Confirms that loading
// scope from the canonical row doesn't accidentally block legitimate
// same-creator partial updates.
func TestUpdateData_LoadsScopeFromCanonicalRow_AllowsCreatorMatch(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	// Course and instance both owned by Alice.
	course := makeTestResourceForAgent(t, "urn:course:alice-pub", "course",
		`{"name":"Alice Public"}`, "user-alice", "acct-A")
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResourceForAgent(t, "urn:course-instance:alice-update", "course-instance",
		`{"name":"Alice CI"}`, "user-alice", "acct-A")
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	patch := json.RawMessage(`{"courseId":"urn:course:alice-pub"}`)
	if err := repo.UpdateData(ctx, "urn:course-instance:alice-update", patch, 2); err != nil {
		t.Fatalf("UpdateData: %v", err)
	}

	var row map[string]any
	repo.db.Table("course_instances").Where("id = ?", "urn:course-instance:alice-update").Take(&row)
	if fmt.Sprint(row["course_id_display"]) != "Alice Public" {
		t.Errorf("course_id_display = %v, want 'Alice Public' (same-owner UpdateData should populate)",
			row["course_id_display"])
	}
}
