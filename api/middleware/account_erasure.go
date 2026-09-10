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
	"errors"
	"net/http"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/labstack/echo/v4"
)

// Refusal codes a client tells apart. The first three are pericarp's, written
// by RequireAuth; the fourth is this package's, and it is the one an app
// answers with "finish deleting your account".
const (
	CodeUnscopedSession       = "unscoped_session"
	CodeAccountAccessRevoked  = "account_access_revoked"
	CodeAccountDeactivated    = "account_deactivated"
	CodeAccountErasurePending = "account_erasure_pending"
)

type erasureLockedKey struct{}

// ErasureLocked reports whether SessionAuthForErasure admitted the request
// into an account whose erasure is unfinished.
func ErasureLocked(ctx context.Context) bool {
	locked, _ := ctx.Value(erasureLockedKey{}).(bool)
	return locked
}

// ErasureGuard refuses, before any other authentication runs, every request
// whose session cookie names an account whose erasure has begun and not
// finished. It answers 401 with the code that names the unfinished deletion.
//
// It sits in front of pericarp's RequireAuth because RequireAuth cannot tell
// the lock from a suspension: both are an inactive account, and it answers
// account_deactivated for either. An app told account_deactivated offers
// nothing; told account_erasure_pending it offers "finish deleting", which is
// the one thing a locked account may still do — on the deletion route, which
// is not mounted behind this guard.
//
// It decides from the cookie alone, before the session is validated. Refusing
// is the fail-closed direction, and the cookie is signed, so a forged one
// cannot pass the session manager in the first place. A request carrying a
// bearer token is left to BearerOrSession, which makes the same check on the
// token's account.
func ErasureGuard(
	sm session.SessionManager,
	locks repositories.AccountErasureLocks,
	logger entities.Logger,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if extractBearer(c.Request()) != "" {
				return next(c)
			}
			data, err := sm.GetHTTPSession(c.Request())
			if err != nil || data == nil || data.AccountID == "" {
				return next(c)
			}
			locked, err := locks.IsLocked(c.Request().Context(), data.AccountID)
			if err != nil {
				// Fail closed: a lock that cannot be read must not let a
				// request into an account that may be half-deleted.
				logger.Error(c.Request().Context(), "could not read the erasure lock", "account_id", data.AccountID, "error", err)
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "could not read the account's state"})
			}
			if locked {
				return refuseErasurePending(c)
			}
			return next(c)
		}
	}
}

// SessionAuthForErasure authenticates the account deletion route. It makes
// the checks pericarp's RequireAuth makes and answers the same codes, with
// one admission RequireAuth cannot make: a session scoped to an account whose
// erasure is unfinished is let through, so the person can run the deletion
// again. Nothing else admits such a session; ErasureGuard refuses it on
// every other route.
//
// A session that names no account is refused as RequireAuth refuses it, but
// the code says why the person has no account when the reason is something
// the app should show: their only account is suspended
// (account_deactivated), or they are a plain member of an account whose
// erasure is unfinished (account_erasure_pending). Otherwise it is
// unscoped_session, as everywhere else.
func SessionAuthForErasure(
	sm session.SessionManager,
	as authapp.AuthenticationService,
	accounts authrepos.AccountRepository,
	locks repositories.AccountErasureLocks,
	logger entities.Logger,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			r := c.Request()
			ctx := r.Context()
			data, err := sm.GetHTTPSession(r)
			if err != nil || data == nil {
				return refuse(c, "")
			}
			info, err := as.ValidateSession(ctx, data.SessionID)
			if err != nil {
				switch {
				case errors.Is(err, authapp.ErrSessionAccountRevoked):
					return refuse(c, CodeAccountAccessRevoked)
				case errors.Is(err, authapp.ErrSessionAccountDeactivated):
					locked, lockErr := locks.IsLocked(ctx, data.AccountID)
					if lockErr != nil {
						logger.Error(ctx, "could not read the erasure lock", "account_id", data.AccountID, "error", lockErr)
						return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "could not read the account's state"})
					}
					if !locked {
						return refuse(c, CodeAccountDeactivated)
					}
					// The membership was verified when the session was made,
					// and the account is inactive only because its erasure
					// began. Admit the person into it for the one thing it
					// still serves.
					admit(c, &auth.Identity{AgentID: data.AgentID, AccountIDs: []string{data.AccountID}, ActiveAccountID: data.AccountID}, true)
					return next(c)
				default:
					return refuse(c, "")
				}
			}
			if info.AccountID == "" {
				return refuse(c, unscopedCode(ctx, info.AgentID, accounts, locks, logger))
			}
			accountIDs := info.AccountIDs
			if len(accountIDs) == 0 {
				accountIDs = []string{info.AccountID}
			}
			admit(c, &auth.Identity{AgentID: info.AgentID, AccountIDs: accountIDs, ActiveAccountID: info.AccountID}, false)
			return next(c)
		}
	}
}

