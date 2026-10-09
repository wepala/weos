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
	"net/http"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

// InstanceAdminRequiredCode is the code on the 403 RequireInstanceAdmin
// answers, so a client can tell "you are not the instance's admin" from any
// other refusal.
const InstanceAdminRequiredCode = "instance_admin_required"

// InstanceAdminCheckFailedCode is the code on the 500 RequireInstanceAdmin
// answers when it cannot read the caller's role, so a client can tell a
// failed check from a refusal and from any other server error.
const InstanceAdminCheckFailedCode = "instance_admin_check_failed"

// instanceAdminRequiredMessage is the 403's text. It names the way through,
// because the two common refusals of a real operator are acting in another
// account and an impersonation still running, under which the guard judges
// the person impersonated.
const instanceAdminRequiredMessage = "only an owner or admin of this instance's admin account, acting in it, " +
	"may change resource types or install presets: switch to the instance admin account, " +
	"and end any impersonation first"

// RequireInstanceAdmin returns Echo middleware for the routes that change what
// every account on the instance shares: resource types and presets. A type's
// behaviors are set per account, so their route does not use it. It admits only a caller whose active account is
// instanceAccountID and who is an owner or admin there.
//
// Owning an account is not enough on its own. Resource types are
// instance-wide, and on an instance with sign-in every first sign-in makes its
// person the owner of an account of their own, so a check of the caller's
// role in the caller's account would admit anyone who signed in.
//
// The role is read in instanceAccountID itself, so an identity that merely
// names that account as its active one, without belonging to it, is refused.
// An identity with no active account is refused too: a session that has not
// settled on an account has not finished signing in.
//
// A request with no identity gets 401; any other refusal gets 403 with
// InstanceAdminRequiredCode. A role that cannot be read fails closed with 500
// and InstanceAdminCheckFailedCode. Every refusal is the shared ErrorEnvelope.
//
// An empty instanceAccountID (after trimming) returns a pass-through, so an
// instance that names no admin account keeps the behavior it had. It must run
// after the auth middleware that establishes the identity, and after
// Impersonation: an impersonating operator is judged as the person
// impersonated.
func RequireInstanceAdmin(
	instanceAccountID string,
	accountRepo authrepos.AccountRepository,
	logger entities.Logger,
) echo.MiddlewareFunc {
	instanceAccountID = strings.TrimSpace(instanceAccountID)
	if instanceAccountID == "" {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()
			identity := auth.AgentFromCtx(ctx)
			if identity == nil {
				return respondErrorEnvelope(c, http.StatusUnauthorized, "authentication required", "")
			}
			refuse := func(reason string) error {
				fields := []any{
					"reason", reason,
					"caller_agent_id", identity.AgentID,
					"account_id", identity.ActiveAccountID,
					"method", c.Request().Method,
					"path", c.Path(),
				}
				if impersonator := ImpersonatorFromCtx(ctx); impersonator != nil {
					fields = append(fields, "impersonator_agent_id", impersonator.AgentID,
						"remedy", "end the impersonation first: the caller is judged as the person impersonated")
				}
				logger.Warn(ctx, "instance admin required: refused a change to the instance's resource types", fields...)
				return respondErrorEnvelope(c, http.StatusForbidden, instanceAdminRequiredMessage, InstanceAdminRequiredCode)
			}
			if identity.ActiveAccountID != instanceAccountID {
				return refuse("the caller is not acting in the instance admin account")
			}
			role, err := accountRepo.FindMemberRole(ctx, instanceAccountID, identity.AgentID)
			if err != nil {
				logger.Error(ctx, "instance admin required: failed to read the caller's role",
					"caller_agent_id", identity.AgentID, "error", err)
				return respondErrorEnvelope(c, http.StatusInternalServerError,
					"authorization check failed", InstanceAdminCheckFailedCode)
			}
			if role != authentities.RoleOwner && role != authentities.RoleAdmin {
				return refuse("the caller is not an owner or admin of the instance admin account")
			}
			return next(c)
		}
	}
}
