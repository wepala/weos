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
	"reflect"
	"runtime/debug"
	"sort"

	"go.uber.org/fx"
)

// ErrErasureParticipantFailed is returned when a participant's step failed.
// The erasure stops there with nothing of the account's removed, and the
// account is left locked, so running the deletion again runs every
// participant again.
var ErrErasureParticipantFailed = errors.New("account erasure: a participant step failed")

// ErasingAccount names the account a participant is being asked about, and
// who asked for the deletion. Everything else the participant needs — the
// account's members, its resources, its credentials — it reads through its
// own repositories: on the first pass the account's SQL rows, files and
// graph are all still there when the participant runs.
type ErasingAccount struct {
	// AccountID is the account being erased.
	AccountID string
	// RequestedBy is whoever asked for the deletion, as the caller labelled
	// them: the agent id of the person who asked through the API, and the
	// word "operator" when the deletion was run from the command line. It is
	// a label to log and to branch on at the caller's own risk, not an
	// identity this service checks.
	RequestedBy string
	// Pass is 1 the first time the participants run for this deletion, and
	// 2 or more when they are being asked again about rows that landed after
	// the purge (a request admitted just before the lock can commit after
	// it). Past the first pass most of the account's data is gone; the rows
	// that are left are what the sweep is about to remove.
	Pass int
	// AccountGone reports that the account's own row was already removed
	// when this deletion started — a sweep of what an earlier, finished
	// deletion left behind. The participant is still asked, because those
	// rows can name something outside this instance, but it may find nothing
	// of the account's left to read. Finding nothing is success.
	AccountGone bool
}

// AccountErasureParticipant is a step an embedding service runs inside an
// account erasure. It is the seam for the work core cannot do itself: a
// deployment that linked the account to something outside this instance has
// to unlink it when the account goes, and only that deployment knows how.
// Two examples, neither of which core depends on: dropping a bank-aggregator
// item and the credentials it was linked with, and revoking an identity
// provider's token for the person who is leaving.
//
// When it runs. BeforeAccountErased is called inside
// AccountErasureService.Erase, after the account is locked and deactivated
// and after the background projections have drained, and before the first
// step that removes anything — the enumeration, the file folder, the graph
// and the SQL purge all follow it. So on that first pass a participant sees
// the account's data whole, and it sees a read model that has caught up with
// every event the account committed.
//
// It can be asked more than once in one deletion. The lock stops new
// requests, not requests already admitted, so a request that was let in a
// moment before the lock can commit rows after the purge; those rows are
// swept again, and the participants run again before each of those sweeps,
// because a row that landed that way can name something outside this
// instance too. ErasingAccount.Pass says which pass this is. Past the first
// one most of the account's data is already gone.
//
// Two things the first pass does not always promise. A deletion an operator
// ran with --skip-drain has not waited for the background projections, so a
// participant that reads a projection may be reading a stale one. And a
// deletion whose account row was already gone — a sweep of what an earlier
// deletion left behind — takes no lock and runs with the account's data
// already purged; ErasingAccount.AccountGone says so. In both cases the
// participant may find nothing of the account's to work from. Finding
// nothing is success, not a failure to report: a participant that errors
// there wedges the cleanup of those rows for good.
//
// It runs synchronously. Erase returns only once every participant has
// returned, and the HTTP handler answers 200 only once Erase has returned —
// so a 2xx on the deletion means every participant completed.
//
// What an error does. The erasure stops at the first participant that
// returns one and answers ErrErasureParticipantFailed, wrapping both the
// participant's name and its error. Nothing of the account's has been
// removed at that point, the account is left locked and inactive, and the
// caller sees the failure rather than a 2xx. The deletion can be run again,
// and it runs every participant again from the start: a participant must be
// idempotent, like every other step of the sequence.
//
// A participant that panics fails the same way. The panic is contained at
// this boundary, logged with its stack — the only place it is written down,
// since it no longer reaches the server's own recovery — and returned as
// ErrErasureParticipantFailed naming the step, so the caller gets an answer
// rather than a dropped connection.
//
// The context carries the erasure's own deadline and is detached from the
// request, so a caller that hangs up does not cancel the step. A participant
// must honor it — it is the only bound on the run.
type AccountErasureParticipant interface {
	// Name identifies the participant in the log and in the error a failed
	// step fails the erasure with. Keep it short and stable: it is what an
	// operator reads when a deletion did not finish.
	Name() string
	// BeforeAccountErased runs the step. Returning an error aborts the
	// erasure with nothing removed.
	BeforeAccountErased(ctx context.Context, account ErasingAccount) error
}

