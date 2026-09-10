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
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const (
	ImpersonationSessionName = "weos-impersonation"
	KeyImpersonatedAgentID   = "impersonated_agent_id"
	KeyRealAgentID           = "real_agent_id"
	KeyRealAccountID         = "real_account_id"
)

// Impersonation returns Echo middleware that checks for an active impersonation
// session and, if present, replaces the auth.Identity in the request context
// with the impersonated user's identity (including their account).
//
// The account it lands in is the person's first ACTIVE one, the way pericarp's
// own sign-in resolves it. A person whose accounts are all inactive is not
// impersonated into one of them: an account locked for deletion serves nothing
// but the deletion, and a suspended one serves nothing at all, so the request
// is refused with the code that says which (wm-iiasy). A person with no
// account is impersonated with none, as before.
func Impersonation(
	store sessions.Store,
	accountRepo authrepos.AccountRepository,
	locks repositories.AccountErasureLocks,
	logger entities.Logger,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sess, err := store.Get(c.Request(), ImpersonationSessionName)
			if err != nil {
				logger.Warn(c.Request().Context(), "impersonation session read error, passing through", "error", err)
				return next(c)
			}

			impersonatedAgentID, ok := sess.Values[KeyImpersonatedAgentID].(string)
			if !ok || impersonatedAgentID == "" {
				return next(c)
			}

			realAgentID, _ := sess.Values[KeyRealAgentID].(string)

			currentIdentity := auth.AgentFromCtx(c.Request().Context())
			if currentIdentity == nil {
				return next(c)
			}

			// Only apply impersonation if the real session matches the admin who started it.
			if currentIdentity.AgentID != realAgentID {
				return next(c)
			}

			ctx := c.Request().Context()
			activeAccountID := ""
			accounts, err := accountRepo.FindByMember(ctx, impersonatedAgentID)
			if err != nil {
				logger.Warn(ctx, "impersonation: could not read the person's memberships", "agent_id", impersonatedAgentID, "error", err)
			}
			for _, account := range accounts {
				if account != nil && account.Active() {
					activeAccountID = account.GetID()
					break
				}
			}
			if activeAccountID == "" && len(accounts) > 0 {
				switch code := unscopedCode(ctx, impersonatedAgentID, accountRepo, locks, logger); code {
				case CodeAccountErasurePending, CodeAccountDeactivated:
					return refuse(c, code)
				}
			}

			impersonatedIdentity := &auth.Identity{
				AgentID:         impersonatedAgentID,
				AccountIDs:      []string{activeAccountID},
				ActiveAccountID: activeAccountID,
			}
			ctx = auth.ContextWithAgent(ctx, impersonatedIdentity)
			c.SetRequest(c.Request().WithContext(ctx))

			c.Response().Header().Set("X-Impersonating", impersonatedAgentID)

			return next(c)
		}
	}
}
