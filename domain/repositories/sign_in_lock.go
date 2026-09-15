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

// SignInLock serializes the sign-ins that write a credential, in one process
// and across every process that shares one database.
//
// Owner binding looks up who holds an email and then links or creates a
// person. Two replicas that both find no holder would each create a person, and
// the two proving credentials that leaves make every later sign-in for the
// email ambiguous. The OAuth callbacks write google and apple credentials
// through FindOrCreateAgent, which binding reads, and password registration
// creates a person through RegisterPassword, so they hold the same keys.
// pericarp's FindOrCreateAgent writes its rows outside any transaction core can
// join, so the look-up and the create cannot share one; a lock held across both
// is what keeps the second sign-in from reading before the first one's rows are
// written.
type SignInLock interface {
	// Hold blocks until this process holds every key, taken in the order given,
	// and returns the function that releases them all. The keys must differ from
	// each other. It returns an error, and holds nothing, when a key cannot be
	// taken, for example because ctx ended: every wait ends with ctx, and a ctx
	// that has ended holds nothing even when every key is free.
	Hold(ctx context.Context, keys ...string) (release func(), err error)
}
