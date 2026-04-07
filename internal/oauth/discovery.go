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

package oauth

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// ProtectedResourceMetadata returns a handler for RFC 9728 metadata.
// GET /.well-known/oauth-protected-resource
func ProtectedResourceMetadata(baseURL string) echo.HandlerFunc {
	baseURL = strings.TrimRight(baseURL, "/")
	return func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"resource":                 baseURL + "/api/mcp",
			"authorization_servers":    []string{baseURL},
			"scopes_supported":         SupportedScopesList,
			"bearer_methods_supported": []string{"header"},
		})
	}
}

// AuthorizationServerMetadata returns a handler for RFC 8414 metadata.
// GET /.well-known/oauth-authorization-server
func AuthorizationServerMetadata(baseURL string, dynamicRegistration bool) echo.HandlerFunc {
	baseURL = strings.TrimRight(baseURL, "/")
	return func(c echo.Context) error {
		meta := map[string]any{
			"issuer":                                baseURL,
			"authorization_endpoint":                baseURL + "/oauth/authorize",
			"token_endpoint":                        baseURL + "/oauth/token",
			"scopes_supported":                      SupportedScopesList,
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none"},
		}
		if dynamicRegistration {
			meta["registration_endpoint"] = baseURL + "/oauth/register"
		}
		return c.JSON(http.StatusOK, meta)
	}
}
