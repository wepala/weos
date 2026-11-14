package gorm

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
)

// These tests pin the reads the owner backfill makes of the projection rows
// (wm-mttt4k). The canonical record and the projection rows are written one
// after the other, so a row can stay ownerless while the record has the
// owner; the backfill must see that row to count and to repair it.

type ownerFixture struct {
	repo                                         *ResourceRepository
	recordOwnerless, ancestorRowOwnerless        string
	ownTypeRowOwnerless, healthy, othersAncestor string
	// ancestorRowMissing has the owner in its record and its own type's row,
	// and no row at all in the ancestor's table (wm-zm4sbz).
	ancestorRowMissing string
}

func seedOwnerFixture(t *testing.T) ownerFixture {
	t.Helper()
	repo, _, ctx := setupDualProjectionTest(t)
	if err := repo.db.AutoMigrate(&models.ResourceType{}); err != nil {
		t.Fatal(err)
	}
	f := ownerFixture{
		repo:                 repo,
		recordOwnerless:      "urn:loan:record-ownerless",
		ancestorRowOwnerless: "urn:loan:ancestor-row-ownerless",
		ownTypeRowOwnerless:  "urn:loan:own-row-ownerless",
		healthy:              "urn:loan:healthy",
		othersAncestor:       "urn:loan:others-ancestor-row-ownerless",
		ancestorRowMissing:   "urn:loan:ancestor-row-missing",
	}
	for _, s := range []struct{ id, agent, account string }{
		{f.recordOwnerless, "agent-1", ""},
		{f.ancestorRowOwnerless, "agent-1", "acct-1"},
		{f.ownTypeRowOwnerless, "agent-1", "acct-1"},
		{f.healthy, "agent-1", "acct-1"},
		{f.othersAncestor, "agent-2", "acct-1"},
		{f.ancestorRowMissing, "agent-1", "acct-1"},
	} {
		if err := repo.Save(ctx, makeTestResourceForAgent(t, s.id, "loan", `{"name":"x"}`, s.agent, s.account)); err != nil {
			t.Fatal(err)
		}
	}
	// The record took the owner and a row did not, as a failed projection
	// write leaves it.
	blank := func(table, id string) {
		if err := repo.db.Table(table).Where("id = ?", id).Update("account_id", "").Error; err != nil {
			t.Fatal(err)
		}
	}
	blank("instruments", f.ancestorRowOwnerless)
	blank("instruments", f.othersAncestor)
	blank("loans", f.ownTypeRowOwnerless)
	// The row was never written: an insert that failed, or a table that
	// arrived after the resource.
	if err := repo.db.Table("instruments").Where("id = ?", f.ancestorRowMissing).
		Delete(map[string]any{}).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFindOwnerlessByCreator_ListsARecordWithAnOwnerWhoseProjectionRowHasNone(t *testing.T) {
	t.Parallel()
	f := seedOwnerFixture(t)
	got, err := f.repo.FindOwnerlessByCreator(t.Context(), "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		f.ancestorRowOwnerless: "acct-1",
		f.ownTypeRowOwnerless:  "acct-1",
		f.recordOwnerless:      "",
		f.ancestorRowMissing:   "acct-1",
	}
	if len(got) != len(want) {
		t.Fatalf("listed %+v, want %v", got, want)
	}
	for i, r := range got {
		owner, ok := want[r.ID]
		if !ok || r.RecordAccountID != owner || r.TypeSlug != "loan" || r.CreatedAt.IsZero() {
			t.Errorf("listed %+v; want record owner %q for it (listed: %v)", r, owner, ok)
		}
		// A resource with no row at all in a table is listed with that table
		// (wm-zm4sbz); no other is.
		if wantMissing := map[bool]string{true: "instruments"}[r.ID == f.ancestorRowMissing]; r.MissingTable != wantMissing {
			t.Errorf("%s: MissingTable %q, want %q", r.ID, r.MissingTable, wantMissing)
		}
		if i > 0 && got[i-1].ID >= r.ID {
			t.Errorf("not in id order: %s before %s", got[i-1].ID, r.ID)
		}
	}
}

// The preview reads the store over a connection with no projection manager,
// so it must find the same rows from the stored resource types alone.
func TestFindOwnerlessByCreator_WithoutAProjectionManagerFindsTheSameRows(t *testing.T) {
	t.Parallel()
	f := seedOwnerFixture(t)
	for _, rt := range []models.ResourceType{
		{ID: "rt-instrument", Name: "Instrument", Slug: "instrument",
			Context: `{"@vocab":"https://schema.org/","weos:abstract":true}`},
		{ID: "rt-loan", Name: "Loan", Slug: "loan",
			Context: `{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`},
	} {
		if err := f.repo.db.Create(&rt).Error; err != nil {
			t.Fatal(err)
		}
	}
	withMgr, err := f.repo.FindOwnerlessByCreator(t.Context(), "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	readOnly := ProvideOwnerlessResourceFinder(f.repo.db)
	got, err := readOnly.FindOwnerlessByCreator(t.Context(), "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || !reflect.DeepEqual(ids(got), ids(withMgr)) {
		t.Errorf("without a projection manager listed %v, with one %v", ids(got), ids(withMgr))
	}
}

func ids(rs []repositories.OwnerlessResource) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID + "@" + r.RecordAccountID + "/" + r.MissingTable
	}
	return out
}

