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

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// FeatureHandler serves the endpoints the admin UI (#486) calls to see and
// change feature state.
//
// It holds no permission logic of its own. Every check lives in
// FeatureAdminService, shared with the CLI and the MCP tools, so the three
// surfaces cannot come to disagree about who may change what — which is
// exactly what a listing that says one thing and a write that does another
// would look like from the outside.
type FeatureHandler struct {
	admin  *application.FeatureAdminService
	logger entities.Logger
}

type FeatureHandlerConfig struct {
	Admin  *application.FeatureAdminService
	Logger entities.Logger
}

func NewFeatureHandler(cfg FeatureHandlerConfig) *FeatureHandler {
	return &FeatureHandler{admin: cfg.Admin, logger: cfg.Logger}
}

// setFeatureRequest is the body of both PUT endpoints.
//
// Enabled is a pointer so an omitted field is distinguishable from false. A
// bare bool would bind an empty body to false, so a client that forgot the
// field — or sent {} — would silently DISABLE the feature it meant to enable,
// and get a 200 saying so.
type setFeatureRequest struct {
	Enabled *bool `json:"enabled"`
}

// List returns every declared feature with its resolved value and the layer
// that decided it, for the caller asking.
//
// Readable by any authenticated caller, including an ordinary member. Seeing
// which capabilities exist is not the same as being able to change them, and
// the admin UI needs the list to decide what to render for the person looking
// at it.
func (h *FeatureHandler) List(c echo.Context) error {
	statuses, err := h.admin.Listing(c.Request().Context())
	if err != nil {
		// The service answers all-off rather than failing when the store
		// cannot be read, so an error here is something else entirely.
		h.logger.Error(c.Request().Context(), "failed to list features", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to list features")
	}
	return respond(c, http.StatusOK, statuses)
}

// SetInstance turns a feature on or off for the whole instance.
func (h *FeatureHandler) SetInstance(c echo.Context) error {
	req, err := bindSetFeature(c)
	if err != nil {
		return respondError(c, http.StatusBadRequest, err.Error())
	}
	return h.respondAfterChange(c, h.admin.SetInstance(
		c.Request().Context(), c.Param("key"), *req.Enabled, entities.FeatureChangeSourceAPI))
}

// ResetInstance removes the instance override so the feature returns to its
// declared default. DELETE rather than a PUT of false, because removing an
// override and setting it to off are genuinely different outcomes: an account
// or a grant can still turn a reset feature on.
func (h *FeatureHandler) ResetInstance(c echo.Context) error {
	err := h.admin.ResetInstance(c.Request().Context(), c.Param("key"), entities.FeatureChangeSourceAPI)
	return h.respondAfterChange(c, err)
}

// SetAccount turns a feature on or off for the account the caller is signed in
// to.
//
// The account is never a parameter. It comes off the session, so an admin of
// one account cannot reach into another by naming it — and an endpoint that
// accepted an account id would make that property untestable.
func (h *FeatureHandler) SetAccount(c echo.Context) error {
	req, err := bindSetFeature(c)
	if err != nil {
		return respondError(c, http.StatusBadRequest, err.Error())
	}
	return h.respondAfterChange(c, h.admin.SetAccount(
		c.Request().Context(), c.Param("key"), *req.Enabled, entities.FeatureChangeSourceAPI))
}

// ResetAccount removes the caller's account override.
func (h *FeatureHandler) ResetAccount(c echo.Context) error {
	err := h.admin.ResetAccount(c.Request().Context(), c.Param("key"), entities.FeatureChangeSourceAPI)
	return h.respondAfterChange(c, err)
}

// grantRequestBody is the body of POST /api/features/:key/grants.
//
// There is deliberately no account field. The account comes off the session,
// so an admin naming another one has nothing to bind to — the property is
// enforced by the shape rather than by a check that could be forgotten.
type grantRequestBody struct {
	Email        string     `json:"email,omitempty"`
	Role         string     `json:"role,omitempty"`
	ValidFrom    *time.Time `json:"validFrom,omitempty"`
	ValidThrough *time.Time `json:"validThrough,omitempty"`
}

// ListGrants returns every grant on one feature in the caller's account,
// including pending and expired ones.
func (h *FeatureHandler) ListGrants(c echo.Context) error {
	views, err := h.admin.GrantsOn(c.Request().Context(), c.Param("key"), "")
	if err != nil {
		return h.grantError(c, err)
	}
	return respond(c, http.StatusOK, views)
}

// GrantsHeldBy returns everything one person holds, directly or through a role.
func (h *FeatureHandler) GrantsHeldBy(c echo.Context) error {
	views, err := h.admin.GrantsHeldBy(c.Request().Context(), c.QueryParam("email"))
	if err != nil {
		return h.grantError(c, err)
	}
	return respond(c, http.StatusOK, views)
}

// Grant gives a feature to one person or to a role.
func (h *FeatureHandler) Grant(c echo.Context) error {
	var body grantRequestBody
	if err := c.Bind(&body); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request")
	}
	err := h.admin.Grant(c.Request().Context(), application.GrantRequest{
		Key:          c.Param("key"),
		Email:        body.Email,
		Role:         body.Role,
		ValidFrom:    body.ValidFrom,
		ValidThrough: body.ValidThrough,
		Source:       entities.FeatureChangeSourceAPI,
	})
	if err != nil {
		return h.grantError(c, err)
	}
	return h.respondWithGrants(c)
}

