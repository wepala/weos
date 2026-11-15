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
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// This file is the service behind `weos maintenance adopt-account`
// (wm-c7xehr).
//
// An account with no account_members row cannot be signed in to by anyone,
// so every resource it owns is out of reach of every account-scoped path:
// listings never match it, and checkInstanceAccess refuses every caller but
// the resource's creator. The service moves each such resource to a named
// account through the event stream, one Resource.AccountAdopted per
// resource, committed through the same unit of work and dispatcher as any
// other write so every read model follows it. It is the one place an owner
// moves; assign-owner still only fills an empty owner.

var (
	// ErrAdoptSourceNotFound means the account to move from does not exist.
	ErrAdoptSourceNotFound = errors.New("adopt account: the account to move from does not exist")
	// ErrAdoptSourceHasMembers refuses an account someone can still use.
	ErrAdoptSourceHasMembers = errors.New("adopt account: the account to move from has members")
	// ErrAdoptSourceHasInvites refuses an account with a pending, unexpired
	// invitation: accepting it makes a member of an account the move would
	// have emptied (wm-m0ci05).
	ErrAdoptSourceHasInvites = errors.New("adopt account: the account to move from has a pending invitation")
	// ErrAdoptTargetNotFound means the account to move to does not exist.
	ErrAdoptTargetNotFound = errors.New("adopt account: the account to move to does not exist")
	// ErrAdoptSameAccount refuses a move into the account it comes from.
	ErrAdoptSameAccount = errors.New("adopt account: the source and the target are the same account")
	// ErrAdoptAgentNotAdmin refuses an agent that is not an owner or an
	// admin of the target account: the move hands the target everything the
	// source owns.
	ErrAdoptAgentNotAdmin = errors.New("adopt account: the agent is not an owner or admin of the target account")
	// ErrAdoptReadOnly refuses a write from a service built by
	// NewAccountAdoptionPlanner.
	ErrAdoptReadOnly = errors.New("adopt account: this service only plans; it was built without an event store")
)

// AdoptAccountCommand moves every resource of FromAccountID, an account with
// no members, to AccountID. AgentID must be an owner or admin of AccountID.
// DryRun plans and writes nothing.
type AdoptAccountCommand struct {
	FromAccountID string
	AccountID     string
	AgentID       string
	DryRun        bool
}

// AdoptAccountResource is one selected resource.
type AdoptAccountResource struct {
	ID        string
	TypeSlug  string
	CreatedAt time.Time
	// RecordMoved says the resource's record is already in the target and
	// only a projection row is still in the source: an earlier move stored
	// its event and the projection write did not reach that row. A run moves
	// the row and records no second event.
	RecordMoved bool
	// MissingTable names a projection table with no row for the resource.
	// The move writes it; the read-back checks that it did.
	MissingTable string
}

// AdoptAccountTypeCount is one resource type's line in the report. ToMove and
// Moved count resources, each one Resource.AccountAdopted. RowsToMove and
// RowsMoved count rows a failed projection write left behind, moved with no
// event. They are kept apart so the moved count matches the events a run
// stored.
type AdoptAccountTypeCount struct {
	ToMove         int
	Moved          int
	RowsToMove     int
	RowsMoved      int
	Skipped        int
	NeedsReproject int
	ForeignRows    int
	// Granted counts the read grants the run gave the agent on resources of
	// this type. A resource on which the agent already held read is not
	// counted, so a re-run grants nothing twice.
	Granted int
}

// AdoptAccountProgress is reported after each resource a real run handles.
// Done of Total counts every selected resource; TypeDone of TypeTotal counts
// those of TypeSlug.
type AdoptAccountProgress struct {
	Done      int
	Total     int
	TypeSlug  string
	TypeDone  int
	TypeTotal int
}

