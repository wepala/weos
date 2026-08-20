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
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/wepala/weos/v3/domain/entities"
)

// Gating an HTTP route on a feature (epic #480, story #486).
//
// #484 gated the MCP tools and #485 gated the agent skills, and both ended at
// a surface a model talks to. This one ends at a surface a person clicks,
// which is why hiding a sidebar entry is not enough on its own. Hiding is a
// courtesy to the person: it stops them being offered a link that leads to a
// refusal. This is the control, because a URL bar does not consult a sidebar.

// FeatureGate answers whether the caller on ctx holds a feature. It is the
// same shape internal/mcp and application/agents use, and
// application.ToolFeatureGate builds the real one — so a route and a tool
// gated on one key can never disagree about who holds it.
type FeatureGate func(ctx context.Context, featureKey string) bool

// RequireFeature refuses a request whose caller does not hold featureKey.
//
// The refusal is 403 carrying the shared gate wording, which is deliberately
// not what a role refusal says. The two send a reader to different places: a
// role refusal means ask an admin of this account to change your role, and a
// feature refusal means this capability is not switched on for you. One
// message for both would make a capability question look like a permissions
// question, and the person would go and ask the wrong thing.
//
// The feature is checked BEFORE the role, wherever both apply. A capability
// that is off for an instance is off for its owner too, so answering "you are
// not allowed" to somebody who would be allowed if it were on would be a lie.
//
// A nil gate refuses nothing, which is how a build with no feature wiring
// behaves. A surface that must never ship ungated checks for that itself; see
// internal/mcp.NewConfiguredServer.
func RequireFeature(gate FeatureGate, featureKey string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if gate == nil || featureKey == "" {
				return next(c)
			}
			ctx := c.Request().Context()
			if gate(ctx, featureKey) {
				return next(c)
			}
			return c.JSON(http.StatusForbidden, map[string]any{
				"error": entities.GateRefusal(ctx, c.Request().URL.Path, featureKey),
			})
		}
	}
}
