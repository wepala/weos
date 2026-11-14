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
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// This file is the service behind `weos maintenance assign-owner` (wm-4kdhro).
//
// A resource's owning account is set once, by Resource.Created, from the
// writer's active account. While every session ran with an empty active
// account, every resource written stored an empty one, and account-scoped
// reads never match such a resource again. Writing the owner into the
// projection tables by hand does not last: a rebuild replays Resource.Created
// and empties it again. So the owner is given through the event stream, as a
// Resource.AccountAssigned per resource, committed through the same unit of
// work and dispatcher as any other write so every projection sees it.

var (
	// ErrOwnerAccountNotFound means the account to assign does not exist.
	ErrOwnerAccountNotFound = errors.New("assign owner: account not found")
	// ErrOwnerAgentNotMember means the creator is not a member of the account
	// to assign, so the account cannot be the owner of what it wrote.
	ErrOwnerAgentNotMember = errors.New("assign owner: the creator is not a member of the account")
	// ErrOwnerAccountNotConfirmed refuses a real run for a creator who is a
	// member of more than one account, until the command repeats the account
	// in ConfirmAccount. An assignment cannot be undone, and with two
	// accounts to choose from a wrong --account is one typo away.
	ErrOwnerAccountNotConfirmed = errors.New("assign owner: the creator is a member of more than one account " +
		"and the account was not confirmed")
)

// AssignOwnerCommand selects the resources to give an owner: those with no
// owning account that CreatedBy created, optionally only those created inside
// [CreatedFrom, CreatedTo]. AccountID is the owner to give them. DryRun lists
// them and writes nothing.
type AssignOwnerCommand struct {
	AccountID string
	CreatedBy string
	// CreatedFrom and CreatedTo bound the selection by creation time. Both
	// are inclusive; a zero value leaves that end open.
	CreatedFrom time.Time
	CreatedTo   time.Time
	// ConfirmAccount must repeat AccountID for a real run when CreatedBy is a
	// member of more than one account. A dry run does not need it.
	ConfirmAccount string
	DryRun         bool
}

// AssignOwnerResource is one selected resource, as a dry run lists it.
type AssignOwnerResource struct {
	ID        string
	TypeSlug  string
	CreatedAt time.Time
	// RecordOwned says the resource's canonical record already carries the
	// owner and only a projection row does not: an earlier assignment stored
	// its event and the projection write did not reach that row. A run fills
	// the row from the record and records no second event.
	RecordOwned bool
}

// AssignOwnerAccount names an account the creator is a member of.
type AssignOwnerAccount struct {
	ID   string
	Name string
}

// AssignOwnerTypeCount is one resource type's line in the report.
//
// Ownerless and Assigned count assignments, each one Resource.AccountAssigned
// event. ToFill and Filled count fills, which store no event (see
// AssignOwnerResource.RecordOwned). They are kept apart so the assigned count
// matches the events a run stored.
type AssignOwnerTypeCount struct {
	// Ownerless is how many of the creator's resources of this type had no
	// owner in their record when the run started.
	Ownerless int
	// Assigned is how many of them this run gave the owner, one event each.
	// Always 0 on a dry run.
	Assigned int
	// ToFill is how many of the creator's resources of this type had the
	// owner in their record and a projection row with none when the run
	// started.
	ToFill int
	// Filled is how many of them this run filled from the record, with no
	// event. Always 0 on a dry run.
	Filled int
	// Skipped is how many, of either kind, already had an owner by the time
	// the run reached them — a concurrent write — and so were left as they
	// were.
	Skipped int
	// NeedsReproject is how many, of either kind, were left ownerless because
	// their read model is behind the event store or changed during the run.
	// Each is listed in the report's NeedsReproject.
	NeedsReproject int
}

// AssignOwnerSkip names a resource a run left ownerless, and why.
type AssignOwnerSkip struct {
	ID       string
	TypeSlug string
	Reason   string
}

// AssignOwnerElsewhere counts the creator's resources whose record another
// account owns and whose projection row has no owner. A run fills a row only
// from a record its own account owns, so these need a run with AccountID.
type AssignOwnerElsewhere struct {
	AccountID string
	Resources int
}

