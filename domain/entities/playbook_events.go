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

package entities

import (
	"context"
	"time"
)

// PlaybookOutcome is an agent's verdict after using a playbook.
type PlaybookOutcome string

const (
	// PlaybookOutcomeConfirmed marks a procedure that worked.
	PlaybookOutcomeConfirmed PlaybookOutcome = "confirmed"
	// PlaybookOutcomeRejected marks a procedure that failed or misled.
	PlaybookOutcomeRejected PlaybookOutcome = "rejected"
)

// PlaybookConfirmed signals a successful playbook use. Like the fact signal
// events it is recorded on the playbook's aggregate in the same UnitOfWork
// commit as the counter-bearing ResourceUpdated — the counters in the
// resource's data are the derived state, the signal is the audit trail.
type PlaybookConfirmed struct {
	PlaybookID string
	Note       string
	Timestamp  time.Time
}

func (e PlaybookConfirmed) EventType() string {
	return "Playbook.Confirmed"
}

// PlaybookRejected signals a failed playbook use.
type PlaybookRejected struct {
	PlaybookID string
	Note       string
	Timestamp  time.Time
}

func (e PlaybookRejected) EventType() string {
	return "Playbook.Rejected"
}

type playbookOutcomeCtxKey struct{}

type playbookOutcomeCtxValue struct {
	outcome PlaybookOutcome
	note    string
}

// WithPlaybookOutcome marks the context so the playbook behavior records the
// matching signal event during the update commit. Set by
// PlaybookService.RecordOutcome — plain resource edits carry no marker and
// record no signal.
func WithPlaybookOutcome(ctx context.Context, outcome PlaybookOutcome, note string) context.Context {
	return context.WithValue(ctx, playbookOutcomeCtxKey{}, playbookOutcomeCtxValue{outcome: outcome, note: note})
}

// PlaybookOutcomeFromCtx returns the outcome marker set by WithPlaybookOutcome.
func PlaybookOutcomeFromCtx(ctx context.Context) (PlaybookOutcome, string, bool) {
	v, ok := ctx.Value(playbookOutcomeCtxKey{}).(playbookOutcomeCtxValue)
	if !ok {
		return "", "", false
	}
	return v.outcome, v.note, true
}