// AdoptAccountReport says what a run found and did, per resource type.
type AdoptAccountReport struct {
	FromAccountID   string
	FromAccountName string
	// FromMembers is the source account's member count, which the run
	// requires to be 0.
	FromMembers int
	// FromInvites is the source account's count of pending, unexpired
	// invitations, which the run requires to be 0.
	FromInvites int
	// FromCreators lists each agent that created a live resource of the
	// source account. An agent keeps access to what it created with no
	// membership, so one that still has a credential, or wrote recently, is a
	// sign the account is still in use. The run does not refuse on it; the
	// operator reads it in the preview.
	FromCreators []repositories.AccountCreator
	AccountID    string
	AccountName  string
	AgentID      string
	// AgentRole is the agent's role in the target account.
	AgentRole string
	DryRun    bool
	// Resources lists every selected resource, in type and then id order.
	Resources []AdoptAccountResource
	Types     map[string]AdoptAccountTypeCount
	// NeedsReproject lists the resources whose read model is behind the event
	// store, or that the move left without a projection row. A resource listed
	// after its read-back already has its event stored.
	NeedsReproject []AssignOwnerSkip
	// ForeignRows lists the resources of the source account with a projection
	// row a third account holds. They are not moved: no write this command
	// makes repairs that row, so moving the record would only hide the
	// mismatch.
	ForeignRows []AssignOwnerForeignRow
}

