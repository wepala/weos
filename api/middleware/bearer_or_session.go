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
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/labstack/echo/v4"
)

// BearerOrSession returns Echo middleware that authenticates requests via
// either a Bearer JWT token or the existing session cookie flow.
//
// When a Bearer token is present, it is validated using the JWTService and
// an auth.Identity is injected into context. When no Bearer token is present,
// the request is passed through the sessionAuth middleware (pericarp RequireAuth).
//
// Unauthenticated requests receive a 401 with WWW-Authenticate header per the
// MCP Authorization spec, pointing to the Protected Resource Metadata endpoint.
func BearerOrSession(
	jwtService authapp.JWTService,
	sessionAuth func(http.Handler) http.Handler,
	baseURL string,
) echo.MiddlewareFunc {
	wwwAuth := `Bearer resource_metadata="` + baseURL +
		`/.well-known/oauth-protected-resource"`

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := extractBearer(c.Request())
			if token == "" {
				// No Bearer token — fall through to session auth.
				return sessionAuthEcho(sessionAuth, next, wwwAuth)(c)
			}

			claims, err := jwtService.ValidateToken(c.Request().Context(), token)
			if err != nil {
				c.Response().Header().Set("WWW-Authenticate", wwwAuth)
				return c.JSON(http.StatusUnauthorized,
					map[string]string{"error": "invalid token"})
			}

			identity := &auth.Identity{
				AgentID:         claims.AgentID,
				AccountIDs:      claims.AccountIDs,
				ActiveAccountID: claims.ActiveAccountID,
			}
			ctx := auth.ContextWithAgent(c.Request().Context(), identity)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

// sessionAuthEcho wraps the pericarp http.Handler middleware into an Echo
// handler and adds the WWW-Authenticate header on 401 responses.
func sessionAuthEcho(
	sessionAuth func(http.Handler) http.Handler,
	next echo.HandlerFunc,
	wwwAuth string,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Set WWW-Authenticate before calling wrapped so it is present
		// even if the session middleware commits a 401 response.
		c.Response().Header().Set("WWW-Authenticate", wwwAuth)

		var called bool
		var handlerErr error
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			c.SetRequest(r)
			handlerErr = next(c)
		})

		wrapped := sessionAuth(inner)
		wrapped.ServeHTTP(c.Response(), c.Request())

		if called {
			// Clear the header on successful auth — only relevant for 401s.
			c.Response().Header().Del("WWW-Authenticate")
			return handlerErr
		}
		// Session middleware rejected — 401 already written with header.
		return nil
	}
}
