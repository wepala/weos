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
	// typeRowSeq is the sequence number the type row was last written at.
	typeRowSeq int
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
	// beforeFill runs at the start of FillOwnerlessProjectionRows, before
	// the fill reads the row: the moment a concurrent write would land.
	beforeFill func(id string)
	// updates and fills count the calls to Update and to
	// FillOwnerlessProjectionRows.
	updates, fills int
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
	r.updates++
	row := r.rows[e.GetID()]
	row.accountID, row.seq = e.AccountID(), e.GetSequenceNo()
	if r.failTypeRow[e.GetID()] {
		return errors.New("simulated failure of the type projection row write")
	}
	if row.typeRowOwner == "" {
		row.typeRowOwner = e.AccountID()
	}
	row.typeRowSeq = e.GetSequenceNo()
	return nil
}

// FillOwnerlessProjectionRows writes the record to the type row when the row
// is still ownerless and not past the record's sequence, as the gorm
// repository's guarded write does. The record itself is not written.
func (r *backfillRepo) FillOwnerlessProjectionRows(_ context.Context, e *entities.Resource) ([]string, error) {
	if r.beforeFill != nil {
		r.beforeFill(e.GetID())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fills++
	row := r.rows[e.GetID()]
	if row.typeRowOwner != "" {
		return nil, nil
	}
	table := row.typeSlug + "s"
	if row.typeRowSeq > e.GetSequenceNo() {
		return nil, fmt.Errorf("%s in %s: %w", e.GetID(), table, repositories.ErrProjectionRowChanged)
	}
	if r.failTypeRow[e.GetID()] {
		return nil, errors.New("simulated failure of the type projection row write")
	}
	row.typeRowOwner, row.typeRowSeq = e.AccountID(), e.GetSequenceNo()
	return []string{table}, nil
}

func (r *backfillRepo) calls() (updates, fills int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.updates, r.fills
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
		h.repo.rows[id] = &backfillRow{typeSlug: "task", createdBy: agent, seq: seq, createdAt: time.Now(),
			typeRowSeq: seq}
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
	if got := preview.Types["task"]; got.Ownerless != 0 || got.ToFill != 1 {
		t.Errorf("preview task counts = %+v, want 1 row to fill and nothing ownerless", got)
	}
	if !preview.Resources[0].RecordOwned {
		t.Errorf("the preview does not say %s's record already has the owner", id)
	}

	report, err := h.run(t)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	// A fill stores no event, so it is counted apart from the assignments
	// (wm-y49sxb): the assigned count must match the events the run stored.
	if got := report.Types["task"]; got.Ownerless != 0 || got.Assigned != 0 || got.ToFill != 1 || got.Filled != 1 {
		t.Errorf("task counts = %+v, want 1 row to fill and 1 filled, and nothing ownerless or assigned", got)
	}
	if report.Ownerless() != 0 || report.Assigned() != 0 || report.ToFill() != 1 || report.Filled() != 1 {
		t.Errorf("totals = %d ownerless, %d assigned, %d to fill, %d filled; want 0, 0, 1, 1",
			report.Ownerless(), report.Assigned(), report.ToFill(), report.Filled())
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

// failFirstRun runs the backfill once with id's type-row write failing, so
// the record has the owner and the type row has none, then lets writes
// through again.
func (h *backfillHarness) failFirstRun(t *testing.T, id string) {
	t.Helper()
	h.repo.failTypeRow[id] = true
	if _, err := h.run(t); err == nil {
		t.Fatal("the first run did not fail on the ownerless type row")
	}
	delete(h.repo.failTypeRow, id)
}

// appendPastTheRecord stores an event for id that the record has not
// applied, so the record is behind its event stream.
func (h *backfillHarness) appendPastTheRecord(t *testing.T, id string) {
	t.Helper()
	e, err := h.repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(json.RawMessage(`{"name":"renamed"}`)); err != nil {
		t.Fatal(err)
	}
	uow := esapp.NewSimpleUnitOfWork(h.store, domain.NewEventDispatcher())
	if err := uow.Track(e); err != nil {
		t.Fatal(err)
	}
	if err := uow.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// wm-5o1kcm: a fill writes the record over its rows, so a record that is
// behind its event stream would put stale data back. It is left for a
// reproject, and nothing is written.
func TestAssignOwner_AFillLeavesARecordBehindItsStreamForAReproject(t *testing.T) {
	const id = "urn:task:reconcile-may"
	h := newBackfillHarness(t, id)
	h.failFirstRun(t, id)
	h.appendPastTheRecord(t, id)
	updates, fills := h.repo.calls()

	report, err := h.run(t)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.NeedsReproject) != 1 || report.NeedsReproject[0].ID != id {
		t.Fatalf("NeedsReproject = %+v, want %s", report.NeedsReproject, id)
	}
	if u, f := h.repo.calls(); u != updates || f != fills {
		t.Errorf("the run wrote a record behind its stream: %d update(s), %d fill(s)", u-updates, f-fills)
	}
	if got := h.repo.typeRowOwner(id); got != "" {
		t.Errorf("the type row took %q from a stale record", got)
	}
}

// wm-5o1kcm: a row that a reproject or another write filled between the plan
// and the run is left alone, and not counted as filled by this run.
func TestAssignOwner_AFillSkipsARowFilledBetweenThePlanAndTheRun(t *testing.T) {
	const id = "urn:task:reconcile-may"
	h := newBackfillHarness(t, id)
	h.failFirstRun(t, id)
	ctx := context.Background()
	plan, err := h.svc.PlanAssignOwner(ctx, AssignOwnerCommand{AccountID: h.account, CreatedBy: h.agent})
	if err != nil || len(plan.Resources) != 1 || !plan.Resources[0].RecordOwned {
		t.Fatalf("plan = %+v (err %v), want %s as a row to fill", plan.Resources, err, id)
	}
	h.repo.mu.Lock()
	h.repo.rows[id].typeRowOwner = h.account
	h.repo.mu.Unlock()
	_, fills := h.repo.calls()

	report, err := h.svc.ApplyAssignOwner(ctx, plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := report.Types["task"]; got.Skipped != 1 {
		t.Errorf("task counts = %+v, want the filled row skipped", got)
	}
	if _, f := h.repo.calls(); f != fills {
		t.Errorf("the run wrote a row that already had its owner (%d fill call(s))", f-fills)
	}
	if len(report.NeedsReproject) != 0 {
		t.Errorf("NeedsReproject = %+v, want none", report.NeedsReproject)
	}
}

// wm-5o1kcm: a write that lands between the fill's read of the record and its
// write of the row moves the row past the record. The fill must not write the
// stale record over it: it skips the resource and lists it, and the user's
// write stands.
func TestAssignOwner_AFillDoesNotWriteARowThatChangedSinceTheRecordWasRead(t *testing.T) {
	const id = "urn:task:reconcile-may"
	h := newBackfillHarness(t, id)
	h.failFirstRun(t, id)
	h.repo.beforeFill = func(id string) {
		h.repo.mu.Lock()
		defer h.repo.mu.Unlock()
		h.repo.rows[id].typeRowSeq = h.repo.rows[id].seq + 1
	}
	stored := h.events(t, id)

	report, err := h.run(t)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.NeedsReproject) != 1 || report.NeedsReproject[0].ID != id ||
		!strings.Contains(report.NeedsReproject[0].Reason, "changed") {
		t.Fatalf("NeedsReproject = %+v, want %s with a reason that says the row changed", report.NeedsReproject, id)
	}
	if report.Assigned() != 0 {
		t.Errorf("a skipped fill counts %d", report.Assigned())
	}
	if got := h.repo.typeRowOwner(id); got != "" {
		t.Errorf("the fill wrote the stale record over a row that changed: owner %q", got)
	}
	if u, _ := h.repo.calls(); u != 1 {
		t.Errorf("Update was called %d time(s), want only the first run's: the fill must not rewrite the record", u)
	}
	if after := h.events(t, id); after != stored {
		t.Errorf("the run appended %d event(s)", after-stored)
	}
}
