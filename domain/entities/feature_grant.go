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

import "time"

// Window states a grant can be in, for listings.
const (
	GrantActive  = "active"
	GrantPending = "pending"
	GrantExpired = "expired"
)

// FeatureGrantRecord is one stored grant, as resolution and listings read it.
//
// The subject is an agent id or a role id — never an email. An address is how
// an admin names somebody when granting; it is not what the grant is against,
// because an address can change and the grant should not follow it. Listings
// resolve the address back at read time.
type FeatureGrantRecord struct {
	SubjectType string
	SubjectID   string
	AccountID   string
	FeatureKey  string

	// ValidFrom and ValidThrough bound the grant. Nil means unbounded on that
	// side. The window is half-open: the grant is on at the instant ValidFrom
	// arrives and off at the instant ValidThrough passes.
	ValidFrom    *time.Time
	ValidThrough *time.Time

	// Provenance. Kept on the row as well as in the event log because the two
	// answer different questions: the row says who granted a right that still
	// exists, and survives to be listed; the event log says what happened, and
	// survives revocation, which deletes the row.
	GrantedByID    string
	GrantedByEmail string
	Source         string
	CreatedAt      time.Time
}

// Active reports whether the grant applies at now.
func (g FeatureGrantRecord) Active(now time.Time) bool {
	if g.ValidFrom != nil && now.Before(*g.ValidFrom) {
		return false
	}
	if g.ValidThrough != nil && !now.Before(*g.ValidThrough) {
		return false
	}
	return true
}

// WindowState describes the grant for an operator reading a listing. An
// expired grant is still a row — revocation deletes, a closed window does not —
// so a listing has to be able to say which it is looking at.
func (g FeatureGrantRecord) WindowState(now time.Time) string {
	switch {
	case g.ValidFrom != nil && now.Before(*g.ValidFrom):
		return GrantPending
	case g.ValidThrough != nil && !now.Before(*g.ValidThrough):
		return GrantExpired
	default:
		return GrantActive
	}
}

// FoldGrants reduces a caller's grant rows to the set of features they hold
// right now, and to the next instant at which that answer changes on its own.
//
// The second return is what lets a grant expire to the second without shrinking
// the cache. A resolved feature set is cached and reused for every later
// evaluation in a session — thirty in an agent turn is ordinary — so a window
// that closes between two of them would otherwise go unnoticed until the whole
// entry aged out. Instead the entry remembers the earliest boundary among the
// grants that fed it and treats itself as stale at that instant. Nothing polls,
// nothing is scheduled, and the cost is paid lazily by whoever evaluates next.
//
// The alternative — tuning the maximum cache age down to whatever precision a
// window needs — would throw away the caching that exists for the other three
// hundred and sixty evaluations.
//
// Only boundaries strictly after now are returned, so a set is never born
// stale. A row already expired contributes nothing: it will not change again.
func FoldGrants(records []FeatureGrantRecord, now time.Time) (map[string]bool, time.Time) {
	active := make(map[string]bool, len(records))
	var next time.Time

	consider := func(t *time.Time) {
		if t == nil || !t.After(now) {
			return
		}
		if next.IsZero() || t.Before(next) {
			next = *t
		}
	}

	for _, r := range records {
		if r.Active(now) {
			active[r.FeatureKey] = true
			// A live grant stops being live when its window closes.
			consider(r.ValidThrough)
			continue
		}
		// A grant that has not started yet begins on its own, with nothing
		// written and nobody signed in again.
		consider(r.ValidFrom)
	}
	return active, next
}
