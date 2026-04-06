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
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/segmentio/ksuid"
)

type registerRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

type registerResponse struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

// RegisterClient returns a handler for RFC 7591 Dynamic Client Registration.
// POST /oauth/register
func RegisterClient(clientRepo ClientRepository, enabled bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !enabled {
			return c.JSON(http.StatusForbidden,
				map[string]string{"error": "dynamic client registration is disabled"})
		}

		var req registerRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "invalid request body"})
		}
		if req.ClientName == "" {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "client_name is required"})
		}
		if len(req.RedirectURIs) == 0 {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": "redirect_uris is required"})
		}
		for _, uri := range req.RedirectURIs {
			if err := validateRedirectURI(uri); err != nil {
				return c.JSON(http.StatusBadRequest,
					map[string]string{"error": err.Error()})
			}
		}

		if len(req.GrantTypes) == 0 {
			req.GrantTypes = []string{"authorization_code"}
		}
		if len(req.ResponseTypes) == 0 {
			req.ResponseTypes = []string{"code"}
		}
		if req.TokenEndpointAuthMethod == "" {
			req.TokenEndpointAuthMethod = "none"
		}

		redirectJSON, err := json.Marshal(req.RedirectURIs)
		if err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}
		grantJSON, err := json.Marshal(req.GrantTypes)
		if err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}
		responseJSON, err := json.Marshal(req.ResponseTypes)
		if err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "server_error"})
		}

		client := &OAuthClient{
			ClientID:                ksuid.New().String(),
			ClientName:              req.ClientName,
			RedirectURIs:            string(redirectJSON),
			GrantTypes:              string(grantJSON),
			ResponseTypes:           string(responseJSON),
			TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
			Scope:                   req.Scope,
		}

		if err := clientRepo.Create(c.Request().Context(), client); err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": "failed to register client"})
		}

		return c.JSON(http.StatusCreated, registerResponse{
			ClientID:                client.ClientID,
			ClientName:              client.ClientName,
			RedirectURIs:            req.RedirectURIs,
			GrantTypes:              req.GrantTypes,
			ResponseTypes:           req.ResponseTypes,
			TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
			Scope:                   req.Scope,
		})
	}
}

// validateRedirectURI checks that a redirect URI uses HTTPS or is a localhost
// address (for development). Rejects javascript:, data:, and other schemes.
func validateRedirectURI(uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid redirect_uri: %s", uri)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("redirect_uri must not contain a fragment: %s", uri)
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		host := parsed.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("http redirect_uri only allowed for localhost: %s", uri)
	default:
		return fmt.Errorf("redirect_uri must use https (or http for localhost): %s", uri)
	}
}
