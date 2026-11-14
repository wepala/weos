package gorm

import (
	"reflect"
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
	}
	for _, s := range []struct{ id, agent, account string }{
		{f.recordOwnerless, "agent-1", ""},
		{f.ancestorRowOwnerless, "agent-1", "acct-1"},
		{f.ownTypeRowOwnerless, "agent-1", "acct-1"},
		{f.healthy, "agent-1", "acct-1"},
		{f.othersAncestor, "agent-2", "acct-1"},
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
	}
	if len(got) != len(want) {
		t.Fatalf("listed %+v, want %v", got, want)
	}
	for i, r := range got {
		owner, ok := want[r.ID]
		if !ok || r.RecordAccountID != owner || r.TypeSlug != "loan" || r.CreatedAt.IsZero() {
			t.Errorf("listed %+v; want record owner %q for it (listed: %v)", r, owner, ok)
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
	if len(got) != 3 || !reflect.DeepEqual(ids(got), ids(withMgr)) {
		t.Errorf("without a projection manager listed %v, with one %v", ids(got), ids(withMgr))
	}
}

func ids(rs []repositories.OwnerlessResource) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID + "@" + r.RecordAccountID
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
