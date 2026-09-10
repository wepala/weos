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

package repositories

import (
	"context"
	"errors"
)

// ErrNothingToPurge is what Purge answers for an account whose row is gone
// and that nothing else names: a second deletion that arrived beside the
// first, or a re-run of one that already finished. The caller reports the
// account as not found.
var ErrNothingToPurge = errors.New("account erasure: nothing of the account remains to purge")

// AccountErasureLocks records which accounts are part-way through an erasure.
//
// The lock is what tells a deletion that failed after the account was
// deactivated apart from an account an operator suspended: both are inactive
// in pericarp's terms, but only the first may be signed in to — for the one
// purpose of running the deletion again — and every refusal it produces has
// to say so, because an app offers "finish deleting" for one and nothing for
// the other. The row is written before anything is removed and goes in the
// same transaction as the account row, so a crash anywhere between leaves it
// in place.
type AccountErasureLocks interface {
	// Lock records that accountID's erasure has begun. Locking an account
	// that is already locked is not an error: a re-run takes the same lock.
	Lock(ctx context.Context, accountID, requestedBy string) error

	// IsLocked reports whether an erasure of accountID has begun and not
	// finished.
	IsLocked(ctx context.Context, accountID string) (bool, error)
}

// AccountEnumeration is the SQL state that names an account, read before any
// store is touched so the external deletes and the graph drop work from the
// same facts the SQL purge will.
type AccountEnumeration struct {
	// ResourceURNs are the ids of every resource row the account owns. They
	// are the graph subjects to drop and the triple subjects to delete.
	ResourceURNs []string
	// Members is how many people belong to the account — what the answer to
	// a deletion reports as the number who lost it.
	Members int
}

// PurgeReport is what the SQL purge removed.
type PurgeReport struct {
	// Members is how many memberships the account had when it was deleted.
	Members int
	// Events is how many event rows were deleted.
	Events int
	// Resources is how many canonical resource rows were deleted.
	Resources int
	// DeletedAgents lists the agents removed with the account: the members
	// who belonged to nothing else.
	DeletedAgents []string
	// Groupings lists the role assignments in the account that the purge
	// deleted from the authorization table, so the caller can revoke them
	// from the running enforcer's copy as well.
	Groupings []AccountGrouping
}

// AccountGrouping is one role an agent held in the account being erased.
type AccountGrouping struct {
	AgentID string
	RoleID  string
}

// AccountDataPurger enumerates and deletes every SQL row an account owns.
//
// It is the one place in the tree that deletes event rows. Article I of the
// constitution names account erasure as its single exception, and confines it
// here: nothing reachable from a unit of work can call Purge, and no handler
// or service other than the erasure service holds this port.
type AccountDataPurger interface {
	// Enumerate reads what the account owns without changing anything.
	Enumerate(ctx context.Context, accountID string) (*AccountEnumeration, error)

	// Purge deletes every SQL row that belongs to the account, in one
	// transaction, with the account row last. Every step is idempotent, so a
	// second run after a failure finishes what the first started. The event
	// sweep takes only events of aggregates being deleted: the account, its
	// resources, and the auth aggregates of the members who belonged to
	// nothing else. An event of a surviving aggregate that shares a
	// transaction with one of those is left alone. When the account row is
	// already gone and nothing names the account, it answers
	// ErrNothingToPurge; when the row is gone but rows naming the account
	// remain — a request admitted before the lock that committed after the
	// purge — it purges those.
	Purge(ctx context.Context, accountID string) (*PurgeReport, error)

	// Remains reports whether anything still names the account: its row,
	// events, resources, memberships or invites. The erasure asks it after
	// the purge, and again for an account whose row is gone, so a row that
	// landed late is swept rather than stranded.
	Remains(ctx context.Context, accountID string) (bool, error)
}
