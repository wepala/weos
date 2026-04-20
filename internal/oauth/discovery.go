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

// WellKnownProtectedResourcePrefix is the RFC 9728 §3.1 well-known URI prefix; resource paths are appended as a suffix.
const WellKnownProtectedResourcePrefix = "/.well-known/oauth-protected-resource"

// IsProtectedResourceMetadataRequest reports whether the given HTTP method
// and request path target the protected-resource metadata endpoint, in either
// the bare or path-suffixed form per RFC 9728 §3.1.
func IsProtectedResourceMetadataRequest(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	return path == WellKnownProtectedResourcePrefix ||
		strings.HasPrefix(path, WellKnownProtectedResourcePrefix+"/")
}

// ProtectedResourceMetadata serves RFC 9728 §3.1 metadata at both the bare
// well-known path and the path-suffixed form. defaultResource backs the bare
// path; a non-empty suffix must match an entry in knownResources. Anything
// else — unknown suffix, foreign path, or bare path with empty
// defaultResource — returns 404 so the server doesn't advertise metadata for
// resources it doesn't expose.
func ProtectedResourceMetadata(baseURL, defaultResource string, knownResources map[string]bool) echo.HandlerFunc {
	baseURL = strings.TrimRight(baseURL, "/")
	return func(c echo.Context) error {
		path := c.Request().URL.Path
		if !IsProtectedResourceMetadataRequest(c.Request().Method, path) {
			return echo.ErrNotFound
		}
		suffix := strings.TrimPrefix(path, WellKnownProtectedResourcePrefix)
		var resourcePath string
		switch {
		case suffix == "" || suffix == "/":
			if defaultResource == "" {
				return echo.ErrNotFound
			}
			resourcePath = defaultResource
		case knownResources[suffix]:
			resourcePath = suffix
		default:
			return echo.ErrNotFound
		}
		return c.JSON(http.StatusOK, map[string]any{
			"resource":                 baseURL + resourcePath,
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
