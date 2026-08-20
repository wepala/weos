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
	"net/http"

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
type setFeatureRequest struct {
	Enabled bool `json:"enabled"`
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
		h.logger.Error(c.Request().Context(), "failed to list features", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to list features")
	}
	return respond(c, http.StatusOK, statuses)
}

// SetInstance turns a feature on or off for the whole instance.
func (h *FeatureHandler) SetInstance(c echo.Context) error {
	var req setFeatureRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request")
	}
	err := h.admin.SetInstance(
		c.Request().Context(), c.Param("key"), req.Enabled, entities.FeatureChangeSourceAPI)
	return h.respondAfterChange(c, err)
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
	var req setFeatureRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request")
	}
	err := h.admin.SetAccount(
		c.Request().Context(), c.Param("key"), req.Enabled, entities.FeatureChangeSourceAPI)
	return h.respondAfterChange(c, err)
}

// ResetAccount removes the caller's account override.
func (h *FeatureHandler) ResetAccount(c echo.Context) error {
	err := h.admin.ResetAccount(c.Request().Context(), c.Param("key"), entities.FeatureChangeSourceAPI)
	return h.respondAfterChange(c, err)
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