// RevokeGrant takes a grant back. The subject comes from the query string:
// DELETE bodies are unreliable across clients and proxies.
func (h *FeatureHandler) RevokeGrant(c echo.Context) error {
	err := h.admin.RevokeGrant(c.Request().Context(), application.RevokeRequest{
		Key:    c.Param("key"),
		Email:  c.QueryParam("email"),
		Role:   c.QueryParam("role"),
		Source: entities.FeatureChangeSourceAPI,
	})
	if err != nil {
		return h.grantError(c, err)
	}
	return h.respondWithGrants(c)
}

// respondWithGrants answers a change with the feature's current grants, so a
// client can see what the change produced — a window in particular resolves to
// a status the client cannot compute.
func (h *FeatureHandler) respondWithGrants(c echo.Context) error {
	views, err := h.admin.GrantsOn(c.Request().Context(), c.Param("key"), "")
	if err != nil {
		// The change landed; only the read-back failed. Saying so beats
		// reporting a failure for work that succeeded, and beats an empty body
		// a client would read as "there are no grants".
		h.logger.Error(c.Request().Context(), "grant changed but the listing could not be read", "error", err)
		return respond(c, http.StatusOK, map[string]any{"applied": true})
	}
	return respond(c, http.StatusOK, views)
}

func (h *FeatureHandler) grantError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, application.ErrForbidden):
		return respondError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, application.ErrValidation):
		return respondError(c, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error(c.Request().Context(), "failed to change a grant", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to change the grant")
	}
}

// bindSetFeature refuses a body that does not say which way to set the
// feature, rather than defaulting to off.
func bindSetFeature(c echo.Context) (setFeatureRequest, error) {
	var req setFeatureRequest
	if err := c.Bind(&req); err != nil {
		return req, fmt.Errorf("invalid request")
	}
	if req.Enabled == nil {
		return req, fmt.Errorf(`"enabled" is required: say true or false rather than leaving it out`)
	}
	return req, nil
}

// respondAfterChange maps the service's error to a status, then answers with
// the caller's freshly resolved listing.
//
// Returning the listing rather than an empty 200 means the admin UI never has
// to guess what the change produced — a reset in particular resolves to
// whatever layer now decides, which the client cannot compute for itself.
func (h *FeatureHandler) respondAfterChange(c echo.Context, err error) error {
	ctx := c.Request().Context()
	switch {
	case err == nil:
	case errors.Is(err, repositories.ErrNotFound):
		return respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, application.ErrForbidden):
		return respondError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, application.ErrValidation):
		return respondError(c, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error(ctx, "failed to change feature state", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to change feature state")
	}

	statuses, listErr := h.admin.Listing(ctx)
	if listErr != nil {
		// The change landed. Reporting a failure now would be a lie about what
		// happened, so the caller gets a 200 with no body rather than an error
		// for work that succeeded.
		h.logger.Error(ctx, "feature changed but the listing could not be read", "error", listErr)
		return respond(c, http.StatusOK, nil)
	}
	key := c.Param("key")
	for _, s := range statuses {
		if s.Key == key {
			return respond(c, http.StatusOK, s)
		}
	}
	return respond(c, http.StatusOK, statuses)
}
