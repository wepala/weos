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
	"sync"
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
	// ErrErasureInProgress is returned when this process is already erasing
	// the account: a retry that arrives while the first run is still walking
	// the bucket must not start a second walk beside it.
	ErrErasureInProgress = errors.New("account erasure: a deletion of this account is already running")
)

// SubscriberCheckpoint is one row of the checkpoint table: where a group has
// got to, and when it last wrote that down.
type SubscriberCheckpoint struct {
	Name      string
	Position  int64
	UpdatedAt time.Time
}

// CheckpointPositionsFunc reads the checkpoint row of every subscriber group
// that has one — every group that has ever processed the feed, whether or not
// this process runs it. A group with no row has never projected anything and
// has nothing to drain.
type CheckpointPositionsFunc func(ctx context.Context) ([]SubscriberCheckpoint, error)

// RunningGroupsFunc names the subscriber groups this process is running. A
// group named here is alive by construction, so the drain waits for it
// however old its checkpoint row is. A process that runs no groups — the
// operator command, an API-only deployment — names none.
type RunningGroupsFunc func() []string

// EraseAccountCommand names the account to erase and who asked.
type EraseAccountCommand struct {
	AccountID   string
	RequestedBy string
	// SkipDrain purges without waiting for the background groups. It is the
	// operator's override for a checkpoint that will never move, and the
	// command line refuses it without --confirm; the app never sets it.
	SkipDrain bool
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
	running      RunningGroupsFunc
	drainTimeout time.Duration
	// staleAfter is how long a checkpoint row nobody here runs may go
	// unwritten before the drain treats it as frozen. A live group that is
	// behind writes its row as it catches up, so a row older than this that
	// still does not move is one no process is advancing.
	staleAfter time.Duration
	// frozenGrace is how long the drain gives a stale row to move before it
	// stops waiting on it: a worker in another process wakes on the commit
	// it is behind by and writes its row within this.
	frozenGrace time.Duration
	drainPoll   time.Duration
	// timeout bounds the whole erasure. The run is detached from the
	// caller's context, so this is the only deadline it has.
	timeout time.Duration
	logger  entities.Logger

	mu       sync.Mutex
	inFlight map[string]bool
}

// AccountErasureDeps is everything the service is built from.
type AccountErasureDeps struct {
	Accounts      authrepos.AccountRepository
	Locks         repositories.AccountErasureLocks
	Purger        repositories.AccountDataPurger
	Files         services.FileService
	Graphs        repositories.KnowledgeGraphStores
	EventStore    domain.EventStore
	Checkpoints   CheckpointPositionsFunc
	RunningGroups RunningGroupsFunc
	DrainTimeout  time.Duration
	StaleAfter    time.Duration
	Timeout       time.Duration
	Logger        entities.Logger
}

// AccountErasureParams bundles the service's dependencies from the container.
type AccountErasureParams struct {
	fx.In
	Config        config.Config
	Accounts      authrepos.AccountRepository
	Locks         repositories.AccountErasureLocks
	Purger        repositories.AccountDataPurger
	Files         services.FileService
	Graphs        repositories.KnowledgeGraphStores
	EventStore    domain.EventStore
	Checkpoints   CheckpointPositionsFunc
	RunningGroups RunningGroupsFunc
	Logger        entities.Logger
}

// ProvideAccountErasureService wires the service from the container.
func ProvideAccountErasureService(p AccountErasureParams) *AccountErasureService {
	return NewAccountErasureService(AccountErasureDeps{
		Accounts:      p.Accounts,
		Locks:         p.Locks,
		Purger:        p.Purger,
		Files:         p.Files,
		Graphs:        p.Graphs,
		EventStore:    p.EventStore,
		Checkpoints:   p.Checkpoints,
		RunningGroups: p.RunningGroups,
		DrainTimeout:  p.Config.Worker.ErasureDrainTimeout,
		StaleAfter:    p.Config.Worker.ErasureDrainStaleAfter,
		Timeout:       p.Config.Worker.ErasureTimeout,
		Logger:        p.Logger,
	})
}

