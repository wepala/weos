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
	"net/http"

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

// CodeImpersonationTargetNotMember names the refusal of an impersonation whose
// person is not a member of the account the caller acts in. A person who does
// not exist gets the same code, so the refusal says nothing about who has an
// identity on the instance outside that account (wm-ptcuk).
const CodeImpersonationTargetNotMember = "impersonation_target_not_member"

type impersonatorKey struct{}

// ImpersonatorFromCtx returns the identity Impersonation replaced — the person
// who is really signed in — or nil when no impersonation applies to the
// request.
func ImpersonatorFromCtx(ctx context.Context) *auth.Identity {
	identity, _ := ctx.Value(impersonatorKey{}).(*auth.Identity)
	return identity
}

// ImpersonationAccount is the account an impersonation by caller is judged in
// and acts in: the caller's active account, or, for an identity that names
// none, the first account the caller belongs to, which is how GetUserRole
// resolves the caller's role. It is "" when the caller belongs to no account.
func ImpersonationAccount(ctx context.Context, accounts authrepos.AccountRepository, caller *auth.Identity) (string, error) {
	if caller == nil {
		return "", nil
	}
	if caller.ActiveAccountID != "" {
		return caller.ActiveAccountID, nil
	}
	memberships, err := accounts.FindByMember(ctx, caller.AgentID)
	if err != nil {
		return "", fmt.Errorf("failed to find member accounts: %w", err)
	}
	for _, account := range memberships {
		if account != nil {
			return account.GetID(), nil
		}
	}
	return "", nil
}

// IsMember reports whether agentID holds any role in accountID.
func IsMember(ctx context.Context, accounts authrepos.AccountRepository, accountID, agentID string) (bool, error) {
	if accountID == "" || agentID == "" {
		return false, nil
	}
	role, err := accounts.FindMemberRole(ctx, accountID, agentID)
	if err != nil {
		return false, err
	}
	return role != "", nil
}

// MayImpersonate reports whether callerID may act as targetID in accountID:
// the caller holds the owner or admin role there, and the target is a member
// there. Holding that role in some other account grants nothing here.
func MayImpersonate(ctx context.Context, accounts authrepos.AccountRepository, accountID, callerID, targetID string) (bool, error) {
	if accountID == "" || callerID == "" || targetID == "" || callerID == targetID {
		return false, nil
	}
	admin, err := IsOwnerOrAdmin(ctx, accounts, accountID, callerID)
	if err != nil || !admin {
		return false, err
	}
	return IsMember(ctx, accounts, accountID, targetID)
}

// Impersonation returns Echo middleware that checks for an active impersonation
// session and, if present, replaces the auth.Identity in the request context
// with the impersonated person's identity, acting in the caller's account.
//
// The cookie is not trusted on its own. On every request the caller must still
// hold the owner or admin role in the account they act in, and the person
// impersonated must still be a member of it (wm-ptcuk). The impersonation acts
// in that account, never in some other account the person belongs to: the
// caller's authority reaches no further. A cookie that no longer passes —
// one started before this check existed, or whose person has since left the
// account — is cleared and the request refused with
// CodeImpersonationTargetNotMember, rather than served as either person.
//
// The account is refused with the code that says why when it is not active,
// so an impersonation never opens an account locked for deletion or a
// suspended one (wm-iiasy).
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
			accountID, err := ImpersonationAccount(ctx, accountRepo, currentIdentity)
			if err != nil {
				logger.Error(ctx, "impersonation: could not resolve the caller's account", "agent_id", realAgentID, "error", err)
				return unreadableAccountState(c)
			}
			allowed, err := MayImpersonate(ctx, accountRepo, accountID, realAgentID, impersonatedAgentID)
			if err != nil {
				logger.Error(ctx, "impersonation: could not read the roles in the caller's account",
					"account_id", accountID, "admin_agent_id", realAgentID, "target_agent_id", impersonatedAgentID, "error", err)
				return unreadableAccountState(c)
			}
			if !allowed {
				logger.Warn(ctx, "impersonation refused: the person is not a member of the caller's account, or the caller's role there does not allow it",
					"account_id", accountID, "admin_agent_id", realAgentID, "target_agent_id", impersonatedAgentID, "ip", c.RealIP())
				ExpireImpersonationCookie(c.Response())
				return c.JSON(http.StatusForbidden, map[string]string{
					"error": "impersonation not allowed",
					"code":  CodeImpersonationTargetNotMember,
				})
			}

			state, err := stateOfAccount(ctx, accountID, accountRepo, locks)
			if err != nil {
				logger.Error(ctx, "impersonation: could not read the account's state", "account_id", accountID, "error", err)
				return unreadableAccountState(c)
			}
			switch state {
			case accountErasurePending:
				return refuse(c, CodeAccountErasurePending)
			case accountSuspended:
				return refuse(c, CodeAccountDeactivated)
			case accountGone:
				return refuse(c, "")
			case accountActive:
			}

			impersonatedIdentity := &auth.Identity{
				AgentID:         impersonatedAgentID,
				AccountIDs:      []string{accountID},
				ActiveAccountID: accountID,
			}
			ctx = context.WithValue(ctx, impersonatorKey{}, currentIdentity)
			ctx = auth.ContextWithAgent(ctx, impersonatedIdentity)
			c.SetRequest(c.Request().WithContext(ctx))

			c.Response().Header().Set("X-Impersonating", impersonatedAgentID)

			return next(c)
		}
	}
}

// ExpireImpersonationCookie expires the impersonation cookie on w. It writes
// the same cookie the session store writes for an ended session, so it needs
// no store and cannot fail. Every path that ends an impersonation uses it: a
// refusal here, the stop route, the identity read, and sign-out (wm-1yjuv).
func ExpireImpersonationCookie(w http.ResponseWriter) {
	http.SetCookie(w, sessions.NewCookie(ImpersonationSessionName, "", &sessions.Options{
		MaxAge:   -1,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}))
}

// unreadableAccountState fails closed when the roles or state of the account
// could not be read, with the answer the bearer path gives.
func unreadableAccountState(c echo.Context) error {
	return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "could not read the account's state"})
}
