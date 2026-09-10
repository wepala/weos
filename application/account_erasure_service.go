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
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/internal/config"

	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

var (
	// ErrAccountNotFound is returned when the account to erase does not exist —
	// including when a previous run already erased it.
	ErrAccountNotFound = errors.New("account erasure: account not found")
	// ErrErasureDrainTimeout is returned when a background subscriber group
	// did not reach the head of the event log within the bound. The account is
	// left locked and nothing has been removed; running the deletion again
	// waits again.
	ErrErasureDrainTimeout = errors.New("account erasure: a background projection did not catch up in time")
)

// CheckpointPositionsFunc reads the committed position of every subscriber
// group that has a checkpoint row — every group that has ever processed the
// feed, whether or not this process runs it. A group with no row has never
// projected anything and has nothing to drain.
type CheckpointPositionsFunc func(ctx context.Context) (map[string]int64, error)

// EraseAccountCommand names the account to erase and who asked.
type EraseAccountCommand struct {
	AccountID   string
	RequestedBy string
}

// ErasureResult is what an erasure removed.
type ErasureResult struct {
	AccountID string
	// MembersLost is how many people belonged to the account when it went.
	MembersLost int
	Resources   int
	Events      int
}

// AccountErasureService removes an account and everything of its from the
// instance. It owns the sequence the design record fixes, and every step is
// idempotent so a run that fails part-way is finished by the next:
//
//  1. Lock: the erasure lock is recorded and the account deactivated, so
//     pericarp refuses every session in it from the next request on.
//  2. Drain: wait, bounded, until every subscriber group's checkpoint reaches
//     the head of the event log, so no background group projects the
//     account's events after the purge.
//  3. Enumerate the SQL state that names the account.
//  4. External stores: the bucket folder, then the graph. Neither is
//     transactional, so both go before the SQL commit — a failure leaves the
//     SQL state that drives enumeration in place for the re-run.
//  5. SQL: one chunked transaction, the account row last.
//
// The caller signs the person out; this service knows nothing of cookies.
type AccountErasureService struct {
	accounts     authrepos.AccountRepository
	locks        repositories.AccountErasureLocks
	purger       repositories.AccountDataPurger
	files        services.FileService
	graphs       repositories.KnowledgeGraphStores
	eventStore   domain.EventStore
	checkpoints  CheckpointPositionsFunc
	drainTimeout time.Duration
	drainPoll    time.Duration
	logger       entities.Logger
}

// AccountErasureParams bundles the service's dependencies.
type AccountErasureParams struct {
	fx.In
	Config      config.Config
	Accounts    authrepos.AccountRepository
	Locks       repositories.AccountErasureLocks
	Purger      repositories.AccountDataPurger
	Files       services.FileService
	Graphs      repositories.KnowledgeGraphStores
	EventStore  domain.EventStore
	Checkpoints CheckpointPositionsFunc
	Logger      entities.Logger
}

// ProvideAccountErasureService wires the service from the container.
func ProvideAccountErasureService(p AccountErasureParams) *AccountErasureService {
	return NewAccountErasureService(p.Accounts, p.Locks, p.Purger, p.Files, p.Graphs, p.EventStore,
		p.Checkpoints, p.Config.Worker.ErasureDrainTimeout, p.Logger)
}

// NewAccountErasureService builds the service without fx wiring.
func NewAccountErasureService(
	accounts authrepos.AccountRepository,
	locks repositories.AccountErasureLocks,
	purger repositories.AccountDataPurger,
	files services.FileService,
	graphs repositories.KnowledgeGraphStores,
	eventStore domain.EventStore,
	checkpoints CheckpointPositionsFunc,
	drainTimeout time.Duration,
	logger entities.Logger,
) *AccountErasureService {
	if drainTimeout <= 0 {
		drainTimeout = 30 * time.Second
	}
	return &AccountErasureService{
		accounts:     accounts,
		locks:        locks,
		purger:       purger,
		files:        files,
		graphs:       graphs,
		eventStore:   eventStore,
		checkpoints:  checkpoints,
		drainTimeout: drainTimeout,
		drainPoll:    100 * time.Millisecond,
		logger:       logger,
	}
}

