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

package cli

import (
	"context"
	"net/http"
	"testing"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
)

// wm-qhda1. A role change that would leave the caller's account with no owner
// is refused: nobody could then manage the account's people or invites. These
// run through buildServer, on the account scope of newUsersScope.

// lastOwnerRequired is the code a refused demotion of the last owner carries.
const lastOwnerRequired = "last_owner_required"

// The only owner of an account cannot demote themselves, and nothing about them
// changes, their name included.
func TestServe_UsersUpdateRefusesToDemoteTheOnlyOwnerOfTheirOwnAccount(t *testing.T) {
	s := newUsersScope(t)
	nameBefore := s.nameOf(t, s.owner.agentID)

	answer := s.call(t, http.MethodPut, "/api/users/"+s.owner.agentID,
		`{"name":"Harbor Former Owner","role":"member"}`, s.owner)
	if answer.status != http.StatusBadRequest || usersRefusalCode(t, answer) != lastOwnerRequired {
		t.Errorf("the only owner demoting themselves answered %d %s; want 400 %s", answer.status, answer.body, lastOwnerRequired)
	}
	if role := s.roleIn(t, s.owner.accountID, s.owner.agentID); role != authentities.RoleOwner {
		t.Errorf("after the refusal the owner holds %q, want owner", role)
	}
	if name := s.nameOf(t, s.owner.agentID); name != nameBefore {
		t.Errorf("after the refusal the owner is named %q, want %q", name, nameBefore)
	}
}

// An admin of an account cannot demote the account's only owner.
func TestServe_UsersUpdateRefusesToDemoteAnotherOnlyOwner(t *testing.T) {
	s := newUsersScope(t)
	if err := s.accounts.SaveMember(context.Background(), s.owner.accountID, s.member.agentID, authentities.RoleAdmin); err != nil {
		t.Fatalf("make the member an admin: %v", err)
	}

	answer := s.call(t, http.MethodPut, "/api/users/"+s.owner.agentID, `{"role":"admin"}`, s.member)
	if answer.status != http.StatusBadRequest || usersRefusalCode(t, answer) != lastOwnerRequired {
		t.Errorf("an admin demoting the only owner answered %d %s; want 400 %s", answer.status, answer.body, lastOwnerRequired)
	}
	if role := s.roleIn(t, s.owner.accountID, s.owner.agentID); role != authentities.RoleOwner {
		t.Errorf("after the refusal the owner holds %q, want owner", role)
	}
}

// The rule keeps one owner and no more: while another owner remains, an owner
// can step down.
func TestServe_UsersUpdateLetsAnOwnerStepDownWhileAnotherOwnerRemains(t *testing.T) {
	s := newUsersScope(t)
	if err := s.accounts.SaveMember(context.Background(), s.owner.accountID, s.member.agentID, authentities.RoleOwner); err != nil {
		t.Fatalf("make the member a second owner: %v", err)
	}

	answer := s.call(t, http.MethodPut, "/api/users/"+s.owner.agentID, `{"role":"admin"}`, s.owner)
	if answer.status != http.StatusOK {
		t.Fatalf("an owner stepping down beside a second owner answered %d %s, want 200", answer.status, answer.body)
	}
	if role := s.roleIn(t, s.owner.accountID, s.owner.agentID); role != authentities.RoleAdmin {
		t.Errorf("after stepping down the first owner holds %q, want admin", role)
	}
	if role := s.roleIn(t, s.owner.accountID, s.member.agentID); role != authentities.RoleOwner {
		t.Errorf("after the change the second owner holds %q, want owner", role)
	}
}