// accountErasureParticipantGroup is the Fx value group through which a
// downstream binary contributes participants, and the tags both registration
// helpers write are built from it. The erasure service collects the whole
// group; a helper that spelled the name itself could drift from the
// collector silently, and the step would simply never run.
const (
	accountErasureParticipantGroup = "account_erasure_participants"
	accountErasureParticipantTag   = `group:"` + accountErasureParticipantGroup + `"`
	accountErasureParticipantsTag  = `group:"` + accountErasureParticipantGroup + `,flatten"`
)

// AsAccountErasureParticipant tags a constructor so its result joins the
// "account_erasure_participants" value group the erasure service collects.
// It is the whole seam for out-of-tree erasure steps; nothing in core
// imports anything to support it:
//
//	cli.RegisterErasureFxOptions(
//		fx.Provide(application.AsAccountErasureParticipant(newBankLinkRemover)),
//	)
//
// The constructor is an ordinary Fx provider, so a participant takes its own
// dependencies from the container. Register it with RegisterErasureFxOptions
// rather than RegisterFxOptions: the second is merged into the server's
// graph only, so a participant registered with it would not run when an
// operator finishes a deletion with "account delete".
//
// The constructor may declare its own type as its result — newBankLinkRemover
// returning *BankLinkRemover — which is how one is naturally written. The
// annotation casts the result to AccountErasureParticipant before tagging it,
// because a value group is keyed by the type as well as the name: a result
// tagged as *BankLinkRemover would join a group of *BankLinkRemover that
// nothing collects, and neither the container nor the deletion would say so.
//
// Order. Fx does not promise an order for the members of a value group — dig
// deliberately shuffles them — so the container path runs participants
// sorted by Name, which is stable across restarts. A participant must not
// depend on another participant having run; where an order really matters,
// build the ordered sequence yourself and register it as one participant, or
// wire the service with AccountErasureDeps.Participants, which runs in the
// order the slice gives.
func AsAccountErasureParticipant(constructor any) any {
	return fx.Annotate(constructor,
		fx.As(new(AccountErasureParticipant)),
		fx.ResultTags(accountErasureParticipantTag))
}

// AsAccountErasureParticipants tags a constructor returning
// []AccountErasureParticipant so its elements are flattened into the value
// group. Use it for a provider that contributes zero or more participants
// depending on configuration — the bank-link remover of a deployment that
// has no aggregator configured contributes none:
//
//	cli.RegisterErasureFxOptions(
//		fx.Provide(application.AsAccountErasureParticipants(newExternalUnlinkers)),
//	)
//
// The constructor must return []AccountErasureParticipant exactly. A slice of
// some other type cannot be cast element by element on the way into the
// group, so this refuses it where the mistake is made — at registration, as
// the binary wires itself — rather than letting it join a group nothing
// collects and finding out when an external link is left behind.
func AsAccountErasureParticipants(constructor any) any {
	mustReturnParticipants(constructor)
	return fx.Annotate(constructor, fx.ResultTags(accountErasureParticipantsTag))
}

// mustReturnParticipants refuses a flattening constructor whose first result
// is not the interface slice the value group is keyed by.
func mustReturnParticipants(constructor any) {
	fn := reflect.TypeOf(constructor)
	if fn == nil || fn.Kind() != reflect.Func || fn.NumOut() == 0 {
		panic("application.AsAccountErasureParticipants: want a constructor function returning []application.AccountErasureParticipant")
	}
	if got := fn.Out(0); got != reflect.TypeOf([]AccountErasureParticipant(nil)) {
		panic(fmt.Sprintf(
			"application.AsAccountErasureParticipants: the constructor returns %s; it must return []application.AccountErasureParticipant, "+
				"because a value group is keyed by its element type and a slice of anything else joins a group nothing collects", got))
	}
}

// sortParticipantsByName copies the participants into a stable run order.
// The container hands over a shuffled value group; the run order has to be
// the same on every process that has the same participants, or a deletion
// that failed reproduces differently from the one that failed.
func sortParticipantsByName(participants []AccountErasureParticipant) []AccountErasureParticipant {
	sorted := participantsOf(participants)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name() < sorted[j].Name() })
	return sorted
}

