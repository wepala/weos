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
	"encoding/json"
	"net/http"

	apimw "github.com/wepala/weos/api/middleware"
	"github.com/wepala/weos/domain/entities"
	gormdb "github.com/wepala/weos/infrastructure/database/gorm"
	"github.com/wepala/weos/infrastructure/models"

	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

type SidebarSettingsHandler struct {
	repo        *gormdb.SidebarSettingsRepository
	accountRepo authrepos.AccountRepository
	logger      entities.Logger
}

func NewSidebarSettingsHandler(
	repo *gormdb.SidebarSettingsRepository,
	accountRepo authrepos.AccountRepository,
	logger entities.Logger,
) *SidebarSettingsHandler {
	return &SidebarSettingsHandler{repo: repo, accountRepo: accountRepo, logger: logger}
}

type sidebarSettingsResponse struct {
	HiddenSlugs []string          `json:"hidden_slugs"`
	MenuGroups  map[string]string `json:"menu_groups"`
}

type sidebarSettingsRequest struct {
	HiddenSlugs []string          `json:"hidden_slugs"`
	MenuGroups  map[string]string `json:"menu_groups"`
}

// Get returns sidebar settings. If ?role= is provided (admin-only), returns
// that role's settings. Otherwise returns settings for the current user's role.
func (h *SidebarSettingsHandler) Get(c echo.Context) error {
	ctx := c.Request().Context()

	role := c.QueryParam("role")
	if role == "" {
		var roleErr error
		role, roleErr = apimw.GetUserRole(ctx, h.accountRepo)
		if roleErr != nil {
			h.logger.Warn(ctx, "failed to get user role for sidebar, using default", "error", roleErr)
			role = "default"
		}
	}

	settings, err := h.repo.GetByRole(ctx, role)
	if err != nil {
		h.logger.Error(ctx, "failed to load sidebar settings", "error", err, "role", role)
		return respondError(c, http.StatusInternalServerError, "failed to load settings")
	}

	return respond(c, http.StatusOK, h.toResponse(settings))
}

// Save updates sidebar settings. Admin-only.
func (h *SidebarSettingsHandler) Save(c echo.Context) error {
	isAdmin, err := apimw.IsAdmin(c.Request().Context(), h.accountRepo)
	if err != nil {
		h.logger.Error(c.Request().Context(), "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	var req sidebarSettingsRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}

	if req.HiddenSlugs == nil {
		req.HiddenSlugs = []string{}
	}
	if req.MenuGroups == nil {
		req.MenuGroups = map[string]string{}
	}

	hiddenJSON, err := json.Marshal(req.HiddenSlugs)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to encode settings")
	}

	groupsJSON, err := json.Marshal(req.MenuGroups)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to encode settings")
	}

	role := c.QueryParam("role")
	if role == "" {
		role = "default"
	}

	settings := &models.SidebarSettings{
		HiddenSlugs: string(hiddenJSON),
		MenuGroups:  string(groupsJSON),
	}

	if err := h.repo.SaveByRole(c.Request().Context(), role, settings); err != nil {
		h.logger.Error(c.Request().Context(), "failed to save sidebar settings", "error", err, "role", role)
		return respondError(c, http.StatusInternalServerError, "failed to save settings")
	}

	return respond(c, http.StatusOK, sidebarSettingsResponse(req))
}

func (h *SidebarSettingsHandler) toResponse(settings *models.SidebarSettings) sidebarSettingsResponse {
	resp := sidebarSettingsResponse{
		HiddenSlugs: []string{},
		MenuGroups:  map[string]string{},
	}
	if settings.HiddenSlugs != "" {
		if err := json.Unmarshal([]byte(settings.HiddenSlugs), &resp.HiddenSlugs); err != nil {
			resp.HiddenSlugs = []string{}
		}
	}
	if settings.MenuGroups != "" {
		if err := json.Unmarshal([]byte(settings.MenuGroups), &resp.MenuGroups); err != nil {
			resp.MenuGroups = map[string]string{}
		}
	}
	return resp
}
