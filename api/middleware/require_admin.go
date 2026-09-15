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

package middleware

import (
	"context"
	"fmt"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
)

// GetUserRole returns the authenticated user's role in their active account.
// If no active account is set (legacy sessions), falls back to the first account
// the user belongs to. Returns ("", nil) when no identity is present or the user
// has no accounts. Returns a non-nil error only on database/infrastructure failures.
func GetUserRole(ctx context.Context, accountRepo authrepos.AccountRepository) (string, error) {
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return "", nil
	}

	accountID := identity.ActiveAccountID
	if accountID == "" {
		// Fallback: find the first account this agent belongs to.
		accounts, err := accountRepo.FindByMember(ctx, identity.AgentID)
		if err != nil {
			return "", fmt.Errorf("failed to find member accounts: %w", err)
		}
		if len(accounts) == 0 {
			return "", nil
		}
		accountID = accounts[0].GetID()
	}

	role, err := accountRepo.FindMemberRole(ctx, accountID, identity.AgentID)
	if err != nil {
		return "", fmt.Errorf("failed to find member role: %w", err)
	}
	return role, nil
}

// IsAdmin checks whether the authenticated user has an admin or owner role
// in their active account. Returns (false, error) on infrastructure failures
// so callers can distinguish "not admin" from "cannot determine".
func IsAdmin(ctx context.Context, accountRepo authrepos.AccountRepository) (bool, error) {
	role, err := GetUserRole(ctx, accountRepo)
	if err != nil {
		return false, err
	}
	return role == authentities.RoleOwner || role == authentities.RoleAdmin, nil
}
