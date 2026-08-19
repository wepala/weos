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

import "context"

// FeatureCacheInvalidator drops resolved feature sets so a change reaches
// people who are already signed in.
//
// There is one rule and it has no exceptions: a change to the instance, to an
// account, or to a grant reaches sessions that are already open. What varies
// between deployments is only the machinery behind this interface, never the
// behaviour in front of it — tie the two together and a bug that only appears
// under replicas never shows up on the single-process instance it was
// developed on.
//
// Two implementations:
//
//   - In process. Evicts from the local cache. On SQLite this is the complete
//     and exact implementation, not a fallback: SQLite is single-process with
//     serialized writers, so there is no second cache to reach.
//   - Postgres. Evicts locally, then broadcasts on a NOTIFY channel so other
//     replicas evict too. Deliberately NOT a checkpointed subscriber group:
//     those lock their checkpoint row FOR UPDATE SKIP LOCKED and behave as
//     competing consumers, so exactly one replica would process each event and
//     every other replica would serve a stale answer indefinitely.
//
// None of this ever ends a session. Dropping what a session resolved costs its
// next evaluation one database read; the person stays signed in. Ending a
// session is a different act, belonging to a different feature.
//
// Invalidation is always called explicitly at the write site. Subscribing to
// events would not be enough on its own — a member's role change is a direct
// projection write that emits no event, and resolution depends on the caller's
// role.
type FeatureCacheInvalidator interface {
	// InvalidateAll drops every cached set. Used for an instance-level change,
	// and by a replica told that something changed elsewhere.
	InvalidateAll(ctx context.Context)

	// InvalidateAccount drops every cached set belonging to one account.
	InvalidateAccount(ctx context.Context, accountID string)

	// InvalidateAgents drops the cached sets of specific people within one
	// account. Variadic because the expensive case — a role grant changing —
	// invalidates every member holding that role, and doing so in one call
	// keeps the fan-out at the call site where the member list was read.
	InvalidateAgents(ctx context.Context, accountID string, agentIDs ...string)
}