// NewAccountErasureService builds the service without fx wiring.
func NewAccountErasureService(d AccountErasureDeps) *AccountErasureService {
	if d.DrainTimeout <= 0 {
		d.DrainTimeout = 30 * time.Second
	}
	if d.StaleAfter <= 0 {
		d.StaleAfter = 10 * time.Minute
	}
	if d.RunningGroups == nil {
		d.RunningGroups = func() []string { return nil }
	}
	if d.Timeout <= 0 {
		d.Timeout = 15 * time.Minute
	}
	return &AccountErasureService{
		accounts:     d.Accounts,
		locks:        d.Locks,
		purger:       d.Purger,
		files:        d.Files,
		graphs:       d.Graphs,
		eventStore:   d.EventStore,
		checkpoints:  d.Checkpoints,
		running:      d.RunningGroups,
		drainTimeout: d.DrainTimeout,
		staleAfter:   d.StaleAfter,
		frozenGrace:  2 * time.Second,
		drainPoll:    100 * time.Millisecond,
		timeout:      d.Timeout,
		logger:       d.Logger,
		inFlight:     map[string]bool{},
	}
}

// Erase runs the whole sequence for one account. It answers
// ErrAccountNotFound for an account that does not exist,
// ErrErasureDrainTimeout when a background group never caught up, and
// ErrErasureInProgress when this process is already erasing the account;
// any other error is a step that failed with the account left locked.
//
// The run is detached from the caller's context and given its own deadline.
// A person who asked for the deletion and then hung up — a mobile client
// whose timeout is shorter than a bucket walk — must not abort it: the lock
// row is the state the deletion keeps, and a cancelled walk would leave it
// locked with nothing removed (wm-mpj0l).
func (s *AccountErasureService) Erase(ctx context.Context, cmd EraseAccountCommand) (*ErasureResult, error) {
	if cmd.AccountID == "" {
		return nil, fmt.Errorf("%w: no account named", ErrAccountNotFound)
	}
	if !s.begin(cmd.AccountID) {
		return nil, fmt.Errorf("%w: %s", ErrErasureInProgress, cmd.AccountID)
	}
	defer s.end(cmd.AccountID)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer cancel()

	account, err := s.accounts.FindByID(ctx, cmd.AccountID)
	if err != nil {
		return nil, fmt.Errorf("account erasure: load account %q: %w", cmd.AccountID, err)
	}
	if account == nil {
		// The row is gone. Either the deletion finished — nothing names the
		// account any more — or a request admitted before the lock committed
		// after the purge and left rows the finished deletion refused to
		// touch (wm-mnry2). The second is swept here, with no lock to take:
		// the row the lock hangs off is the one that is gone.
		left, err := s.purger.Remains(ctx, cmd.AccountID)
		if err != nil {
			return nil, err
		}
		if !left {
			return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, cmd.AccountID)
		}
		s.logger.Warn(ctx, "account erasure: the account row is gone but rows naming it remain; sweeping them",
			"account_id", cmd.AccountID, "requested_by", cmd.RequestedBy)
	} else {
		// Lock first, and the lock row before the deactivation: from the
		// moment the account is inactive, every refusal it produces has to be
		// able to say the deletion is unfinished.
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
	}

	if cmd.SkipDrain {
		s.logger.Warn(ctx, "account erasure: the drain was skipped on the operator's say-so", "account_id", cmd.AccountID)
	} else if err := s.drain(ctx); err != nil {
		return nil, err
	}

	report, err := s.sweep(ctx, cmd.AccountID)
	if err != nil {
		return nil, err
	}

	// The lock stops new requests, not requests already admitted: one
	// admitted a moment before it can commit after the head was read, even
	// after the purge's transaction. What it left is swept again, up to a
	// bound, so the deletion does not strand rows it can no longer reach
	// through the account row (wm-mnry2).
	for pass := 1; ; pass++ {
		left, err := s.purger.Remains(ctx, cmd.AccountID)
		if err != nil {
			return nil, err
		}
		if !left {
			break
		}
		if pass > orphanSweeps {
			return nil, fmt.Errorf("account erasure: rows naming %q kept arriving after %d sweeps; run the deletion again",
				cmd.AccountID, orphanSweeps)
		}
		s.logger.Warn(ctx, "account erasure: rows landed after the purge; sweeping again", "account_id", cmd.AccountID, "pass", pass)
		again, err := s.sweep(ctx, cmd.AccountID)
		if err != nil {
			return nil, err
		}
		report.Events += again.Events
		report.Resources += again.Resources
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

// orphanSweeps bounds how many times the purge is run again for rows that
// landed after it.
const orphanSweeps = 2

// sweep is the part of the sequence that removes things: enumerate, then the
// external stores, then the SQL purge. Every step is idempotent, so it is
// safe to run again on whatever a late commit left.
func (s *AccountErasureService) sweep(ctx context.Context, accountID string) (*repositories.PurgeReport, error) {
	enumeration, err := s.purger.Enumerate(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if err := s.files.DeleteAccountFolder(ctx, accountID); err != nil {
		return nil, fmt.Errorf("account erasure: remove the file folder of %q: %w", accountID, err)
	}
	if err := s.graphs.DropAccount(ctx, accountID, enumeration.ResourceURNs); err != nil {
		return nil, fmt.Errorf("account erasure: drop the graph of %q: %w", accountID, err)
	}
	report, err := s.purger.Purge(ctx, accountID)
	if err != nil {
		if errors.Is(err, repositories.ErrNothingToPurge) {
			// Another run finished it between the checks above and the
			// purge's own: the second of two deletions that arrived together
			// (wm-4cysr).
			return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, accountID)
		}
		return nil, err
	}
	return report, nil
}

// begin claims the account for one run in this process. It answers false
// when another run holds it.
func (s *AccountErasureService) begin(accountID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight[accountID] {
		return false
	}
	s.inFlight[accountID] = true
	return true
}

func (s *AccountErasureService) end(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, accountID)
}

