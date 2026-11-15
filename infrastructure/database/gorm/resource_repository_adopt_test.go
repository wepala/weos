// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package gorm

import (
	"reflect"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"

	authmodels "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/models"
)

// These tests pin the reads and the one write the account adoption makes
// (wm-c7xehr): it selects the resources of an account nobody can use, and
// moves each projection row compare-and-set on the source account.

const (
	adoptFrom  = "acct-orphan"
	adoptTo    = "acct-target"
	adoptThird = "acct-third"
)

type adoptFixture struct {
	repo *ResourceRepository
	// In the source account, every row with it.
	healthy string
	// In the source account, with an ownerless ancestor row (the move's
	// upsert fills it).
	blankRow string
	// In the source account, with no ancestor row at all.
	missingRow string
	// In the source account, with an ancestor row a third account holds.
	foreignRow string
	// Moved: the record is in the target, the ancestor row still in the
	// source (a move whose row write failed).
	partial string
	// Of a third account, and of the target: never selected.
	third, target string
}

func seedAdoptFixture(t *testing.T) adoptFixture {
	t.Helper()
	repo, _, ctx := setupDualProjectionTest(t)
	if err := repo.db.AutoMigrate(&models.ResourceType{}); err != nil {
		t.Fatal(err)
	}
	f := adoptFixture{
		repo: repo, healthy: "urn:loan:healthy", blankRow: "urn:loan:blank-row",
		missingRow: "urn:loan:missing-row", foreignRow: "urn:loan:foreign-row",
		partial: "urn:loan:partial", third: "urn:loan:third", target: "urn:loan:target",
	}
	for _, s := range []struct{ id, account string }{
		{f.healthy, adoptFrom}, {f.blankRow, adoptFrom}, {f.missingRow, adoptFrom},
		{f.foreignRow, adoptFrom}, {f.partial, adoptTo}, {f.third, adoptThird}, {f.target, adoptTo},
	} {
		if err := repo.Save(ctx, makeTestResourceForAgent(t, s.id, "loan", `{"name":"x"}`, "agent-1", s.account)); err != nil {
			t.Fatal(err)
		}
	}
	set := func(id, account string) {
		if err := repo.db.Table("instruments").Where("id = ?", id).Update("account_id", account).Error; err != nil {
			t.Fatal(err)
		}
	}
	set(f.blankRow, "")
	set(f.foreignRow, adoptThird)
	set(f.partial, adoptFrom)
	if err := repo.db.Table("instruments").Where("id = ?", f.missingRow).Delete(map[string]any{}).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func addLoanTypes(t *testing.T, f adoptFixture) {
	t.Helper()
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
}

func TestFindForAdoption_ListsTheSourceAccountAndRowsLeftBehind(t *testing.T) {
	t.Parallel()
	f := seedAdoptFixture(t)
	addLoanTypes(t, f)
	want := []repositories.AccountResource{
		{ID: f.blankRow, TypeSlug: "loan", RecordAccountID: adoptFrom},
		{ID: f.foreignRow, TypeSlug: "loan", RecordAccountID: adoptFrom,
			ForeignRowTable: "instruments", ForeignRowAccountID: adoptThird},
		{ID: f.healthy, TypeSlug: "loan", RecordAccountID: adoptFrom},
		{ID: f.missingRow, TypeSlug: "loan", RecordAccountID: adoptFrom, MissingTable: "instruments"},
		{ID: f.partial, TypeSlug: "loan", RecordAccountID: adoptTo},
	}
	for name, finder := range map[string]repositories.AccountResourceFinder{
		"with a projection manager": f.repo, "without one (the preview)": ProvideAccountResourceFinder(f.repo.db),
	} {
		got, err := finder.FindForAdoption(t.Context(), adoptFrom, adoptTo)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for i := range got {
			if got[i].CreatedAt.IsZero() {
				t.Errorf("%s: %s has no creation time", name, got[i].ID)
			}
			got[i].CreatedAt = want[0].CreatedAt
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: listed\n  %+v\nwant\n  %+v", name, got, want)
		}
	}
}

func TestFindForAdoption_AnAccountWithNoResourcesListsNothing(t *testing.T) {
	t.Parallel()
	f := seedAdoptFixture(t)
	got, err := f.repo.FindForAdoption(t.Context(), "acct-empty", adoptTo)
	if err != nil || len(got) != 0 {
		t.Fatalf("listed %+v (err %v), want nothing", got, err)
	}
}

func rowOwner(t *testing.T, f adoptFixture, table, id string) string {
	t.Helper()
	var owners []*string
	if err := f.repo.db.Table(table).Where("id = ?", id).Pluck("account_id", &owners).Error; err != nil {
		t.Fatal(err)
	}
	if len(owners) == 0 || owners[0] == nil {
		return ""
	}
	return *owners[0]
}

func TestMoveProjectionRowsOwner_MovesOnlyRowsInTheSourceAccount(t *testing.T) {
	t.Parallel()
	f := seedAdoptFixture(t)
	ctx := t.Context()

	written, err := f.repo.MoveProjectionRowsOwner(ctx, f.partial, "loan", adoptFrom, adoptTo)
	if err != nil || !reflect.DeepEqual(written, []string{"instruments"}) {
		t.Fatalf("moved %v (err %v), want [instruments]", written, err)
	}
	for _, table := range []string{"loans", "instruments"} {
		if got := rowOwner(t, f, table, f.partial); got != adoptTo {
			t.Errorf("%s row of the partial move = %q, want %s", table, got, adoptTo)
		}
	}
	// A second run finds nothing in the source account.
	if written, err := f.repo.MoveProjectionRowsOwner(ctx, f.partial, "loan", adoptFrom, adoptTo); err != nil ||
		len(written) != 0 {
		t.Fatalf("a second move wrote %v (err %v), want nothing", written, err)
	}
	// A row of a third account and an ownerless row are not the source's.
	for _, c := range []struct{ id, table, want string }{
		{f.third, "loans", adoptThird}, {f.third, "instruments", adoptThird},
		{f.foreignRow, "instruments", adoptThird}, {f.blankRow, "instruments", ""},
	} {
		if _, err := f.repo.MoveProjectionRowsOwner(ctx, c.id, "loan", adoptFrom, adoptTo); err != nil {
			t.Fatal(err)
		}
		if got := rowOwner(t, f, c.table, c.id); got != c.want {
			t.Errorf("%s row of %s = %q, want %q kept", c.table, c.id, got, c.want)
		}
	}
	// The canonical record is never written.
	var record models.Resource
	if err := f.repo.db.Where("id = ?", f.healthy).First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.MoveProjectionRowsOwner(ctx, f.healthy, "loan", adoptFrom, adoptTo); err != nil {
		t.Fatal(err)
	}
	var after models.Resource
	if err := f.repo.db.Where("id = ?", f.healthy).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.AccountID != adoptFrom {
		t.Errorf("the record moved to %q; the row move must not write it", after.AccountID)
	}
}

func TestCountAccountMembers(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.AutoMigrate(&authmodels.AccountMemberModel{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(authmodels.AccountMemberModelFrom("acct-kept", "agent-1", "owner")).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(authmodels.AccountMemberModelFrom("acct-kept", "agent-2", "member")).Error; err != nil {
		t.Fatal(err)
	}
	counter := ProvideAccountMemberCounter(db)
	for account, want := range map[string]int{"acct-kept": 2, "acct-orphan": 0} {
		got, err := counter.CountAccountMembers(t.Context(), account)
		if err != nil || got != want {
			t.Errorf("%s: %d members (err %v), want %d", account, got, err, want)
		}
	}
}