// TypeSlugs returns the report's resource types in name order.
func (r AdoptAccountReport) TypeSlugs() []string {
	slugs := make([]string, 0, len(r.Types))
	for slug := range r.Types {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

// ToMove is how many resources were in the source account, over every type.
func (r AdoptAccountReport) ToMove() int {
	return r.sum(func(c AdoptAccountTypeCount) int { return c.ToMove })
}

// Moved is how many resources this run moved, one event each.
func (r AdoptAccountReport) Moved() int {
	return r.sum(func(c AdoptAccountTypeCount) int { return c.Moved })
}

// RowsToMove is how many resources had a record in the target and a row in
// the source.
func (r AdoptAccountReport) RowsToMove() int {
	return r.sum(func(c AdoptAccountTypeCount) int { return c.RowsToMove })
}

// Granted is how many read grants this run gave the agent.
func (r AdoptAccountReport) Granted() int {
	return r.sum(func(c AdoptAccountTypeCount) int { return c.Granted })
}

// RowsMoved is how many of those this run finished, with no event.
func (r AdoptAccountReport) RowsMoved() int {
	return r.sum(func(c AdoptAccountTypeCount) int { return c.RowsMoved })
}

func (r AdoptAccountReport) sum(count func(AdoptAccountTypeCount) int) int {
	n := 0
	for _, c := range r.Types {
		n += count(c)
	}
	return n
}

// AccountAdoptionParams bundles the service's dependencies.
type AccountAdoptionParams struct {
	fx.In
	Repo        repositories.ResourceRepository
	Accounts    authrepos.AccountRepository
	Members     repositories.AccountUsageReader
	Permissions ResourcePermissionService
	EventStore  domain.EventStore
	Dispatcher  *domain.EventDispatcher
	Logger      entities.Logger
}

// AccountAdoptionService moves the resources of an account nobody can use.
type AccountAdoptionService struct {
	repo        repositories.ResourceRepository
	finder      repositories.AccountResourceFinder
	projOwners  repositories.ProjectionOwnerReader
	mover       repositories.ProjectionOwnerMover
	accounts    authrepos.AccountRepository
	members     repositories.AccountUsageReader
	permissions ResourcePermissionService
	eventStore  domain.EventStore
	dispatcher  *domain.EventDispatcher
	logger      entities.Logger
}

// ProvideAccountAdoptionService wires the service from the container.
func ProvideAccountAdoptionService(p AccountAdoptionParams) *AccountAdoptionService {
	finder, _ := p.Repo.(repositories.AccountResourceFinder)
	projOwners, _ := p.Repo.(repositories.ProjectionOwnerReader)
	mover, _ := p.Repo.(repositories.ProjectionOwnerMover)
	return &AccountAdoptionService{
		repo: p.Repo, finder: finder, projOwners: projOwners, mover: mover,
		accounts: p.Accounts, members: p.Members, permissions: p.Permissions,
		eventStore: p.EventStore, dispatcher: p.Dispatcher, logger: p.Logger,
	}
}

// NewAccountAdoptionPlanner returns a service that can plan a move and never
// write one, over the lookups a plan reads, so a preview can run over a
// read-only connection without starting the application.
func NewAccountAdoptionPlanner(
	finder repositories.AccountResourceFinder, accounts authrepos.AccountRepository,
	members repositories.AccountUsageReader,
) *AccountAdoptionService {
	return &AccountAdoptionService{finder: finder, accounts: accounts, members: members}
}

// AdoptAccount plans a move and, unless cmd.DryRun, applies it.
func (s *AccountAdoptionService) AdoptAccount(
	ctx context.Context, cmd AdoptAccountCommand,
) (AdoptAccountReport, error) {
	report, err := s.PlanAdoptAccount(ctx, cmd)
	if err != nil || cmd.DryRun {
		return report, err
	}
	return s.ApplyAdoptAccount(ctx, report)
}

// PlanAdoptAccount checks a command and selects what it would move, and
// writes nothing. It refuses unless the source exists and has no members,
// the target exists and is another account, and the agent is an owner or an
// admin of the target.
func (s *AccountAdoptionService) PlanAdoptAccount(
	ctx context.Context, cmd AdoptAccountCommand,
) (AdoptAccountReport, error) {
	report := AdoptAccountReport{
		FromAccountID: cmd.FromAccountID, AccountID: cmd.AccountID, AgentID: cmd.AgentID, DryRun: cmd.DryRun,
		Types: map[string]AdoptAccountTypeCount{},
	}
	switch {
	case cmd.FromAccountID == "":
		return report, errors.New("adopt account: no account to move from given")
	case cmd.AccountID == "":
		return report, errors.New("adopt account: no account to move to given")
	case cmd.AgentID == "":
		return report, errors.New("adopt account: no agent given")
	case cmd.FromAccountID == cmd.AccountID:
		return report, fmt.Errorf("%w: %s", ErrAdoptSameAccount, cmd.AccountID)
	}
	from, err := s.accounts.FindByID(ctx, cmd.FromAccountID)
	if err != nil {
		return report, fmt.Errorf("adopt account: load account %s: %w", cmd.FromAccountID, err)
	}
	if from == nil {
		return report, fmt.Errorf("%w: %s", ErrAdoptSourceNotFound, cmd.FromAccountID)
	}
	report.FromAccountName = from.Name()
	if err := s.checkSourceUnused(ctx, &report); err != nil {
		return report, err
	}
	if report.FromCreators, err = s.members.AccountCreators(ctx, cmd.FromAccountID); err != nil {
		return report, fmt.Errorf("adopt account: %w", err)
	}
	to, err := s.accounts.FindByID(ctx, cmd.AccountID)
	if err != nil {
		return report, fmt.Errorf("adopt account: load account %s: %w", cmd.AccountID, err)
	}
	if to == nil {
		return report, fmt.Errorf("%w: %s", ErrAdoptTargetNotFound, cmd.AccountID)
	}
	report.AccountName = to.Name()
	role, err := s.accounts.FindMemberRole(ctx, cmd.AccountID, cmd.AgentID)
	if err != nil {
		return report, fmt.Errorf("adopt account: read the role of %s in %s: %w", cmd.AgentID, cmd.AccountID, err)
	}
	report.AgentRole = role
	if role != authentities.RoleOwner && role != authentities.RoleAdmin {
		if role == "" {
			role = "not a member"
		}
		return report, fmt.Errorf("%w: %s in %s is %s", ErrAdoptAgentNotAdmin, cmd.AgentID, cmd.AccountID, role)
	}
	if s.finder == nil {
		return report, fmt.Errorf("adopt account: the resource repository %T cannot list an account's resources", s.repo)
	}
	found, err := s.finder.FindForAdoption(ctx, cmd.FromAccountID, cmd.AccountID)
	if err != nil {
		return report, fmt.Errorf("adopt account: %w", err)
	}
	for _, r := range found {
		c := report.Types[r.TypeSlug]
		switch {
		case r.ForeignRowTable != "":
			reason := fmt.Sprintf("the row in %s is owned by %s, and the record by %s",
				r.ForeignRowTable, r.ForeignRowAccountID, r.RecordAccountID)
			report.ForeignRows = append(report.ForeignRows, AssignOwnerForeignRow{
				ID: r.ID, TypeSlug: r.TypeSlug, Table: r.ForeignRowTable, RowAccountID: r.ForeignRowAccountID,
				RecordAccountID: r.RecordAccountID, Reason: reason,
			})
			c.ForeignRows++
		case r.RecordAccountID == cmd.AccountID:
			report.Resources = append(report.Resources, AdoptAccountResource{
				ID: r.ID, TypeSlug: r.TypeSlug, CreatedAt: r.CreatedAt, RecordMoved: true,
			})
			c.RowsToMove++
		default:
			report.Resources = append(report.Resources, AdoptAccountResource{
				ID: r.ID, TypeSlug: r.TypeSlug, CreatedAt: r.CreatedAt, MissingTable: r.MissingTable,
			})
			c.ToMove++
		}
		report.Types[r.TypeSlug] = c
	}
	return report, nil
}

// checkSourceUnused refuses a source account somebody can still use: one with
// a member, or with a pending invitation that would make one. It records both
// counts on the report.
func (s *AccountAdoptionService) checkSourceUnused(ctx context.Context, report *AdoptAccountReport) error {
	if s.members == nil {
		return errors.New("adopt account: no way to count the members of an account was wired")
	}
	var err error
	if report.FromMembers, err = s.members.CountAccountMembers(ctx, report.FromAccountID); err != nil {
		return fmt.Errorf("adopt account: %w", err)
	}
	if report.FromMembers > 0 {
		return fmt.Errorf("%w: %s has %d member(s)", ErrAdoptSourceHasMembers,
			report.FromAccountID, report.FromMembers)
	}
	if report.FromInvites, err = s.members.CountPendingInvites(ctx, report.FromAccountID, time.Now()); err != nil {
		return fmt.Errorf("adopt account: %w", err)
	}
	if report.FromInvites > 0 {
		return fmt.Errorf("%w: %s has %d pending invitation(s)", ErrAdoptSourceHasInvites,
			report.FromAccountID, report.FromInvites)
	}
	return nil
}

// ApplyAdoptAccount moves what a plan selected. Each resource commits on its
// own, so a run that stops part-way has moved what it reports, and a re-run
// finishes the rest: a moved resource is no longer in the source account.
//
// A resource is counted as moved only once its record and every projection
// row read back in the target. One whose record is behind its event stream is
// listed for a reproject and the run goes on. A row still in the source
// account after the move stops the run with an error naming the resource: a
// re-run moves that row from the record, with no second event.
//
// The agent named for the run is given a read grant on each resource it
// moves, before the move (wm-ebgqob). Lists and the search index show a
// caller what it created or holds a grant on, and a moved resource keeps the
// creator it had, so without the grant the owner of the target could open a
// moved resource by id and never find it. A grant the agent already holds is
// left alone, so a re-run grants nothing twice.
func (s *AccountAdoptionService) ApplyAdoptAccount(
	ctx context.Context, plan AdoptAccountReport,
) (AdoptAccountReport, error) {
	return s.ApplyAdoptAccountWithProgress(ctx, plan, nil)
}

// ApplyAdoptAccountWithProgress is ApplyAdoptAccount, calling progress, when
// it is not nil, after each resource it handles.
func (s *AccountAdoptionService) ApplyAdoptAccountWithProgress(
	ctx context.Context, plan AdoptAccountReport, progress func(AdoptAccountProgress),
) (AdoptAccountReport, error) {
	if s.eventStore == nil || s.dispatcher == nil {
		return plan, ErrAdoptReadOnly
	}
	if s.permissions == nil {
		return plan, errors.New("adopt account: no way to grant the agent read on the resources it moves was wired")
	}
	if s.projOwners == nil || s.mover == nil {
		return plan, fmt.Errorf("adopt account: the resource repository %T cannot read or move the owner of its "+
			"projection rows", s.repo)
	}
	report := plan
	report.DryRun = false
	// The checks are read again here: a member or an invitation can arrive
	// between the preview and the run, and the run moves only out of an
	// account nobody can use (wm-m0ci05).
	if err := s.checkSourceUnused(ctx, &report); err != nil {
		return report, err
	}
	report.Types = make(map[string]AdoptAccountTypeCount, len(plan.Types))
	for slug, c := range plan.Types {
		report.Types[slug] = AdoptAccountTypeCount{ToMove: c.ToMove, RowsToMove: c.RowsToMove, ForeignRows: c.ForeignRows}
	}
	report.NeedsReproject = append([]AssignOwnerSkip(nil), plan.NeedsReproject...)
	report.ForeignRows = append([]AssignOwnerForeignRow(nil), plan.ForeignRows...)
	typeTotal := map[string]int{}
	for _, r := range plan.Resources {
		typeTotal[r.TypeSlug]++
	}
	typeDone := map[string]int{}
	for i, r := range plan.Resources {
		move := s.adoptOne
		if r.RecordMoved {
			move = s.moveRows
		}
		outcome, note, granted, err := move(ctx, r, plan)
		c := report.Types[r.TypeSlug]
		if granted {
			c.Granted++
			report.Types[r.TypeSlug] = c
		}
		if err != nil {
			s.logger.Error(ctx, "adopt account stopped", "id", r.ID, "type", r.TypeSlug, "error", err)
			return report, fmt.Errorf("adopt account: %s (%s): %w", r.ID, r.TypeSlug, err)
		}
		switch outcome {
		case assignedOwner:
			if r.RecordMoved {
				c.RowsMoved++
			} else {
				c.Moved++
			}
		case gainedOwner:
			c.Skipped++
		case readModelBehind:
			c.NeedsReproject++
			report.NeedsReproject = append(report.NeedsReproject,
				AssignOwnerSkip{ID: r.ID, TypeSlug: r.TypeSlug, Reason: note.reason})
			s.logger.Warn(ctx, "adopt account left a resource for a reproject",
				"id", r.ID, "type", r.TypeSlug, "reason", note.reason)
		case rowOwnedElsewhere:
			c.ForeignRows++
			report.ForeignRows = append(report.ForeignRows, AssignOwnerForeignRow{
				ID: r.ID, TypeSlug: r.TypeSlug, Table: note.row.Table, RowAccountID: note.row.AccountID,
				RecordAccountID: plan.AccountID, Reason: note.reason,
			})
			s.logger.Warn(ctx, "adopt account met a projection row of a third account",
				"id", r.ID, "type", r.TypeSlug, "reason", note.reason)
		}
		report.Types[r.TypeSlug] = c
		typeDone[r.TypeSlug]++
		if progress != nil {
			progress(AdoptAccountProgress{
				Done: i + 1, Total: len(plan.Resources),
				TypeSlug: r.TypeSlug, TypeDone: typeDone[r.TypeSlug], TypeTotal: typeTotal[r.TypeSlug],
			})
		}
	}
	s.logger.Info(ctx, "moved the resources of an account with no members",
		"from", plan.FromAccountID, "to", plan.AccountID, "agent", plan.AgentID, "moved", report.Moved(),
		"rowsMoved", report.RowsMoved(), "granted", report.Granted(), "needsReproject", len(report.NeedsReproject),
		"foreignRows", len(report.ForeignRows))
	return report, nil
}

// errAdoptRecordDidNotMove is the read-back failure after a stored move.
var errAdoptRecordDidNotMove = errors.New("the Resource.AccountAdopted event is stored, " +
	"but the resource's record did not move (the projection write failed). " +
	"Stop the instance and run `weos worker reproject` to apply the stored event, then run this command again")

// errAdoptRowDidNotMove is the read-back failure when the record moved and a
// projection row did not. A re-run moves the row from the record.
var errAdoptRowDidNotMove = errors.New("the resource's record moved, " +
	"but its projection row did not (the projection write failed). " +
	"Fix the cause, then run this command again: it moves the row with no second event. " +
	"`weos worker reproject` on the stopped instance repairs it as well")

// adoptOne moves one resource. It reads the resource again first, so one
// that left the source account since the plan is skipped, never moved.
//
// It grants the agent read on the resource just before the move, so a run
// that stops between the two leaves a grant on a resource a re-run moves,
// never a moved resource its new owner cannot find. granted says whether
// this call wrote a grant.
func (s *AccountAdoptionService) adoptOne(
	ctx context.Context, r AdoptAccountResource, plan AdoptAccountReport,
) (outcome assignOutcome, note assignNote, granted bool, err error) {
	from, to := plan.FromAccountID, plan.AccountID
	entity, err := s.repo.FindByID(ctx, r.ID)
	if err != nil {
		return 0, assignNote{}, false, err
	}
	if entity.AccountID() != from {
		return gainedOwner, assignNote{}, false, nil
	}
	recordSeq := entity.GetSequenceNo()
	if reason, behind, err := storeAheadOfRecord(ctx, s.eventStore, r.ID, recordSeq); err != nil {
		return 0, assignNote{}, false, err
	} else if behind {
		return readModelBehind, assignNote{reason: reason}, false, nil
	}
	if granted, err = s.grantRead(ctx, r.ID, plan.AgentID, to); err != nil {
		return 0, assignNote{}, false, err
	}
	if err := entity.AdoptAccount(from, to); err != nil {
		return 0, assignNote{}, granted, err
	}
	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return 0, assignNote{}, granted, fmt.Errorf("track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		if errors.Is(err, domain.ErrConcurrencyConflict) {
			return readModelBehind, assignNote{reason: fmt.Sprintf(
				"version conflict on the append from sequence %d: %v", recordSeq, err)}, granted, nil
		}
		return 0, assignNote{}, granted, fmt.Errorf("commit the move: %w", err)
	}
	// The unit of work drops the projection's error; only a read-back shows
	// whether the record moved.
	after, err := s.repo.FindByID(ctx, r.ID)
	if err != nil {
		return 0, assignNote{}, granted, fmt.Errorf("read the record back: %w (%w)", err, errAdoptRecordDidNotMove)
	}
	if after.AccountID() != to {
		return 0, assignNote{}, granted, errAdoptRecordDidNotMove
	}
	outcome, note, err = s.readBackRows(ctx, r.ID, after.TypeSlug(), from, to)
	return outcome, note, granted, err
}

// grantRead gives agent a read grant on the resource id, through the
// permission service as the agent itself, an owner or admin of account. An
// agent that already holds read keeps its grant as it is, and one that holds
// a grant without read keeps its actions and gains read. It reports whether
// it wrote a grant.
func (s *AccountAdoptionService) grantRead(ctx context.Context, id, agent, account string) (bool, error) {
	actx := auth.ContextWithAgent(ctx, &auth.Identity{
		AgentID: agent, AccountIDs: []string{account}, ActiveAccountID: account,
	})
	held, err := s.permissions.ListForResource(actx, id)
	if err != nil {
		return false, fmt.Errorf("read the grants on the resource: %w", err)
	}
	actions := []string{"read"}
	for _, p := range held {
		if p.AgentID != agent {
			continue
		}
		if slices.Contains(p.Actions, "read") {
			return false, nil
		}
		actions = append(slices.Clone(p.Actions), "read")
	}
	raw, err := json.Marshal(actions)
	if err != nil {
		return false, err
	}
	if err := s.permissions.Grant(actx, GrantPermissionCommand{ResourceID: id, AgentID: agent, Actions: raw}); err != nil {
		return false, fmt.Errorf("grant %s read on the resource: %w", agent, err)
	}
	return true, nil
}

// moveRows finishes a move whose event is stored and whose record is in the
// target, but a projection row of which is still in the source. It moves the
// row compare-and-set and records no event. It does so only when the
// resource's history holds a move from this source to this target, so a row
// of the source under a record of the target that no move explains is
// listed, never moved.
func (s *AccountAdoptionService) moveRows(
	ctx context.Context, r AdoptAccountResource, plan AdoptAccountReport,
) (assignOutcome, assignNote, bool, error) {
	from, to := plan.FromAccountID, plan.AccountID
	entity, err := s.repo.FindByID(ctx, r.ID)
	if err != nil {
		return 0, assignNote{}, false, err
	}
	if entity.AccountID() != to {
		return gainedOwner, assignNote{}, false, nil
	}
	history, err := s.eventStore.GetEvents(ctx, r.ID)
	if err != nil {
		return 0, assignNote{}, false, fmt.Errorf("read the resource's history: %w", err)
	}
	if !historyMoved(history, from, to) {
		row := repositories.ProjectionRowOwner{AccountID: from}
		if owners, err := s.projOwners.ProjectionRowOwners(ctx, r.ID, entity.TypeSlug()); err == nil {
			for _, o := range owners {
				if !o.Missing && o.AccountID == from {
					row = o
					break
				}
			}
		}
		return rowOwnedElsewhere, assignNote{row: row, reason: fmt.Sprintf("the record is in %s and the row in %s "+
			"is in %s, and no move from %s is in its history", to, row.Table, from, from)}, false, nil
	}
	// A move whose run stopped before its grant would otherwise never get
	// one: the resource is no longer in the source account.
	granted, err := s.grantRead(ctx, r.ID, plan.AgentID, to)
	if err != nil {
		return 0, assignNote{}, false, err
	}
	if _, err := s.mover.MoveProjectionRowsOwner(ctx, r.ID, entity.TypeSlug(), from, to); err != nil {
		return 0, assignNote{}, granted, fmt.Errorf("move the projection rows: %w (%w)", err, errAdoptRowDidNotMove)
	}
	outcome, note, err := s.readBackRows(ctx, r.ID, entity.TypeSlug(), from, to)
	return outcome, note, granted, err
}

// historyMoved reports whether a resource's history holds a move from one
// account to the other.
func historyMoved(history []domain.EventEnvelope[any], from, to string) bool {
	for _, e := range history {
		if e.EventType != (entities.ResourceAccountAdopted{}).EventType() {
			continue
		}
		if f, t := adoptedAccounts(e.Payload); f == from && t == to {
			return true
		}
	}
	return false
}

// readBackRows reads each projection row's owner back. Every row in the
// target counts the resource. A row still in the source, or with no owner,
// is a write that failed and that a re-run repairs, so it is an error. A row
// that does not exist is left for a reproject; a row of a third account is
// listed apart.
func (s *AccountAdoptionService) readBackRows(
	ctx context.Context, id, typeSlug, from, to string,
) (assignOutcome, assignNote, error) {
	owners, err := s.projOwners.ProjectionRowOwners(ctx, id, typeSlug)
	if err != nil {
		return 0, assignNote{}, fmt.Errorf("read the projection rows back: %w (%w)", err, errAdoptRowDidNotMove)
	}
	var stuck, problems []string
	var foreign *repositories.ProjectionRowOwner
	for i, o := range owners {
		switch {
		case o.Missing:
			problems = append(problems, fmt.Sprintf("there is no row in %s at all", o.Table))
		case o.AccountID == to:
		case o.AccountID == from || o.AccountID == "":
			stuck = append(stuck, o.Table)
		default:
			problems = append(problems, fmt.Sprintf("the row in %s is owned by %s, not %s", o.Table, o.AccountID, to))
			if foreign == nil {
				foreign = &owners[i]
			}
		}
	}
	if len(stuck) > 0 {
		return 0, assignNote{}, fmt.Errorf("not moved in %s: %w", strings.Join(stuck, ", "), errAdoptRowDidNotMove)
	}
	if len(problems) == 0 {
		return assignedOwner, assignNote{}, nil
	}
	note := assignNote{reason: "the record moved, and " + strings.Join(problems, "; ") +
		", so the resource is missing from that listing"}
	if foreign != nil {
		note.row = *foreign
		return rowOwnedElsewhere, note, nil
	}
	return readModelBehind, note, nil
}

// storeAheadOfRecord reports whether the event store holds events for id past
// the record's sequence number. A record behind the store would make the
// append conflict on every run, so the resource is left for a reproject.
func storeAheadOfRecord(
	ctx context.Context, eventStore domain.EventStore, id string, recordSeq int,
) (string, bool, error) {
	storeSeq, err := eventStore.GetCurrentVersion(ctx, id)
	if err != nil {
		return "", false, fmt.Errorf("read the event store version: %w", err)
	}
	if storeSeq <= recordSeq {
		return "", false, nil
	}
	return fmt.Sprintf("the record is at sequence %d and the event store at %d", recordSeq, storeSeq), true, nil
}
