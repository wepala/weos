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
)

// AssignOwnerCommand selects the resources to give an owner: those with no
// owning account that CreatedBy created. AccountID is the owner to give them.
// DryRun counts them and writes nothing.
type AssignOwnerCommand struct {
	AccountID string
	CreatedBy string
	DryRun    bool
}

// AssignOwnerTypeCount is one resource type's line in the report.
type AssignOwnerTypeCount struct {
	// Ownerless is how many of the creator's resources of this type had no
	// owner when the run started.
	Ownerless int
	// Assigned is how many of them this run gave the owner. Always 0 on a
	// dry run.
	Assigned int
	// Skipped is how many already had an owner by the time the run reached
	// them — a concurrent write — and so were left as they were.
	Skipped int
	// NeedsReproject is how many were left ownerless because their read
	// model is behind the event store. Each is listed in the report's
	// NeedsReproject.
	NeedsReproject int
}

// AssignOwnerSkip names a resource a run left ownerless, and why.
type AssignOwnerSkip struct {
	ID       string
	TypeSlug string
	Reason   string
}

// AssignOwnerReport says what a run found and did, per resource type.
type AssignOwnerReport struct {
	AccountID string
	CreatedBy string
	DryRun    bool
	Types     map[string]AssignOwnerTypeCount
	// NeedsReproject lists the resources the run skipped because their read
	// model is behind the event store. `weos worker reproject` brings the read
	// model up to the store; a later run then assigns what is still ownerless.
	NeedsReproject []AssignOwnerSkip
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

// Ownerless is the number of ownerless resources found, over every type.
func (r AssignOwnerReport) Ownerless() int {
	n := 0
	for _, c := range r.Types {
		n += c.Ownerless
	}
	return n
}

// Assigned is the number of resources given the owner, over every type.
func (r AssignOwnerReport) Assigned() int {
	n := 0
	for _, c := range r.Types {
		n += c.Assigned
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
	accounts   authrepos.AccountRepository
	eventStore domain.EventStore
	dispatcher *domain.EventDispatcher
	logger     entities.Logger
}

// ProvideOwnerBackfillService wires the service from the container.
func ProvideOwnerBackfillService(p OwnerBackfillParams) *OwnerBackfillService {
	return &OwnerBackfillService{
		repo:       p.Repo,
		accounts:   p.Accounts,
		eventStore: p.EventStore,
		dispatcher: p.Dispatcher,
		logger:     p.Logger,
	}
}

// AssignOwner gives every resource with no owning account that cmd.CreatedBy
// created the owner cmd.AccountID, one Resource.AccountAssigned per resource.
//
// It refuses to run unless the account exists and the creator is a member of
// it. A resource that has an owner is never touched. Each resource commits on
// its own, so a run that stops part-way has assigned what it reports, and
// running it again finishes the rest: a resource already assigned is no
// longer ownerless and is not selected again.
//
// A resource is counted as assigned only once its record is read back with
// the owner. A resource whose read model is behind its event stream — found
// before the write, or met as a version conflict on it — is skipped, listed in
// the report's NeedsReproject, and the run goes on. A write whose event was
// stored but whose record did not take the owner stops the run with an error
// that names the resource: the read model is failing, and every later write
// would fail the same way.
func (s *OwnerBackfillService) AssignOwner(
	ctx context.Context, cmd AssignOwnerCommand,
) (AssignOwnerReport, error) {
	report := AssignOwnerReport{
		AccountID: cmd.AccountID, CreatedBy: cmd.CreatedBy, DryRun: cmd.DryRun,
		Types: map[string]AssignOwnerTypeCount{},
	}
	if cmd.AccountID == "" {
		return report, errors.New("assign owner: no account id given")
	}
	if cmd.CreatedBy == "" {
		return report, errors.New("assign owner: no creator agent id given")
	}
	if err := s.checkMembership(ctx, cmd.AccountID, cmd.CreatedBy); err != nil {
		return report, err
	}
	finder, ok := s.repo.(repositories.OwnerlessResourceFinder)
	if !ok {
		return report, fmt.Errorf("assign owner: the resource repository %T cannot list ownerless resources", s.repo)
	}
	ownerless, err := finder.FindOwnerlessByCreator(ctx, cmd.CreatedBy)
	if err != nil {
		return report, fmt.Errorf("assign owner: %w", err)
	}
	for _, r := range ownerless {
		c := report.Types[r.TypeSlug]
		c.Ownerless++
		report.Types[r.TypeSlug] = c
	}
	if cmd.DryRun {
		return report, nil
	}
	for _, r := range ownerless {
		outcome, reason, err := s.assignOne(ctx, r.ID, cmd.AccountID, cmd.CreatedBy)
		if err != nil {
			s.logger.Error(ctx, "assign owner stopped", "id", r.ID, "type", r.TypeSlug, "error", err)
			return report, fmt.Errorf("assign owner: %s (%s): %w", r.ID, r.TypeSlug, err)
		}
		c := report.Types[r.TypeSlug]
		switch outcome {
		case assignedOwner:
			c.Assigned++
		case gainedOwner:
			c.Skipped++
		case readModelBehind:
			c.NeedsReproject++
			report.NeedsReproject = append(report.NeedsReproject,
				AssignOwnerSkip{ID: r.ID, TypeSlug: r.TypeSlug, Reason: reason})
			s.logger.Warn(ctx, "assign owner skipped a resource whose read model is behind the event store",
				"id", r.ID, "type", r.TypeSlug, "reason", reason)
		}
		report.Types[r.TypeSlug] = c
	}
	s.logger.Info(ctx, "assigned owners to ownerless resources",
		"account", cmd.AccountID, "createdBy", cmd.CreatedBy, "assigned", report.Assigned(),
		"needsReproject", len(report.NeedsReproject))
	return report, nil
}

func (s *OwnerBackfillService) checkMembership(ctx context.Context, accountID, agentID string) error {
	account, err := s.accounts.FindByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("assign owner: load account %s: %w", accountID, err)
	}
	if account == nil {
		return fmt.Errorf("%w: %s", ErrOwnerAccountNotFound, accountID)
	}
	role, err := s.accounts.FindMemberRole(ctx, accountID, agentID)
	if err != nil {
		return fmt.Errorf("assign owner: read the membership of %s in %s: %w", agentID, accountID, err)
	}
	if role == "" {
		return fmt.Errorf("%w: %s is not a member of %s", ErrOwnerAgentNotMember, agentID, accountID)
	}
	return nil
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

// assignOne gives one resource its owner. It reads the resource again first,
// so a resource that gained an owner (or changed creator) since the selection
// is left alone and reported as skipped rather than refused.
//
// The record's version is compared with the event store's before the write.
// A record behind the store would make the append conflict, and every run
// after it would conflict on the same record, so it is reported as behind
// instead, with what the store holds past it.
func (s *OwnerBackfillService) assignOne(
	ctx context.Context, id, accountID, createdBy string,
) (assignOutcome, string, error) {
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
	return assignedOwner, "", nil
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