// AssignOwnerReport says what a run found and did, per resource type.
type AssignOwnerReport struct {
	AccountID string
	// AccountName is the display name of the account to assign, so the
	// operator can see which account a run writes before it writes.
	AccountName string
	CreatedBy   string
	CreatedFrom time.Time
	CreatedTo   time.Time
	DryRun      bool
	// MemberOf lists every account the creator is a member of.
	MemberOf []AssignOwnerAccount
	// Resources lists every selected resource, in type and then id order.
	Resources []AssignOwnerResource
	Types     map[string]AssignOwnerTypeCount
	// NeedsReproject lists the resources the run skipped because their read
	// model is behind the event store. `weos worker reproject` brings the read
	// model up to the store; a later run then assigns what is still ownerless.
	NeedsReproject []AssignOwnerSkip
	// OwnedElsewhere counts, per owning account in id order, the creator's
	// resources (in the window) with a projection row to fill whose record
	// another account owns. The run leaves them; a run with that account as
	// --account fills them.
	OwnedElsewhere []AssignOwnerElsewhere
}

// TypeSlugs returns the report's resource types in name order.
func (r AssignOwnerReport) TypeSlugs() []string {
	slugs := make([]string, 0, len(r.Types))
	for slug := range r.Types {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

// Ownerless is the number of resources found with no owner in their record,
// over every type.
func (r AssignOwnerReport) Ownerless() int {
	return r.sum(func(c AssignOwnerTypeCount) int { return c.Ownerless })
}

// Assigned is the number of resources given the owner, one event each, over
// every type.
func (r AssignOwnerReport) Assigned() int {
	return r.sum(func(c AssignOwnerTypeCount) int { return c.Assigned })
}

// ToFill is the number of resources found with the owner in their record and
// a projection row with none, over every type.
func (r AssignOwnerReport) ToFill() int {
	return r.sum(func(c AssignOwnerTypeCount) int { return c.ToFill })
}

// Filled is the number of resources whose projection rows were filled from
// the record, with no event, over every type.
func (r AssignOwnerReport) Filled() int {
	return r.sum(func(c AssignOwnerTypeCount) int { return c.Filled })
}

func (r AssignOwnerReport) sum(count func(AssignOwnerTypeCount) int) int {
	n := 0
	for _, c := range r.Types {
		n += count(c)
	}
	return n
}

// OwnerBackfillParams bundles the service's dependencies.
type OwnerBackfillParams struct {
	fx.In
	Repo       repositories.ResourceRepository
	Accounts   authrepos.AccountRepository
	EventStore domain.EventStore
	Dispatcher *domain.EventDispatcher
	Logger     entities.Logger
}

// OwnerBackfillService gives ownerless resources their owning account.
type OwnerBackfillService struct {
	repo       repositories.ResourceRepository
	finder     repositories.OwnerlessResourceFinder
	projOwners repositories.ProjectionOwnerReader
	projFiller repositories.ProjectionOwnerFiller
	accounts   authrepos.AccountRepository
	eventStore domain.EventStore
	dispatcher *domain.EventDispatcher
	logger     entities.Logger
}

// ProvideOwnerBackfillService wires the service from the container.
func ProvideOwnerBackfillService(p OwnerBackfillParams) *OwnerBackfillService {
	finder, _ := p.Repo.(repositories.OwnerlessResourceFinder)
	projOwners, _ := p.Repo.(repositories.ProjectionOwnerReader)
	projFiller, _ := p.Repo.(repositories.ProjectionOwnerFiller)
	return &OwnerBackfillService{
		repo:       p.Repo,
		finder:     finder,
		projOwners: projOwners,
		projFiller: projFiller,
		accounts:   p.Accounts,
		eventStore: p.EventStore,
		dispatcher: p.Dispatcher,
		logger:     p.Logger,
	}
}

// ErrOwnerBackfillReadOnly refuses a write from a service built by
// NewOwnerBackfillPlanner, which can only plan.
var ErrOwnerBackfillReadOnly = errors.New("assign owner: this service only plans; it was built without an event store")

// NewOwnerBackfillPlanner returns a service that can plan an assignment and
// never write one. It needs only the two lookups a plan reads, so a dry run
// can build it over a read-only database connection instead of starting the
// application, whose start runs migrations and installs missing built-in
// presets. ApplyAssignOwner on it returns ErrOwnerBackfillReadOnly.
func NewOwnerBackfillPlanner(
	finder repositories.OwnerlessResourceFinder, accounts authrepos.AccountRepository,
) *OwnerBackfillService {
	return &OwnerBackfillService{finder: finder, accounts: accounts}
}

// AssignOwner gives every resource with no owning account that cmd.CreatedBy
// created (inside the creation window, when one is given) the owner
// cmd.AccountID, one Resource.AccountAssigned per resource. A dry run only
// plans: see PlanAssignOwner.
func (s *OwnerBackfillService) AssignOwner(
	ctx context.Context, cmd AssignOwnerCommand,
) (AssignOwnerReport, error) {
	report, err := s.PlanAssignOwner(ctx, cmd)
	if err != nil || cmd.DryRun {
		return report, err
	}
	return s.ApplyAssignOwner(ctx, report)
}

// PlanAssignOwner checks a command and selects what it would assign, and
// writes nothing. The report names the account and lists every selected
// resource with its type and creation time.
//
// It refuses unless the account exists and the creator is a member of it. For
// a real run (cmd.DryRun false) it also refuses a creator who is a member of
// more than one account unless cmd.ConfirmAccount repeats the account. A dry
// run is allowed either way, and its report lists the creator's accounts.
func (s *OwnerBackfillService) PlanAssignOwner(
	ctx context.Context, cmd AssignOwnerCommand,
) (AssignOwnerReport, error) {
	report := AssignOwnerReport{
		AccountID: cmd.AccountID, CreatedBy: cmd.CreatedBy, DryRun: cmd.DryRun,
		CreatedFrom: cmd.CreatedFrom, CreatedTo: cmd.CreatedTo,
		Types: map[string]AssignOwnerTypeCount{},
	}
	if cmd.AccountID == "" {
		return report, errors.New("assign owner: no account id given")
	}
	if cmd.CreatedBy == "" {
		return report, errors.New("assign owner: no creator agent id given")
	}
	if !cmd.CreatedFrom.IsZero() && !cmd.CreatedTo.IsZero() && cmd.CreatedTo.Before(cmd.CreatedFrom) {
		return report, fmt.Errorf("assign owner: the creation window ends (%s) before it starts (%s)",
			cmd.CreatedTo.Format(time.RFC3339), cmd.CreatedFrom.Format(time.RFC3339))
	}
	name, err := s.checkMembership(ctx, cmd.AccountID, cmd.CreatedBy)
	if err != nil {
		return report, err
	}
	report.AccountName = name
	if report.MemberOf, err = s.memberAccounts(ctx, cmd.CreatedBy); err != nil {
		return report, err
	}
	if !cmd.DryRun && len(report.MemberOf) > 1 && cmd.ConfirmAccount != cmd.AccountID {
		return report, fmt.Errorf("%w: %s is a member of %d accounts (%s)", ErrOwnerAccountNotConfirmed,
			cmd.CreatedBy, len(report.MemberOf), describeAccounts(report.MemberOf))
	}
	if s.finder == nil {
		return report, fmt.Errorf("assign owner: the resource repository %T cannot list ownerless resources", s.repo)
	}
	ownerless, err := s.finder.FindOwnerlessByCreator(ctx, cmd.CreatedBy)
	if err != nil {
		return report, fmt.Errorf("assign owner: %w", err)
	}
	// The window is applied here rather than in SQL: each dialect stores a
	// time differently, and the creator's ownerless rows are few.
	elsewhere := map[string]int{}
	for _, r := range ownerless {
		if !inWindow(r.CreatedAt, cmd.CreatedFrom, cmd.CreatedTo) {
			continue
		}
		// A record that has an owner with a projection row that has none is
		// selected only when the record's owner is this run's account: the
		// run then finishes its own assignment. A record owned by another
		// account is not this run's to touch, but it is counted, so the
		// report can name the account a second run needs.
		if r.RecordAccountID != "" && r.RecordAccountID != cmd.AccountID {
			elsewhere[r.RecordAccountID]++
			continue
		}
		recordOwned := r.RecordAccountID != ""
		report.Resources = append(report.Resources, AssignOwnerResource{
			ID: r.ID, TypeSlug: r.TypeSlug, CreatedAt: r.CreatedAt, RecordOwned: recordOwned,
		})
		c := report.Types[r.TypeSlug]
		if recordOwned {
			c.ToFill++
		} else {
			c.Ownerless++
		}
		report.Types[r.TypeSlug] = c
	}
	for account, n := range elsewhere {
		report.OwnedElsewhere = append(report.OwnedElsewhere, AssignOwnerElsewhere{AccountID: account, Resources: n})
	}
	sort.Slice(report.OwnedElsewhere, func(i, j int) bool {
		return report.OwnedElsewhere[i].AccountID < report.OwnedElsewhere[j].AccountID
	})
	return report, nil
}

// ApplyAssignOwner assigns what a plan from PlanAssignOwner selected. A
// resource that has an owner is never touched. Each resource commits on its
// own, so a run that stops part-way has assigned what it reports, and running
// it again finishes the rest: a resource already assigned is no longer
// ownerless and is not selected again.
//
// A resource is counted as assigned only once its record and every one of its
// projection rows are read back with the owner. A resource whose read model is
// behind its event stream — found before the write, or met as a version
// conflict on it — is skipped, listed in the report's NeedsReproject, and the
// run goes on. A write whose event was stored but whose record or projection
// row did not take the owner stops the run with an error that names the
// resource: the read model is failing, and every later write would fail the
// same way.
//
// A resource whose record has the owner and a projection row has none (see
// AssignOwnerResource.RecordOwned) is not given a second event. Its record is
// written to each ownerless projection row, and it is counted once the rows
// read back with the owner. A row that changed after the record was read is
// not written; the resource is listed in NeedsReproject.
func (s *OwnerBackfillService) ApplyAssignOwner(
	ctx context.Context, plan AssignOwnerReport,
) (AssignOwnerReport, error) {
	if s.eventStore == nil || s.dispatcher == nil {
		return plan, ErrOwnerBackfillReadOnly
	}
	if s.projOwners == nil {
		return plan, fmt.Errorf("assign owner: the resource repository %T cannot read the owner of its "+
			"projection rows, so an assignment could not be checked", s.repo)
	}
	if s.projFiller == nil {
		return plan, fmt.Errorf("assign owner: the resource repository %T cannot fill the owner of its "+
			"projection rows", s.repo)
	}
	report := plan
	report.DryRun = false
	report.Types = make(map[string]AssignOwnerTypeCount, len(plan.Types))
	for slug, c := range plan.Types {
		report.Types[slug] = AssignOwnerTypeCount{Ownerless: c.Ownerless, ToFill: c.ToFill}
	}
	report.NeedsReproject = nil
	for _, r := range plan.Resources {
		assign := s.assignOne
		if r.RecordOwned {
			assign = s.fillProjectionRows
		}
		outcome, reason, err := assign(ctx, r, plan.AccountID, plan.CreatedBy)
		if err != nil {
			s.logger.Error(ctx, "assign owner stopped", "id", r.ID, "type", r.TypeSlug, "error", err)
			return report, fmt.Errorf("assign owner: %s (%s): %w", r.ID, r.TypeSlug, err)
		}
		c := report.Types[r.TypeSlug]
		switch {
		case outcome == assignedOwner && r.RecordOwned:
			c.Filled++
		case outcome == assignedOwner:
			c.Assigned++
		case outcome == gainedOwner:
			c.Skipped++
		case outcome == readModelBehind:
			c.NeedsReproject++
			report.NeedsReproject = append(report.NeedsReproject,
				AssignOwnerSkip{ID: r.ID, TypeSlug: r.TypeSlug, Reason: reason})
			s.logger.Warn(ctx, "assign owner skipped a resource whose read model is behind the event store",
				"id", r.ID, "type", r.TypeSlug, "reason", reason)
		}
		report.Types[r.TypeSlug] = c
	}
	s.logger.Info(ctx, "assigned owners to ownerless resources",
		"account", plan.AccountID, "createdBy", plan.CreatedBy, "assigned", report.Assigned(),
		"filled", report.Filled(), "needsReproject", len(report.NeedsReproject))
	return report, nil
}

// inWindow reports whether t lies in [from, to]; a zero bound is open.
func inWindow(t, from, to time.Time) bool {
	if !from.IsZero() && t.Before(from) {
		return false
	}
	if !to.IsZero() && t.After(to) {
		return false
	}
	return true
}

func describeAccounts(accounts []AssignOwnerAccount) string {
	parts := make([]string, len(accounts))
	for i, a := range accounts {
		parts[i] = fmt.Sprintf("%s (%s)", a.Name, a.ID)
	}
	return strings.Join(parts, ", ")
}

// memberAccounts lists every account the agent is a member of, in id order.
func (s *OwnerBackfillService) memberAccounts(ctx context.Context, agentID string) ([]AssignOwnerAccount, error) {
	accounts, err := s.accounts.FindByMember(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("assign owner: list the accounts of %s: %w", agentID, err)
	}
	out := make([]AssignOwnerAccount, 0, len(accounts))
	for _, a := range accounts {
		if a == nil {
			continue
		}
		out = append(out, AssignOwnerAccount{ID: a.GetID(), Name: a.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// checkMembership refuses an account that does not exist or that the agent is
// not a member of, and returns the account's name.
func (s *OwnerBackfillService) checkMembership(ctx context.Context, accountID, agentID string) (string, error) {
	account, err := s.accounts.FindByID(ctx, accountID)
	if err != nil {
		return "", fmt.Errorf("assign owner: load account %s: %w", accountID, err)
	}
	if account == nil {
		return "", fmt.Errorf("%w: %s", ErrOwnerAccountNotFound, accountID)
	}
	role, err := s.accounts.FindMemberRole(ctx, accountID, agentID)
	if err != nil {
		return "", fmt.Errorf("assign owner: read the membership of %s in %s: %w", agentID, accountID, err)
	}
	if role == "" {
		return "", fmt.Errorf("%w: %s is not a member of %s", ErrOwnerAgentNotMember, agentID, accountID)
	}
	return account.Name(), nil
}

// assignOutcome is what assignOne did with one resource.
type assignOutcome int

const (
	// assignedOwner: the event is stored and the record reads back the owner.
	assignedOwner assignOutcome = iota
	// gainedOwner: the resource gained an owner (or changed creator) since
	// the selection, so it was left alone.
	gainedOwner
	// readModelBehind: the record is behind the event store, so an append
	// from it would conflict. Nothing was written.
	readModelBehind
)

// errReadModelDidNotTakeOwner is the read-back failure after a stored
// assignment: the event is in the store and the record is still ownerless.
var errReadModelDidNotTakeOwner = errors.New("the Resource.AccountAssigned event is stored, " +
	"but the resource's record did not take the owner (the projection write failed). " +
	"Stop the instance and run `weos worker reproject` to apply the stored event, then run this command again")

// errProjectionRowDidNotTakeOwner is the read-back failure when the record
// has the owner and a projection row of the resource does not. The record and
// the rows are written one after the other, so the record alone does not show
// it. The record holds the owner, so a later run can fill the row from it.
var errProjectionRowDidNotTakeOwner = errors.New("the resource's record has the owner, " +
	"but its projection row did not take it (the projection write failed). " +
	"Fix the cause, then run this command again: it fills the row from the record. " +
	"`weos worker reproject` on the stopped instance repairs it as well")

// assignOne gives one resource its owner. It reads the resource again first,
// so a resource that gained an owner (or changed creator) since the selection
// is left alone and reported as skipped rather than refused.
//
// The record's version is compared with the event store's before the write.
// A record behind the store would make the append conflict, and every run
// after it would conflict on the same record, so it is reported as behind
// instead, with what the store holds past it.
func (s *OwnerBackfillService) assignOne(
	ctx context.Context, r AssignOwnerResource, accountID, createdBy string,
) (assignOutcome, string, error) {
	id := r.ID
	entity, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return 0, "", err
	}
	if entity.AccountID() != "" || entity.CreatedBy() != createdBy {
		return gainedOwner, "", nil
	}
	recordSeq := entity.GetSequenceNo()
	if reason, behind, err := s.readModelBehind(ctx, id, recordSeq); err != nil {
		return 0, "", err
	} else if behind {
		return readModelBehind, reason, nil
	}
	if err := entity.AssignAccount(accountID); err != nil {
		return 0, "", err
	}
	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return 0, "", fmt.Errorf("track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		if errors.Is(err, domain.ErrConcurrencyConflict) {
			return readModelBehind, fmt.Sprintf("version conflict on the append from sequence %d: %v",
				recordSeq, err), nil
		}
		return 0, "", fmt.Errorf("commit the assignment: %w", err)
	}
	// The unit of work stores the event and then dispatches it to the
	// projection, and it drops the projection's error. Only a read-back
	// shows whether the record took the owner.
	after, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return 0, "", fmt.Errorf("read the record back: %w (%w)", err, errReadModelDidNotTakeOwner)
	}
	if after.AccountID() != accountID {
		return 0, "", errReadModelDidNotTakeOwner
	}
	if err := s.readBackProjectionRows(ctx, id, after.TypeSlug()); err != nil {
		return 0, "", err
	}
	return assignedOwner, "", nil
}

// fillProjectionRows finishes an assignment whose event is stored and whose
// record has the owner, but whose projection row does not. It writes the
// record over each ownerless projection row and records no event: the owner
// is already in the stream, so a rebuild keeps it, and the background workers
// saw the stored event. The canonical record is not written.
//
// A record behind its event stream is left for a reproject, as in assignOne:
// writing it would put a stale record back over its rows. The fill runs
// outside a unit of work, so it cannot rely on a version conflict the way
// assignOne does; the repository writes each row only while that row has no
// owner and is not past the record's sequence, checked in the write statement
// itself. A row that fails that check changed after the record was read — the
// instance is serving — and the resource is left for a reproject too.
func (s *OwnerBackfillService) fillProjectionRows(
	ctx context.Context, r AssignOwnerResource, accountID, createdBy string,
) (assignOutcome, string, error) {
	entity, err := s.repo.FindByID(ctx, r.ID)
	if err != nil {
		return 0, "", err
	}
	if entity.AccountID() != accountID || entity.CreatedBy() != createdBy {
		return gainedOwner, "", nil
	}
	if reason, behind, err := s.readModelBehind(ctx, r.ID, entity.GetSequenceNo()); err != nil {
		return 0, "", err
	} else if behind {
		return readModelBehind, reason, nil
	}
	tables, err := s.projOwners.OwnerlessProjectionTables(ctx, r.ID, entity.TypeSlug())
	if err != nil {
		return 0, "", fmt.Errorf("read the owner of the projection rows: %w", err)
	}
	if len(tables) == 0 {
		// Filled since the selection, by a reproject or a concurrent write.
		return gainedOwner, "", nil
	}
	if _, err := s.projFiller.FillOwnerlessProjectionRows(ctx, entity); err != nil {
		if errors.Is(err, repositories.ErrProjectionRowChanged) {
			return readModelBehind, fmt.Sprintf("a projection row changed after the run read the record "+
				"at sequence %d, so the run did not write over it (%v)", entity.GetSequenceNo(), err), nil
		}
		return 0, "", fmt.Errorf("write the record's owner to %s: %w (%w)",
			strings.Join(tables, ", "), err, errProjectionRowDidNotTakeOwner)
	}
	if err := s.readBackProjectionRows(ctx, r.ID, entity.TypeSlug()); err != nil {
		return 0, "", err
	}
	return assignedOwner, "", nil
}

// readBackProjectionRows fails when a projection row of the resource has no
// owner, and names the tables that hold such a row.
func (s *OwnerBackfillService) readBackProjectionRows(ctx context.Context, id, typeSlug string) error {
	tables, err := s.projOwners.OwnerlessProjectionTables(ctx, id, typeSlug)
	if err != nil {
		return fmt.Errorf("read the projection rows back: %w (%w)", err, errProjectionRowDidNotTakeOwner)
	}
	if len(tables) > 0 {
		return fmt.Errorf("ownerless in %s: %w", strings.Join(tables, ", "), errProjectionRowDidNotTakeOwner)
	}
	return nil
}

// readModelBehind reports whether the event store holds events for id past
// the record's sequence number, and if so, what they are.
func (s *OwnerBackfillService) readModelBehind(
	ctx context.Context, id string, recordSeq int,
) (string, bool, error) {
	storeSeq, err := s.eventStore.GetCurrentVersion(ctx, id)
	if err != nil {
		return "", false, fmt.Errorf("read the event store version: %w", err)
	}
	if storeSeq <= recordSeq {
		return "", false, nil
	}
	ahead, err := s.eventStore.GetEventsRange(ctx, id, recordSeq+1, -1)
	if err != nil {
		return "", false, fmt.Errorf("read the events past the record: %w", err)
	}
	for _, env := range ahead {
		if env.EventType == (entities.ResourceAccountAssigned{}).EventType() {
			return fmt.Sprintf("the event store already holds a Resource.AccountAssigned at sequence %d "+
				"that the record (at sequence %d) has not applied", env.SequenceNo, recordSeq), true, nil
		}
	}
	return fmt.Sprintf("the record is at sequence %d and the event store at %d", recordSeq, storeSeq), true, nil
}