// unscopedCode says why a session names no account, when the reason is one
// the app should show instead of a bare sign-in prompt.
func unscopedCode(
	ctx context.Context, agentID string,
	accounts authrepos.AccountRepository, locks repositories.AccountErasureLocks, logger entities.Logger,
) string {
	memberships, err := accounts.FindByMember(ctx, agentID)
	if err != nil {
		logger.Warn(ctx, "could not read memberships for an unscoped session", "agent_id", agentID, "error", err)
		return CodeUnscopedSession
	}
	suspended := false
	for _, account := range memberships {
		if account == nil || account.Active() {
			continue
		}
		locked, lockErr := locks.IsLocked(ctx, account.GetID())
		if lockErr != nil {
			logger.Warn(ctx, "could not read the erasure lock", "account_id", account.GetID(), "error", lockErr)
			continue
		}
		if locked {
			return CodeAccountErasurePending
		}
		suspended = true
	}
	if suspended {
		return CodeAccountDeactivated
	}
	return CodeUnscopedSession
}

func admit(c echo.Context, identity *auth.Identity, locked bool) {
	ctx := auth.ContextWithAgent(c.Request().Context(), identity)
	if locked {
		ctx = context.WithValue(ctx, erasureLockedKey{}, true)
	}
	c.SetRequest(c.Request().WithContext(ctx))
}

// refuse writes the 401 RequireAuth writes: "not authenticated", with a code
// only when there is one to give.
func refuse(c echo.Context, code string) error {
	body := map[string]string{"error": "not authenticated"}
	if code != "" {
		body["code"] = code
	}
	return c.JSON(http.StatusUnauthorized, body)
}

func refuseErasurePending(c echo.Context) error {
	return refuse(c, CodeAccountErasurePending)
}

// accountState is what the bearer path learns about a token's account.
type accountState int

const (
	accountActive accountState = iota
	accountGone
	accountSuspended
	accountErasurePending
)

// stateOfAccount makes the one lookup the session path always made, for the
// bearer path: whether the token's account still exists, and if it is
// inactive, whether that is a suspension or an unfinished erasure.
func stateOfAccount(
	ctx context.Context, accountID string,
	accounts authrepos.AccountRepository, locks repositories.AccountErasureLocks,
) (accountState, error) {
	account, err := accounts.FindByID(ctx, accountID)
	if err != nil {
		return accountGone, err
	}
	if account == nil {
		return accountGone, nil
	}
	if account.Active() {
		return accountActive, nil
	}
	locked, err := locks.IsLocked(ctx, accountID)
	if err != nil {
		return accountSuspended, err
	}
	if locked {
		return accountErasurePending, nil
	}
	return accountSuspended, nil
}

// IsOwnerOrAdmin reports whether agentID holds the owner or admin role in
// accountID — the roles that may delete the account.
func IsOwnerOrAdmin(ctx context.Context, accounts authrepos.AccountRepository, accountID, agentID string) (bool, error) {
	role, err := accounts.FindMemberRole(ctx, accountID, agentID)
	if err != nil {
		return false, err
	}
	return role == authentities.RoleOwner || role == authentities.RoleAdmin, nil
}