// participantsOf keeps the order it is given and drops the nil entries a
// hand-wired Deps can carry.
func participantsOf(participants []AccountErasureParticipant) []AccountErasureParticipant {
	kept := make([]AccountErasureParticipant, 0, len(participants))
	for _, p := range participants {
		if p != nil {
			kept = append(kept, p)
		}
	}
	return kept
}

// ParticipantNames names the participants this service runs, in the order it
// runs them. A command that is about to erase an account says them out loud,
// so a binary whose participants were registered into a graph this command
// does not carry reads as "none registered" rather than as silence.
func (s *AccountErasureService) ParticipantNames() []string {
	names := make([]string, 0, len(s.participants))
	for _, participant := range s.participants {
		names = append(names, participantName(participant))
	}
	return names
}

// runParticipants runs every registered participant, in order, and stops at
// the first one that fails. It is called from Erase before anything is
// removed; see AccountErasureParticipant for what that guarantees.
func (s *AccountErasureService) runParticipants(ctx context.Context, cmd EraseAccountCommand, pass int, accountGone bool) error {
	account := ErasingAccount{
		AccountID:   cmd.AccountID,
		RequestedBy: cmd.RequestedBy,
		Pass:        pass,
		AccountGone: accountGone,
	}
	// finished is the last step that returned, so a budget that ran out is
	// reported against what spent it rather than against the step that never
	// started.
	finished := ""
	for _, participant := range s.participants {
		name := participantName(participant)
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("account erasure: ran out of time %s, with the %s step still to run: %w",
				spentBy(finished), name, err)
		}
		// Said before the step, not only after it: a deletion stuck on a
		// participant answers every retry "already running", and this line is
		// the only thing that names which step it is stuck on.
		s.logger.Info(ctx, "account erasure: participant starting",
			"account_id", cmd.AccountID, "participant", name, "pass", pass)
		if err := s.callParticipant(ctx, participant, name, account); err != nil {
			s.logger.Error(ctx, "account erasure: a participant failed; nothing of the account has been removed from this instance",
				"account_id", cmd.AccountID, "participant", name, "error", err)
			return fmt.Errorf("%w: %s: %w", ErrErasureParticipantFailed, name, err)
		}
		s.logger.Info(ctx, "account erasure: participant finished", "account_id", cmd.AccountID, "participant", name)
		finished = name
	}
	return nil
}

// spentBy says what used the budget up, for a deadline that passed between
// two steps.
func spentBy(finished string) string {
	if finished == "" {
		return "before any participant ran"
	}
	return "after the " + finished + " step"
}

// callParticipant runs one participant's step and turns a panic into the
// error a failed step returns. A participant is the embedding binary's own
// code and core neither validates nor sandboxes it, which is the argument
// for containing what it does here: a panic that unwound out of Erase would
// drop the caller's connection with no answer at all — not the 500 the
// contract promises — name no step in the erasure log, and kill an
// operator's command mid-run.
func (s *AccountErasureService) callParticipant(
	ctx context.Context, participant AccountErasureParticipant, name string, account ErasingAccount,
) (err error) {
	// Each step gets its own slice of the erasure's budget. A participant
	// wraps code core does not control — commonly a third-party SDK whose
	// HTTP client has no timeout of its own — and the whole budget is shared
	// with the bucket walk and every step after it, so one hang must not
	// spend all of it. A participant that ignores its context can still
	// hang; nothing here can preempt it.
	ctx, cancel := context.WithTimeout(ctx, s.participantTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			// The stack goes to the log because it no longer reaches the
			// server's own recovery: this is the only place it is written
			// down for whoever wrote the participant.
			s.logger.Error(ctx, "account erasure: a participant panicked",
				"account_id", account.AccountID, "participant", name,
				"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
			err = fmt.Errorf("panicked: %v", recovered)
		}
	}()
	return participant.BeforeAccountErased(ctx, account)
}

// participantName is what the log and the error call a participant. A
// participant that names itself nothing is named by its type, so the failure
// still says which step it was.
func participantName(participant AccountErasureParticipant) string {
	if name := participant.Name(); name != "" {
		return name
	}
	return fmt.Sprintf("%T", participant)
}
