package gorm

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Issue #550 — an unset of a projected property never reaches the flat
// projection. The event store records the clear correctly; the projection row
// keeps the old value forever.
//
// The cause is one shared shape in all three projection write paths: the row is
// built from the keys ExtractFlatColumns produced, and a property that went
// away produces no key. updateProjectionBySlug then derives its ON CONFLICT
// column list from those same keys, so the column is never in the UPDATE.
//
// The update path is what makes clearing the correct reading. It replaces the
// entity's data WHOLESALE — application/event_handlers.go restores the stored
// resource with state.Data and keeps only metadata from the stored row — so
// entity.Data() at projection time is the full intended state, and a projected
// column absent from it should be cleared.
//
// These tests split into two halves that must be read together:
//
//   - The CLEAR tests fail before the change. They are the regression the issue
//     asks for.
//   - The KEEP tests pass before the change. They are the boundary: a fix that
//     nulls every column absent from the row would pass the first half and fail
//     these. Do not delete one half without the other.

// --- the clear: a property absent from the entity's data is nulled ---

// TestUpdateProjection_ClearedReference_NullsFKColumn is the issue's headline
// claim. The reference is removed through the normal update path, so the FK
// column must be NULL rather than holding the target it used to name.
func TestUpdateProjection_ClearedReference_NullsFKColumn(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:clr1", "course", `{"name":"Coding"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:clr1", "course-instance",
		`{"name":"Easter Camp","courseId":"urn:course:clr1"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save course instance: %v", err)
	}

	// The whole document, minus the reference — how a client clears one.
	cleared := makeTestResource(t, "urn:course-instance:clr1", "course-instance",
		`{"name":"Easter Camp"}`)
	if err := repo.Update(ctx, cleared); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", "urn:course-instance:clr1")
	if row["course_id"] != nil {
		t.Errorf("course_id = %v, want nil after the reference was cleared", row["course_id"])
	}
}

// TestUpdateProjection_ClearedReference_NullsDisplayColumn pins the sibling
// display column to the SAME write. A cleared FK with a surviving display name
// is a ghost label in the UI, which is the symptom a reader actually sees.
func TestUpdateProjection_ClearedReference_NullsDisplayColumn(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:clr2", "course", `{"name":"Coding"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:clr2", "course-instance",
		`{"name":"Easter Camp","courseId":"urn:course:clr2"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save course instance: %v", err)
	}
	before := readProjectionRow(t, repo, "course_instances", "urn:course-instance:clr2")
	if got := fmt.Sprint(before["course_id_display"]); got != "Coding" {
		t.Fatalf("display before the clear = %v, want Coding", before["course_id_display"])
	}

	cleared := makeTestResource(t, "urn:course-instance:clr2", "course-instance",
		`{"name":"Easter Camp"}`)
	if err := repo.Update(ctx, cleared); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", "urn:course-instance:clr2")
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil in the same write that cleared the FK",
			row["course_id_display"])
	}
}

// TestUpdateProjection_ClearedLiteral_NullsColumn generalizes the claim past
// references. The defect is in the column list, not in the edge handling, so a
// literal property removed from the document goes stale in exactly the same way.
func TestUpdateProjection_ClearedLiteral_NullsColumn(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:clr3", "course", `{"name":"Coding"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:clr3", "course-instance",
		`{"name":"Easter Camp","courseId":"urn:course:clr3"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save course instance: %v", err)
	}

	// Reference kept, literal removed.
	cleared := makeTestResource(t, "urn:course-instance:clr3", "course-instance",
		`{"courseId":"urn:course:clr3"}`)
	if err := repo.Update(ctx, cleared); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", "urn:course-instance:clr3")
	if row["name"] != nil {
		t.Errorf("name = %v, want nil after the property was cleared", row["name"])
	}
}

// TestDualProjection_ClearedProperty_NullsAncestorColumn holds the ancestor
// path to the same rule. Save and Update fan out to every ancestor table, so a
// fix applied to one path only leaves an abstract-type read disagreeing with
// the concrete one.
func TestDualProjection_ClearedProperty_NullsAncestorColumn(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupDualProjectionTest(t)

	loan := makeTestResource(t, "urn:loan:clr1", "loan",
		`{"name":"Mortgage","interestRate":4.5}`)
	if err := repo.Save(ctx, loan); err != nil {
		t.Fatalf("Save loan: %v", err)
	}

	// `name` is the one property BOTH the child and the abstract ancestor
	// declare, so clearing it must be visible in both tables.
	cleared := makeTestResource(t, "urn:loan:clr1", "loan", `{"interestRate":4.5}`)
	if err := repo.Update(ctx, cleared); err != nil {
		t.Fatalf("Update: %v", err)
	}

	child := readProjectionRow(t, repo, "loans", "urn:loan:clr1")
	if child["name"] != nil {
		t.Errorf("loans.name = %v, want nil after the property was cleared", child["name"])
	}
	ancestor := readProjectionRow(t, repo, "instruments", "urn:loan:clr1")
	if ancestor["name"] != nil {
		t.Errorf("instruments.name = %v, want nil after the property was cleared", ancestor["name"])
	}
}

// --- the boundary: only a DECLARED SCHEMA PROPERTY is nulled by absence ---

// TestUpdateProjection_ClearedLiteral_KeepsUnrelatedDisplayColumn is the
// display-column boundary on the full-update path.
//
// A display column is DERIVED, not declared: it never appears in a document, so
// it is absent from the row on every single write. A fix that nulls whatever is
// absent — or that nulls AFTER populateDisplayColumns has filled the row —
// wipes the label of a reference the update never touched.
//
// This test passes today. It fails against exactly that mistake.
func TestUpdateProjection_ClearedLiteral_KeepsUnrelatedDisplayColumn(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:keep1", "course", `{"name":"Coding"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:keep1", "course-instance",
		`{"name":"Easter Camp","courseId":"urn:course:keep1"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save course instance: %v", err)
	}

	cleared := makeTestResource(t, "urn:course-instance:keep1", "course-instance",
		`{"courseId":"urn:course:keep1"}`)
	if err := repo.Update(ctx, cleared); err != nil {
		t.Fatalf("Update: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", "urn:course-instance:keep1")
	if got := fmt.Sprint(row["course_id"]); got != "urn:course:keep1" {
		t.Errorf("course_id = %v, want the reference the update kept", row["course_id"])
	}
	if got := fmt.Sprint(row["course_id_display"]); got != "Coding" {
		t.Errorf("course_id_display = %v, want Coding — the update never touched this reference",
			row["course_id_display"])
	}
}

// TestUpdateData_PartialPatch_KeepsOmittedLiteralColumn is the sharpest
// boundary in this file.
//
// UpdateData is a PARTIAL PATCH: its caller supplies only the fields it means
// to change, and populateDisplayColumns is written around that contract ("the
// row doesn't carry the FK key at all (partial UpdateData patch)"). Absence
// means "unchanged" on this path, not "cleared".
//
// The obvious place to add the clear is the shared ExtractFlatColumns +
// dropMissingColumns sequence, which all three write paths run. Adding it there
// turns every partial patch into a wipe of everything the patch omitted. This
// test passes today and fails against that fix.
func TestUpdateData_PartialPatch_KeepsOmittedLiteralColumn(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	c1 := makeTestResource(t, "urn:course:pp1", "course", `{"name":"Piano"}`)
	c2 := makeTestResource(t, "urn:course:pp2", "course", `{"name":"Guitar"}`)
	if err := repo.Save(ctx, c1); err != nil {
		t.Fatalf("Save c1: %v", err)
	}
	if err := repo.Save(ctx, c2); err != nil {
		t.Fatalf("Save c2: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:pp1", "course-instance",
		`{"name":"Summer Camp","courseId":"urn:course:pp1"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	patch := json.RawMessage(`{"courseId":"urn:course:pp2"}`)
	if err := repo.UpdateData(ctx, "urn:course-instance:pp1", patch, 2); err != nil {
		t.Fatalf("UpdateData: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", "urn:course-instance:pp1")
	if got := fmt.Sprint(row["name"]); got != "Summer Camp" {
		t.Errorf("name = %v, want Summer Camp — a partial patch must not clear what it omits",
			row["name"])
	}
}

// TestUpdateData_PartialPatch_KeepsOmittedReferenceAndDisplay is the same
// boundary from the reference side: a patch that names only a literal must
// leave the FK column AND its display sibling exactly as they were.
func TestUpdateData_PartialPatch_KeepsOmittedReferenceAndDisplay(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	course := makeTestResource(t, "urn:course:pp3", "course", `{"name":"Piano"}`)
	if err := repo.Save(ctx, course); err != nil {
		t.Fatalf("Save course: %v", err)
	}
	ci := makeTestResource(t, "urn:course-instance:pp3", "course-instance",
		`{"name":"Summer Camp","courseId":"urn:course:pp3"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save ci: %v", err)
	}

	patch := json.RawMessage(`{"name":"Autumn Camp"}`)
	if err := repo.UpdateData(ctx, "urn:course-instance:pp3", patch, 2); err != nil {
		t.Fatalf("UpdateData: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", "urn:course-instance:pp3")
	if got := fmt.Sprint(row["course_id"]); got != "urn:course:pp3" {
		t.Errorf("course_id = %v, want the reference the patch omitted", row["course_id"])
	}
	if got := fmt.Sprint(row["course_id_display"]); got != "Piano" {
		t.Errorf("course_id_display = %v, want Piano — the patch omitted this reference",
			row["course_id_display"])
	}
}

// TestSaveToProjection_AbsentReference_LeavesFKAndDisplayNull holds the INSERT
// path. saveToProjection has no prior value to clear, so the claim there is
// that the same change does not break the insert: a document with no reference
// still writes a row, with the FK and its display sibling NULL.
func TestSaveToProjection_AbsentReference_LeavesFKAndDisplayNull(t *testing.T) {
	t.Parallel()
	repo, ctx := setupReferenceProjectionTest(t)

	ci := makeTestResource(t, "urn:course-instance:ins1", "course-instance",
		`{"name":"Unattached Camp"}`)
	if err := repo.Save(ctx, ci); err != nil {
		t.Fatalf("Save: %v", err)
	}

	row := readProjectionRow(t, repo, "course_instances", "urn:course-instance:ins1")
	if got := fmt.Sprint(row["name"]); got != "Unattached Camp" {
		t.Errorf("name = %v, want Unattached Camp", row["name"])
	}
	if row["course_id"] != nil {
		t.Errorf("course_id = %v, want nil", row["course_id"])
	}
	if row["course_id_display"] != nil {
		t.Errorf("course_id_display = %v, want nil", row["course_id_display"])
	}
}

// readProjectionRow reads one projection row, failing the test when it is gone.
// Reading a cleared column back needs the distinction between "the row holds
// NULL" and "there is no row", which a bare Take into a map hides.
func readProjectionRow(
	t *testing.T, repo *ResourceRepository, table, id string,
) map[string]any {
	t.Helper()
	var row map[string]any
	if err := repo.db.Table(table).Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatalf("read %s row %s: %v", table, id, err)
	}
	return row
}
