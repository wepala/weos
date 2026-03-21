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

	"weos/domain/entities"
	gormdb "weos/infrastructure/database/gorm"
	"weos/infrastructure/models"

	"github.com/labstack/echo/v4"
)

type SidebarSettingsHandler struct {
	repo   *gormdb.SidebarSettingsRepository
	logger entities.Logger
}

func NewSidebarSettingsHandler(repo *gormdb.SidebarSettingsRepository, logger entities.Logger) *SidebarSettingsHandler {
	return &SidebarSettingsHandler{repo: repo, logger: logger}
}

type sidebarSettingsResponse struct {
	HiddenSlugs []string          `json:"hidden_slugs"`
	MenuGroups  map[string]string `json:"menu_groups"`
}

type sidebarSettingsRequest struct {
	HiddenSlugs []string          `json:"hidden_slugs"`
	MenuGroups  map[string]string `json:"menu_groups"`
}

func (h *SidebarSettingsHandler) Get(c echo.Context) error {
	ctx := c.Request().Context()
	settings, err := h.repo.Get(ctx)
	if err != nil {
		h.logger.Error(ctx, "failed to load sidebar settings", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load settings"})
	}

	resp := sidebarSettingsResponse{
		HiddenSlugs: []string{},
		MenuGroups:  map[string]string{},
	}

	if settings.HiddenSlugs != "" {
		if err := json.Unmarshal([]byte(settings.HiddenSlugs), &resp.HiddenSlugs); err != nil {
			h.logger.Warn(ctx, "corrupt hidden_slugs JSON in sidebar settings", "error", err)
			resp.HiddenSlugs = []string{}
		}
	}
	if settings.MenuGroups != "" {
		if err := json.Unmarshal([]byte(settings.MenuGroups), &resp.MenuGroups); err != nil {
			h.logger.Warn(ctx, "corrupt menu_groups JSON in sidebar settings", "error", err)
			resp.MenuGroups = map[string]string{}
		}
	}

	return c.JSON(http.StatusOK, resp)
}

func (h *SidebarSettingsHandler) Save(c echo.Context) error {
	var req sidebarSettingsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.HiddenSlugs == nil {
		req.HiddenSlugs = []string{}
	}
	if req.MenuGroups == nil {
		req.MenuGroups = map[string]string{}
	}

	hiddenJSON, err := json.Marshal(req.HiddenSlugs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encode settings"})
	}

	groupsJSON, err := json.Marshal(req.MenuGroups)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encode settings"})
	}

	settings := &models.SidebarSettings{
		HiddenSlugs: string(hiddenJSON),
		MenuGroups:  string(groupsJSON),
	}

	if err := h.repo.Save(c.Request().Context(), settings); err != nil {
		h.logger.Error(c.Request().Context(), "failed to save sidebar settings", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
	}

	return c.JSON(http.StatusOK, sidebarSettingsResponse{
		HiddenSlugs: req.HiddenSlugs,
		MenuGroups:  req.MenuGroups,
	})
}