func TestOwnerlessProjectionTables_NamesEachRowWithNoOwner(t *testing.T) {
	t.Parallel()
	f := seedOwnerFixture(t)
	var reader repositories.ProjectionOwnerReader = f.repo
	for id, want := range map[string][]string{
		f.ancestorRowOwnerless: {"instruments"},
		f.ownTypeRowOwnerless:  {"loans"},
		f.recordOwnerless:      {"loans", "instruments"},
		f.healthy:              nil,
		"urn:loan:no-such-row": nil,
	} {
		got, err := reader.OwnerlessProjectionTables(t.Context(), id, "loan")
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: ownerless in %v, want %v", id, got, want)
		}
	}
}

// The repair the backfill makes: writing the record again fills the empty
// owner of each row and keeps a set one.
func TestUpdate_FillsAnOwnerlessProjectionRowFromTheRecord(t *testing.T) {
	t.Parallel()
	f := seedOwnerFixture(t)
	for _, id := range []string{f.ancestorRowOwnerless, f.ownTypeRowOwnerless} {
		e, err := f.repo.FindByID(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.repo.Update(t.Context(), e); err != nil {
			t.Fatal(err)
		}
		if got, err := f.repo.OwnerlessProjectionTables(t.Context(), id, "loan"); err != nil || len(got) != 0 {
			t.Errorf("%s after the update: ownerless in %v (err %v)", id, got, err)
		}
	}
}

// wm-5o1kcm: the backfill's fill writes the record over each ownerless row
// and leaves the record and every owned row as they were.
func TestFillOwnerlessProjectionRows_WritesOnlyTheOwnerlessRows(t *testing.T) {
	t.Parallel()
	f := seedOwnerFixture(t)
	var filler repositories.ProjectionOwnerFiller = f.repo
	for id, want := range map[string][]string{
		f.ancestorRowOwnerless: {"instruments"},
		f.ownTypeRowOwnerless:  {"loans"},
		f.healthy:              nil,
	} {
		e, err := f.repo.FindByID(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		var before models.Resource
		if err := f.repo.db.Where("id = ?", id).First(&before).Error; err != nil {
			t.Fatal(err)
		}
		got, err := filler.FillOwnerlessProjectionRows(t.Context(), e)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: wrote %v, want %v", id, got, want)
		}
		if left, err := f.repo.OwnerlessProjectionTables(t.Context(), id, "loan"); err != nil || len(left) != 0 {
			t.Errorf("%s after the fill: ownerless in %v (err %v)", id, left, err)
		}
		var after models.Resource
		if err := f.repo.db.Where("id = ?", id).First(&after).Error; err != nil {
			t.Fatal(err)
		}
		if !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("%s: the fill rewrote the canonical record (updated_at %v, was %v)", id,
				after.UpdatedAt, before.UpdatedAt)
		}
	}
}

// wm-5o1kcm: a write that lands between the read of the record and the fill
// moves the row past the record. The fill must not put the older record over
// it; the check is in the write statement itself.
func TestFillOwnerlessProjectionRows_LeavesARowThatIsPastTheRecord(t *testing.T) {
	t.Parallel()
	f := seedOwnerFixture(t)
	e, err := f.repo.FindByID(t.Context(), f.ownTypeRowOwnerless)
	if err != nil {
		t.Fatal(err)
	}
	// The concurrent write: the row moves to a later sequence with new data.
	if err := f.repo.db.Table("loans").Where("id = ?", f.ownTypeRowOwnerless).
		Updates(map[string]any{"sequence_no": e.GetSequenceNo() + 1, "name": "renamed"}).Error; err != nil {
		t.Fatal(err)
	}

	got, err := f.repo.FillOwnerlessProjectionRows(t.Context(), e)
	if !errors.Is(err, repositories.ErrProjectionRowChanged) || !strings.Contains(err.Error(), "loans") {
		t.Fatalf("fill = (%v, %v), want ErrProjectionRowChanged naming loans", got, err)
	}
	var row struct {
		Name      string
		AccountID string
	}
	if err := f.repo.db.Table("loans").Select("name, account_id").
		Where("id = ?", f.ownTypeRowOwnerless).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Name != "renamed" || row.AccountID != "" {
		t.Errorf("the row after the refused fill = %+v, want the concurrent write's name and no owner", row)
	}
}
