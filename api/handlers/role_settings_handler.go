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

	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"

	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/labstack/echo/v4"
)

type RoleSettingsHandler struct {
	repo        repositories.RoleSettingsRepository
	accountRepo authrepos.AccountRepository
	logger      entities.Logger
}

type RoleSettingsHandlerConfig struct {
	Repo        repositories.RoleSettingsRepository
	AccountRepo authrepos.AccountRepository
	Logger      entities.Logger
}

func NewRoleSettingsHandler(cfg RoleSettingsHandlerConfig) *RoleSettingsHandler {
	return &RoleSettingsHandler{
		repo:        cfg.Repo,
		accountRepo: cfg.AccountRepo,
		logger:      cfg.Logger,
	}
}

type roleSettingsResponse struct {
	Roles []string `json:"roles"`
}

type roleSettingsRequest struct {
	Roles []string `json:"roles"`
}

func (h *RoleSettingsHandler) Get(c echo.Context) error {
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	roles, err := h.repo.GetRoleNames(ctx)
	if err != nil {
		h.logger.Error(ctx, "failed to load role settings", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to load roles")
	}
	return respond(c, http.StatusOK, roleSettingsResponse{Roles: roles})
}

// Save updates the role list. Admin-only.
func (h *RoleSettingsHandler) Save(c echo.Context) error {
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	var req roleSettingsRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}

	hasAdmin := false
	for _, r := range req.Roles {
		if r == "admin" {
			hasAdmin = true
			break
		}
	}
	if !hasAdmin {
		req.Roles = append([]string{"admin"}, req.Roles...)
	}

	rolesJSON, err := json.Marshal(req.Roles)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to encode roles")
	}

	settings := &models.RoleSettings{
		Roles: string(rolesJSON),
	}
	if err := h.repo.Save(ctx, settings); err != nil {
		h.logger.Error(ctx, "failed to save role settings", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to save roles")
	}

	return respond(c, http.StatusOK, roleSettingsResponse(req))
}
