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
	"bytes"
	"context"
	"encoding/json"
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

// ErasureGuardOption tunes ErasureGuard for the group it guards.
type ErasureGuardOption func(*erasureGuardConfig)

type erasureGuardConfig struct {
	deferToBearer bool
}

// DeferToBearer leaves a request that carries a bearer token alone. It is for
// a group that authenticates bearers: there BearerOrSession makes this check
// on the token's account itself, and the cookie beside a bearer says nothing
// about the account the token names.
func DeferToBearer() ErasureGuardOption {
	return func(c *erasureGuardConfig) { c.deferToBearer = true }
}

// ErasureGuard makes a refusal for an account whose erasure has begun and not
// finished say so. pericarp's RequireAuth cannot tell the lock from a
// suspension — both are an inactive account, and it answers
// account_deactivated for either — and an app told account_deactivated
// offers nothing, while told account_erasure_pending it offers "finish
// deleting", the one thing a locked account may still do (on the deletion
// route, which is not mounted behind this guard).
//
// It costs a healthy request nothing (wm-tsugz). It runs the chain below it
// with the response held back only when that chain writes a 401, and then,
// only when the refusal's code is account_deactivated, reads the cookie's
// account and asks the lock repository whether the deletion is what made it
// inactive; a locked account's refusal is rewritten with the erasure code
// before it is sent. Every other response passes through untouched, streams
// included. A lock that cannot be read answers 503 with Retry-After: a
// request into an account that may be half-deleted must not be let through on
// a guess.
//
// Because it judges RequireAuth's refusal rather than the request, a bearer
// header beside the cookie changes nothing on a group that authenticates by
// session (wm-6umqn); a group that authenticates bearers passes
// DeferToBearer.
func ErasureGuard(
	sm session.SessionManager,
	locks repositories.AccountErasureLocks,
	logger entities.Logger,
	opts ...ErasureGuardOption,
) echo.MiddlewareFunc {
	cfg := erasureGuardConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if cfg.deferToBearer && extractBearer(c.Request()) != "" {
				return next(c)
			}
			res := c.Response()
			real := res.Writer
			held := &heldRefusal{ResponseWriter: real}
			res.Writer = held
			err := next(c)
			res.Writer = real
			if !held.holding {
				return err
			}
			body := held.body.Bytes()
			if refusalCodeOf(body) == CodeAccountDeactivated {
				if data, sessErr := sm.GetHTTPSession(c.Request()); sessErr == nil && data != nil && data.AccountID != "" {
					ctx := c.Request().Context()
					locked, lockErr := locks.IsLocked(ctx, data.AccountID)
					if lockErr != nil {
						logger.Error(ctx, "could not read the erasure lock", "account_id", data.AccountID, "error", lockErr)
						real.Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
						real.Header().Set("Retry-After", accountStateRetryAfter)
						real.WriteHeader(http.StatusServiceUnavailable)
						_, _ = real.Write([]byte(`{"error":"could not read the account's state"}`))
						return err
					}
					if locked {
						body = withRefusalCode(body, CodeAccountErasurePending)
					}
				}
			}
			real.WriteHeader(held.status)
			_, _ = real.Write(body)
			return err
		}
	}
}

// heldRefusal passes every response through as it is written, except a 401,
// whose body it holds until the chain returns so the guard can read the code
// in it. A streaming response is never a 401, so it is never held.
type heldRefusal struct {
	http.ResponseWriter
	status  int
	holding bool
	body    bytes.Buffer
}

func (w *heldRefusal) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	if code == http.StatusUnauthorized {
		w.holding = true
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *heldRefusal) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.holding {
		return w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// Flush passes through for a streaming response; a held refusal is flushed
// when the guard sends it.
func (w *heldRefusal) Flush() {
	if w.holding {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the writer underneath.
func (w *heldRefusal) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// refusalCodeOf reads the code out of a refusal body, or "" for a body that
// is not the shape RequireAuth writes.
func refusalCodeOf(body []byte) string {
	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &refusal); err != nil {
		return ""
	}
	return refusal.Code
}

// withRefusalCode returns the body with its code replaced. A body that does
// not parse is returned as it was.
func withRefusalCode(body []byte, code string) []byte {
	var refusal map[string]any
	if err := json.Unmarshal(body, &refusal); err != nil || refusal == nil {
		return body
	}
	refusal["code"] = code
	out, err := json.Marshal(refusal)
	if err != nil {
		return body
	}
	return out
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
						return accountStateUnreadable(c)
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
				// A sign-in resolves no active account for a locked one, so
				// the session it made names none. That is every provider
				// sign-in — Google, Apple — after a deletion failed part-way;
				// the password sign-in scopes its session to the locked account
				// itself, but the admission has to live where the session is
				// checked, not where one kind of session is made (wm-or9a5).
				if locked := LockedAccountFor(ctx, info.AgentID, accounts, locks, logger); locked != nil {
					id := locked.GetID()
					admit(c, &auth.Identity{AgentID: info.AgentID, AccountIDs: []string{id}, ActiveAccountID: id}, true)
					return next(c)
				}
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

// LockedAccountFor finds an account whose erasure is unfinished that the
// agent may finish deleting — one they are an owner or admin of. A plain
// member of a locked account is not offered the deletion, because the account
// is not theirs to end. It is the one rule for every way in: the password
// sign-in uses it to scope its session, and the deletion route uses it to
// admit a session that names no account.
func LockedAccountFor(
	ctx context.Context, agentID string,
	accounts authrepos.AccountRepository, locks repositories.AccountErasureLocks, logger entities.Logger,
) *authentities.Account {
	if accounts == nil || locks == nil {
		return nil
	}
	memberships, err := accounts.FindByMember(ctx, agentID)
	if err != nil {
		logger.Warn(ctx, "could not read memberships for an unscoped sign-in", "agent_id", agentID, "error", err)
		return nil
	}
	for _, account := range memberships {
		if account == nil || account.Active() {
			continue
		}
		locked, err := locks.IsLocked(ctx, account.GetID())
		if err != nil {
			logger.Warn(ctx, "could not read the erasure lock", "account_id", account.GetID(), "error", err)
			continue
		}
		if !locked {
			continue
		}
		allowed, err := IsOwnerOrAdmin(ctx, accounts, account.GetID(), agentID)
		if err != nil {
			logger.Warn(ctx, "could not read the role in a locked account", "account_id", account.GetID(), "error", err)
			continue
		}
		if allowed {
			return account
		}
	}
	return nil
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
