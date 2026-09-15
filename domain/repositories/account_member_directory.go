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

// The page sizes of an account's member list (wm-g7284). An account can hold
// thousands of people, so no request reads all of them at once.
const (
	// DefaultMemberPageSize is the page size when the client names no limit.
	DefaultMemberPageSize = 100
	// MaxMemberPageSize is the largest page a client can ask for; a larger
	// limit is cut to it.
	MaxMemberPageSize = 500
)

// AccountMemberDirectory lists the people of one account for the users routes
// (wm-govvg), so an owner or admin sees the members of their own account and
// nobody else on the instance.
//
// It is its own port, not a method on AccountMemberQuery, because
// AccountMemberQuery is published: adding a method to it would stop every
// existing implementation outside this module from compiling (wm-9wslp). Like
// AccountMemberQuery, it is a read-only port over tables pericarp owns.
type AccountMemberDirectory interface {
	// ListMembers returns one page of the members of accountID, ordered by
	// agent ID, each with the role they hold there and the person's own
	// record. cursor is the Cursor of the previous page, or "" for the first
	// page. A limit of zero or less means DefaultMemberPageSize, and a limit
	// above MaxMemberPageSize is cut to it. A page costs the same number of
	// queries whatever it holds. An account with no members is an empty last
	// page, not an error.
	ListMembers(ctx context.Context, accountID, cursor string, limit int) (*AccountMemberPage, error)

	// CountMembersWithRole returns how many people hold roleID in accountID.
	// The users routes read it to refuse a role change that would leave an
	// account with no owner (wm-qhda1).
	CountMembersWithRole(ctx context.Context, accountID, roleID string) (int, error)
}

// AccountMemberPage is one page of an account's members.
type AccountMemberPage struct {
	Members []AccountMember
	// Cursor is the agent ID this page ended at, to pass for the next page.
	// It is "" on the last page.
	Cursor  string
	HasMore bool
}

// AccountMember is one person's membership of an account, the role they hold
// in it, and their own record.
type AccountMember struct {
	AgentID string
	RoleID  string
	// HasRecord is false for a membership whose person record is gone. The
	// fields below are then empty.
	HasRecord bool
	Name      string
	Status    string
	// Email is the address on the person's earliest credential that carries
	// one, or "" when none does.
	Email string
}
