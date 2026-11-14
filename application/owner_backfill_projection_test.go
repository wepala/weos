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

package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	esinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
)

// These tests pin wm-mttt4k: the owner backfill writes the owner into the
// canonical record and then into the resource's per-type projection row, and
// the two writes do not share a transaction. A run must not count a resource
// as assigned while its type row is still ownerless, and a later run must be
// able to fill that row.

// backfillRow is one resource as the fake repository stores it: the
// canonical record, and the owner its type's projection row carries.
type backfillRow struct {
	typeSlug, createdBy, accountID string
	seq                            int
	createdAt                      time.Time
	typeRowOwner                   string
}

// backfillRepo is a ResourceRepository with a canonical record and one type
// projection row per resource. Update writes the record first and the type
// row second, as the gorm repository does, and fails the type-row write for
// any id in failTypeRow — the failure the unit of work drops.
type backfillRepo struct {
	repositories.ResourceRepository
	mu          sync.Mutex
	rows        map[string]*backfillRow
	failTypeRow map[string]bool
}

func (r *backfillRepo) FindByID(_ context.Context, id string) (*entities.Resource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[id]
	if !ok {
		return nil, fmt.Errorf("resource %q: %w", id, repositories.ErrNotFound)
	}
	e := &entities.Resource{}
	if err := e.Restore(id, row.typeSlug, "active", json.RawMessage(`{"name":"x"}`),
		row.createdBy, row.accountID, row.createdAt, row.seq); err != nil {
		return nil, err
	}
	return e, nil
}

func (r *backfillRepo) Update(_ context.Context, e *entities.Resource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.rows[e.GetID()]
	row.accountID, row.seq = e.AccountID(), e.GetSequenceNo()
	if r.failTypeRow[e.GetID()] {
		return errors.New("simulated failure of the type projection row write")
	}
	if row.typeRowOwner == "" {
		row.typeRowOwner = e.AccountID()
	}
	return nil
}

func (r *backfillRepo) typeRowOwner(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows[id].typeRowOwner
}

func (r *backfillRepo) recordOwner(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows[id].accountID
}