// drain reads the head of the event log once — after the lock, so nothing
// of the account's can be appended past it — and waits until every group
// that can still move has processed up to it. Rows are read fresh on every
// poll so a group that starts during the wait is waited for too.
//
// A row that no process is advancing must not hold the drain forever: a
// group that was turned off, renamed or retired leaves its row where it
// stopped, and waiting on it fails every deletion on the instance
// (wm-gyfdi). Such a row is told from a live one by two things together: it
// is not a group this process runs, and it has not been written for longer
// than staleAfter. A live group that is behind writes its row as it catches
// up, and a worker in another process wakes on the commit it is behind by,
// so a stale row is given frozenGrace to move before it is set aside.
func (s *AccountErasureService) drain(ctx context.Context) error {
	head, err := s.eventStore.HeadPosition(ctx)
	if err != nil {
		return fmt.Errorf("account erasure: read the head of the event log: %w", err)
	}
	started := time.Now()
	deadline := started.Add(s.drainTimeout)
	running := map[string]bool{}
	for _, name := range s.running() {
		running[name] = true
	}
	for {
		rows, err := s.checkpoints(ctx)
		if err != nil {
			return fmt.Errorf("account erasure: read subscriber checkpoints: %w", err)
		}
		now := time.Now()
		behind, frozen := s.lagging(rows, head, running, now, now.Sub(started) >= s.frozenGrace)
		if len(behind) == 0 {
			if len(frozen) > 0 {
				s.logger.Warn(ctx, "account erasure: checkpoint rows nobody is advancing were not waited for",
					"groups", frozen, "stale_after", s.staleAfter.String())
			}
			return nil
		}
		if now.After(deadline) {
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

// lagging sorts the rows behind head into the ones to wait for and the
// frozen ones to set aside. Before the grace has passed every row behind head
// is waited for.
func (s *AccountErasureService) lagging(
	rows []SubscriberCheckpoint, head int64, running map[string]bool, now time.Time, graceOver bool,
) (behind, frozen []string) {
	for _, row := range rows {
		if row.Position >= head {
			continue
		}
		stale := !running[row.Name] && now.Sub(row.UpdatedAt) > s.staleAfter
		if stale && graceOver {
			frozen = append(frozen, row.Name)
			continue
		}
		behind = append(behind, row.Name)
	}
	return behind, frozen
}
