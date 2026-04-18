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
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/labstack/echo/v4"
)

type RoleAccessHandler struct {
	repo        repositories.RoleResourceAccessRepository
	checker     *authcasbin.CasbinAuthorizationChecker
	accountRepo authrepos.AccountRepository
	logger      entities.Logger
}

type RoleAccessHandlerConfig struct {
	Repo        repositories.RoleResourceAccessRepository
	Checker     *authcasbin.CasbinAuthorizationChecker
	AccountRepo authrepos.AccountRepository
	Logger      entities.Logger
}

func NewRoleAccessHandler(cfg RoleAccessHandlerConfig) *RoleAccessHandler {
	return &RoleAccessHandler{
		repo:        cfg.Repo,
		checker:     cfg.Checker,
		accountRepo: cfg.AccountRepo,
		logger:      cfg.Logger,
	}
}

type roleAccessResponse struct {
	Roles entities.AccessMap `json:"roles"`
}

type roleAccessRequest struct {
	Roles entities.AccessMap `json:"roles"`
}

// Get returns the current role-resource access configuration. Admin-only.
func (h *RoleAccessHandler) Get(c echo.Context) error {
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	accessMap, err := h.repo.GetAccessMap(ctx)
	if err != nil {
		h.logger.Error(ctx, "failed to load role access", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to load role access")
	}
	return respond(c, http.StatusOK, roleAccessResponse{Roles: accessMap})
}

// Save updates the role-resource access configuration and syncs policies to Casbin.
func (h *RoleAccessHandler) Save(c echo.Context) error {
	ctx := c.Request().Context()

	isAdmin, err := apimw.IsAdmin(ctx, h.accountRepo)
	if err != nil {
		h.logger.Error(ctx, "failed to check admin status", "error", err)
		return respondError(c, http.StatusInternalServerError, "authorization check failed")
	}
	if !isAdmin {
		return respondError(c, http.StatusForbidden, "admin role required")
	}

	var req roleAccessRequest
	if err := c.Bind(&req); err != nil {
		return respondError(c, http.StatusBadRequest, "invalid request body")
	}
	if req.Roles == nil {
		req.Roles = entities.AccessMap{}
	}

	// Remove admin/owner from the map — they always have wildcard access.
	delete(req.Roles, authentities.RoleAdmin)
	delete(req.Roles, authentities.RoleOwner)

	// Load old access map before saving to know which roles to clear.
	oldMap, oldMapErr := h.repo.GetAccessMap(ctx)
	if oldMapErr != nil {
		h.logger.Warn(ctx, "failed to load previous access map, stale policies may not be cleared", "error", oldMapErr)
	}

	accessJSON, err := json.Marshal(req.Roles)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to encode access")
	}

	settings := &models.RoleResourceAccess{Access: string(accessJSON)}
	if err := h.repo.Save(ctx, settings); err != nil {
		h.logger.Error(ctx, "failed to save role access", "error", err)
		return respondError(c, http.StatusInternalServerError, "failed to save role access")
	}

	// Sync policies to Casbin enforcer, clearing stale role policies.
	if syncErr := application.SyncAccessMapToCasbin(h.checker, req.Roles, oldMap); syncErr != nil {
		h.logger.Warn(ctx, "casbin policy sync partially failed", "error", syncErr)
	}

	return respond(c, http.StatusOK, roleAccessResponse(req))
}
