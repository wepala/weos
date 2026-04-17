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

	"github.com/wepala/weos/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/labstack/echo/v4"
)

// methodToAction maps HTTP methods to ODRL action IRIs.
var methodToAction = map[string]string{
	"GET":    "http://www.w3.org/ns/odrl/2/read",
	"POST":   "http://www.w3.org/ns/odrl/2/modify",
	"PUT":    "http://www.w3.org/ns/odrl/2/modify",
	"PATCH":  "http://www.w3.org/ns/odrl/2/modify",
	"DELETE": "http://www.w3.org/ns/odrl/2/delete",
}

// AuthorizeResource returns Echo middleware that checks Casbin policies for
// dynamic resource routes (/:typeSlug and /:typeSlug/:id). Requests without a
// :typeSlug parameter are passed through (they are static routes).
//
// Fail-closed: nil identity returns 401, missing role returns 403, and any
// Casbin error returns 500. The only exception is unconfigured roles (zero
// policies) which are allowed through with a logged warning.
//
// Middleware order is security-critical: RequireAuth must run first to establish
// identity, then Impersonation to swap identity if active, then this middleware.
func AuthorizeResource(
	checker *authcasbin.CasbinAuthorizationChecker,
	accountRepo authrepos.AccountRepository,
	logger entities.Logger,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			typeSlug := c.Param("typeSlug")
			if typeSlug == "" {
				return next(c)
			}

			ctx := c.Request().Context()

			identity := auth.AgentFromCtx(ctx)
			if identity == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			}

			role, err := GetUserRole(ctx, accountRepo)
			if err != nil {
				logger.Error(ctx, "authorization: failed to resolve role", "error", err)
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "authorization check failed"})
			}
			if role == "" {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "no role assigned"})
			}

			// Ensure the user's role is assigned as a Casbin grouping policy
			// so role-based policies apply. This is idempotent.
			if identity.ActiveAccountID != "" {
				_ = checker.AssignAccountRole(identity.AgentID, role, identity.ActiveAccountID)
			} else {
				_ = checker.AssignRole(identity.AgentID, role)
			}

			action, ok := methodToAction[strings.ToUpper(c.Request().Method)]
			if !ok {
				return c.JSON(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			}

			var allowed bool
			if identity.ActiveAccountID != "" {
				allowed, err = checker.IsAuthorizedInAccount(
					ctx, identity.AgentID, identity.ActiveAccountID,
					action, typeSlug,
				)
			} else {
				allowed, err = checker.IsAuthorized(
					ctx, identity.AgentID, action, typeSlug,
				)
			}

			if err != nil {
				logger.Error(ctx, "authorization: casbin check failed",
					"error", err, "role", role, "action", action, "resource", typeSlug)
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "authorization check failed"})
			}

			if !allowed {
				// Allow read-only access when the role has zero configured policies
				// (unconfigured role — admin has not yet set up access).
				// Write operations are always denied to prevent unauthorized mutations.
				perms, permErr := checker.GetPermissions(ctx, role)
				if permErr != nil {
					logger.Error(ctx, "authorization: failed to check permissions",
						"error", permErr, "role", role)
					return c.JSON(http.StatusInternalServerError, map[string]string{"error": "authorization check failed"})
				}
				isRead := action == "http://www.w3.org/ns/odrl/2/read"
				if len(perms) == 0 && isRead {
					logger.Warn(ctx, "authorization: allowing unconfigured role read access",
						"role", role, "action", action, "resource", typeSlug)
					return next(c)
				}
				return c.JSON(http.StatusForbidden, map[string]string{
					"error": "you do not have access to this resource type",
				})
			}

			return next(c)
		}
	}
}
