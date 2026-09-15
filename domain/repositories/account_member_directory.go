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

// AccountMemberDirectory lists the people of one account for the users routes
// (wm-govvg), so an owner or admin sees the members of their own account and
// nobody else on the instance.
//
// It is its own port, not a method on AccountMemberQuery, because
// AccountMemberQuery is published: adding a method to it would stop every
// existing implementation outside this module from compiling (wm-9wslp). Like
// AccountMemberQuery, it is a read-only port over tables pericarp owns.
type AccountMemberDirectory interface {
	// ListMembers returns every membership of accountID with the role it
	// carries, ordered by agent ID. An account with no members is not an
	// error.
	ListMembers(ctx context.Context, accountID string) ([]AccountMembership, error)
}

// AccountMembership is one person's membership of an account and the role
// they hold in it.
type AccountMembership struct {
	AgentID string
	RoleID  string
}
