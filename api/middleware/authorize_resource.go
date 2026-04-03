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

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/labstack/echo/v4"
)

// methodToAction maps HTTP methods to ODRL action IRIs.
var methodToAction = map[string]string{
	"GET":    authentities.ActionRead,
	"POST":   authentities.ActionModify,
	"PUT":    authentities.ActionModify,
	"PATCH":  authentities.ActionModify,
	"DELETE": authentities.ActionDelete,
}

// AuthorizeResource returns Echo middleware that checks Casbin policies
// for dynamic resource routes (/:typeSlug and /:typeSlug/:id).
// Non-resource routes (those with a known static prefix) are passed through.
func AuthorizeResource(
	checker *authcasbin.CasbinAuthorizationChecker,
	accountRepo authrepos.AccountRepository,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			typeSlug := c.Param("typeSlug")
			if typeSlug == "" {
				return next(c)
			}

			identity := auth.AgentFromCtx(c.Request().Context())
			if identity == nil {
				return next(c)
			}

			role := GetUserRole(c.Request().Context(), accountRepo)
			if role == "" {
				return next(c)
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
				return next(c)
			}

			var allowed bool
			var err error
			if identity.ActiveAccountID != "" {
				allowed, err = checker.IsAuthorizedInAccount(
					c.Request().Context(),
					identity.AgentID, identity.ActiveAccountID,
					action, typeSlug,
				)
			} else {
				allowed, err = checker.IsAuthorized(
					c.Request().Context(),
					identity.AgentID, action, typeSlug,
				)
			}

			if err != nil || !allowed {
				// Default allow for unconfigured roles: if no policies exist for this role
				// at all (role has no configured access), allow through.
				perms, _ := checker.GetPermissions(c.Request().Context(), role)
				if len(perms) == 0 {
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