// Erase runs the whole sequence for one account. It answers
// ErrAccountNotFound for an account that does not exist, and
// ErrErasureDrainTimeout when a background group never caught up; any other
// error is a step that failed with the account left locked.
func (s *AccountErasureService) Erase(ctx context.Context, cmd EraseAccountCommand) (*ErasureResult, error) {
	if cmd.AccountID == "" {
		return nil, fmt.Errorf("%w: no account named", ErrAccountNotFound)
	}
	account, err := s.accounts.FindByID(ctx, cmd.AccountID)
	if err != nil {
		return nil, fmt.Errorf("account erasure: load account %q: %w", cmd.AccountID, err)
	}
	if account == nil {
		return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, cmd.AccountID)
	}

	// Lock first, and the lock row before the deactivation: from the moment
	// the account is inactive, every refusal it produces has to be able to
	// say the deletion is unfinished.
	if err := s.locks.Lock(ctx, cmd.AccountID, cmd.RequestedBy); err != nil {
		return nil, err
	}
	if account.Active() {
		if err := account.Deactivate(); err != nil {
			return nil, fmt.Errorf("account erasure: deactivate %q: %w", cmd.AccountID, err)
		}
		if err := s.accounts.Save(ctx, account); err != nil {
			return nil, fmt.Errorf("account erasure: save the lock on %q: %w", cmd.AccountID, err)
		}
	}
	s.logger.Info(ctx, "account erasure: locked", "account_id", cmd.AccountID, "requested_by", cmd.RequestedBy)

	if err := s.drain(ctx); err != nil {
		return nil, err
	}

	enumeration, err := s.purger.Enumerate(ctx, cmd.AccountID)
	if err != nil {
		return nil, err
	}

	if err := s.files.DeleteAccountFolder(ctx, cmd.AccountID); err != nil {
		return nil, fmt.Errorf("account erasure: remove the file folder of %q: %w", cmd.AccountID, err)
	}
	if err := s.graphs.DropAccount(ctx, cmd.AccountID, enumeration.ResourceURNs); err != nil {
		return nil, fmt.Errorf("account erasure: drop the graph of %q: %w", cmd.AccountID, err)
	}

	report, err := s.purger.Purge(ctx, cmd.AccountID)
	if err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "account erasure: finished",
		"account_id", cmd.AccountID, "members", report.Members,
		"resources", report.Resources, "events", report.Events, "deleted_agents", len(report.DeletedAgents))
	return &ErasureResult{
		AccountID:   cmd.AccountID,
		MembersLost: report.Members,
		Resources:   report.Resources,
		Events:      report.Events,
	}, nil
}

// drain reads the head of the event log once — after the lock, so nothing
// of the account's can be appended past it — and waits until every group
// with a checkpoint has processed up to it. Groups are read fresh on every
// poll so one that starts during the wait is waited for too.
func (s *AccountErasureService) drain(ctx context.Context) error {
	head, err := s.eventStore.HeadPosition(ctx)
	if err != nil {
		return fmt.Errorf("account erasure: read the head of the event log: %w", err)
	}
	deadline := time.Now().Add(s.drainTimeout)
	for {
		positions, err := s.checkpoints(ctx)
		if err != nil {
			return fmt.Errorf("account erasure: read subscriber checkpoints: %w", err)
		}
		behind := lagging(positions, head)
		if len(behind) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %v still behind position %d after %s",
				ErrErasureDrainTimeout, behind, head, s.drainTimeout)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("account erasure: drain interrupted: %w", ctx.Err())
		case <-time.After(s.drainPoll):
		}
	}
}

// lagging names the groups whose checkpoint is behind head.
func lagging(positions map[string]int64, head int64) []string {
	var behind []string
	for name, position := range positions {
		if position < head {
			behind = append(behind, name)
		}
	}
	return behind
}