// FindOwnerlessByCreator lists what the gorm repository lists: every live
// resource of the creator whose record has no owner, and every one whose
// record has an owner while its projection row has none.
func (r *backfillRepo) FindOwnerlessByCreator(
	_ context.Context, createdBy string,
) ([]repositories.OwnerlessResource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []repositories.OwnerlessResource
	for id, row := range r.rows {
		if row.createdBy != createdBy {
			continue
		}
		switch {
		case row.accountID == "":
			out = append(out, repositories.OwnerlessResource{ID: id, TypeSlug: row.typeSlug, CreatedAt: row.createdAt})
		case row.typeRowOwner == "":
			out = append(out, repositories.OwnerlessResource{ID: id, TypeSlug: row.typeSlug,
				CreatedAt: row.createdAt, RecordAccountID: row.accountID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *backfillRepo) OwnerlessProjectionTables(_ context.Context, id, typeSlug string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row, ok := r.rows[id]; ok && row.typeRowOwner == "" {
		return []string{typeSlug + "s"}, nil
	}
	return nil, nil
}

// backfillAccounts holds one account with one member.
type backfillAccounts struct {
	authrepos.AccountRepository
	account *authentities.Account
	member  string
}

func (a backfillAccounts) FindByID(_ context.Context, id string) (*authentities.Account, error) {
	if id == a.account.GetID() {
		return a.account, nil
	}
	return nil, nil
}

func (a backfillAccounts) FindMemberRole(_ context.Context, accountID, agentID string) (string, error) {
	if accountID == a.account.GetID() && agentID == a.member {
		return "owner", nil
	}
	return "", nil
}

func (a backfillAccounts) FindByMember(_ context.Context, agentID string) ([]*authentities.Account, error) {
	if agentID == a.member {
		return []*authentities.Account{a.account}, nil
	}
	return nil, nil
}

type backfillHarness struct {
	svc     *OwnerBackfillService
	repo    *backfillRepo
	store   domain.EventStore
	account string
	agent   string
}

// newBackfillHarness stores a Resource.Created for each id, written by the
// agent with no account, and a record and an ownerless type row at the
// event's sequence — the state the session bug left.
func newBackfillHarness(t *testing.T, ids ...string) *backfillHarness {
	t.Helper()
	const agent, accountID = "urn:agent:owner", "urn:account:owner"
	account, err := (&authentities.Account{}).With(accountID, "Household", authentities.AccountTypePersonal)
	if err != nil {
		t.Fatal(err)
	}
	h := &backfillHarness{
		repo:    &backfillRepo{rows: map[string]*backfillRow{}, failTypeRow: map[string]bool{}},
		store:   esinfra.NewMemoryStore(),
		account: accountID, agent: agent,
	}
	ctx := context.Background()
	for _, id := range ids {
		e, err := (&entities.Resource{}).With(id, "task", json.RawMessage(`{"name":"x"}`), agent, "")
		if err != nil {
			t.Fatal(err)
		}
		uow := esapp.NewSimpleUnitOfWork(h.store, domain.NewEventDispatcher())
		if err := uow.Track(e); err != nil {
			t.Fatal(err)
		}
		if err := uow.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		seq, err := h.store.GetCurrentVersion(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		h.repo.rows[id] = &backfillRow{typeSlug: "task", createdBy: agent, seq: seq, createdAt: time.Now()}
	}
	d := domain.NewEventDispatcher()
	if err := domain.Subscribe(d, "Resource.AccountAssigned",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceAccountAssigned]) error {
			return handleResourceAccountAssigned(ctx, env, h.repo, noopLogger{})
		}); err != nil {
		t.Fatal(err)
	}
	h.svc = ProvideOwnerBackfillService(OwnerBackfillParams{
		Repo: h.repo, Accounts: backfillAccounts{account: account, member: agent},
		EventStore: h.store, Dispatcher: d, Logger: noopLogger{},
	})
	return h
}

func (h *backfillHarness) run(t *testing.T) (AssignOwnerReport, error) {
	t.Helper()
	return h.svc.AssignOwner(context.Background(), AssignOwnerCommand{AccountID: h.account, CreatedBy: h.agent})
}

func (h *backfillHarness) events(t *testing.T, id string) int {
	t.Helper()
	seq, err := h.store.GetCurrentVersion(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

// The record takes the owner and the type row does not: the resource is not
// assigned, and the run says which row and how to repair it.
func TestAssignOwner_ATypeRowThatDidNotTakeTheOwnerIsNotCountedAsAssigned(t *testing.T) {
	const id = "urn:task:reconcile-may"
	h := newBackfillHarness(t, id)
	h.repo.failTypeRow[id] = true

	report, err := h.run(t)
	if err == nil {
		t.Fatalf("a run whose type projection row stayed ownerless returned no error; report %+v", report)
	}
	for _, want := range []string{id, "projection row", "run this command again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	if report.Assigned() != 0 {
		t.Errorf("report counts %d assigned, but the type row has no owner", report.Assigned())
	}
	if got := h.repo.recordOwner(id); got != h.account {
		t.Fatalf("precondition: the record's owner is %q, want %q (only the type row should fail)", got, h.account)
	}
	if got := h.repo.typeRowOwner(id); got != "" {
		t.Fatalf("precondition: the type row took the owner %q", got)
	}
}

// A later run finds the resource whose record has the owner and whose type
// row has none, fills the row from the record, and records no second event:
// the assignment is already in the store.
func TestAssignOwner_ARerunFillsATypeRowTheRecordsOwnerDidNotReach(t *testing.T) {
	const id = "urn:task:reconcile-may"
	h := newBackfillHarness(t, id)
	h.repo.failTypeRow[id] = true
	if _, err := h.run(t); err == nil {
		t.Fatal("the first run did not fail on the ownerless type row")
	}
	stored := h.events(t, id)
	delete(h.repo.failTypeRow, id)

	preview, err := h.svc.AssignOwner(context.Background(), AssignOwnerCommand{
		AccountID: h.account, CreatedBy: h.agent, DryRun: true,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Resources) != 1 || preview.Resources[0].ID != id {
		t.Fatalf("the preview selects %+v; want %s, whose type row is still ownerless", preview.Resources, id)
	}
	if !preview.Resources[0].RecordOwned {
		t.Errorf("the preview does not say %s's record already has the owner", id)
	}

	report, err := h.run(t)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if got := report.Types["task"]; got.Ownerless != 1 || got.Assigned != 1 {
		t.Errorf("task counts = %+v, want 1 ownerless and 1 assigned", got)
	}
	if got := h.repo.typeRowOwner(id); got != h.account {
		t.Errorf("type row owner after the re-run = %q, want %q", got, h.account)
	}
	if after := h.events(t, id); after != stored {
		t.Errorf("the re-run appended %d event(s); the assignment was already stored", after-stored)
	}

	// Once both carry the owner, nothing is left to select.
	again, err := h.run(t)
	if err != nil || len(again.Resources) != 0 {
		t.Errorf("a third run selects %+v (err %v), want nothing", again.Resources, err)
	}
}

// The repair fills a row only from a record whose owner is the account the
// run assigns. A record owned by another account is not this run's to touch.
func TestAssignOwner_ATypeRowIsNotFilledForARecordOwnedByAnotherAccount(t *testing.T) {
	const id = "urn:task:reconcile-may"
	h := newBackfillHarness(t, id)
	h.repo.rows[id].accountID = "urn:account:elsewhere"

	report, err := h.run(t)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Resources) != 0 {
		t.Errorf("the run selects %+v, owned by another account", report.Resources)
	}
	if got := h.repo.typeRowOwner(id); got != "" {
		t.Errorf("the type row took %q", got)
	}
}

// A type row that still does not take the owner on the repair is not counted
// either.
func TestAssignOwner_ARepairThatDoesNotReachTheTypeRowIsNotCounted(t *testing.T) {
	const id = "urn:task:reconcile-may"
	h := newBackfillHarness(t, id)
	h.repo.failTypeRow[id] = true
	if _, err := h.run(t); err == nil {
		t.Fatal("the first run did not fail on the ownerless type row")
	}
	report, err := h.run(t)
	if err == nil || !strings.Contains(err.Error(), id) {
		t.Fatalf("a failed repair returned %v, want an error naming %s", err, id)
	}
	if report.Assigned() != 0 {
		t.Errorf("a failed repair counts %d assigned", report.Assigned())
	}
}
