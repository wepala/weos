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

	apimw "weos/api/middleware"
	"weos/domain/entities"
	gormdb "weos/infrastructure/database/gorm"
	"weos/infrastructure/models"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/labstack/echo/v4"
)

// odrlActions maps short action names used in the API to ODRL IRIs.
var odrlActions = map[string]string{
	"read":   authentities.ActionRead,
	"modify": authentities.ActionModify,
	"delete": authentities.ActionDelete,
}

type RoleAccessHandler struct {
	repo        *gormdb.RoleResourceAccessRepository
	checker     *authcasbin.CasbinAuthorizationChecker
	accountRepo authrepos.AccountRepository
	logger      entities.Logger
}

type RoleAccessHandlerConfig struct {
	Repo        *gormdb.RoleResourceAccessRepository
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
	Roles gormdb.AccessMap `json:"roles"`
}

type roleAccessRequest struct {
	Roles gormdb.AccessMap `json:"roles"`
}

// Get returns the current role-resource access configuration.
func (h *RoleAccessHandler) Get(c echo.Context) error {
	accessMap, err := h.repo.GetAccessMap(c.Request().Context())
	if err != nil {
		h.logger.Error(c.Request().Context(), "failed to load role access", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load role access"})
	}
	return c.JSON(http.StatusOK, roleAccessResponse{Roles: accessMap})
}

// Save updates the role-resource access configuration and syncs policies to Casbin.
func (h *RoleAccessHandler) Save(c echo.Context) error {
	ctx := c.Request().Context()

	if !apimw.IsAdmin(ctx, h.accountRepo) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "admin role required"})
	}

	var req roleAccessRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.Roles == nil {
		req.Roles = gormdb.AccessMap{}
	}

	// Remove admin/owner from the map — they always have wildcard access.
	delete(req.Roles, authentities.RoleAdmin)
	delete(req.Roles, authentities.RoleOwner)

	// Load old access map before saving to know which roles to clear.
	oldMap, _ := h.repo.GetAccessMap(ctx)

	accessJSON, err := json.Marshal(req.Roles)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encode access"})
	}

	settings := &models.RoleResourceAccess{Access: string(accessJSON)}
	if err := h.repo.Save(ctx, settings); err != nil {
		h.logger.Error(ctx, "failed to save role access", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save role access"})
	}

	// Sync policies to Casbin enforcer, clearing stale role policies.
	SyncAccessMapToCasbin(h.checker, req.Roles, oldMap)

	return c.JSON(http.StatusOK, roleAccessResponse{Roles: req.Roles})
}

// SyncAccessMapToCasbin clears stale role policies and re-adds from the new access map.
// oldAccessMap may be nil (on startup). It preserves admin and owner wildcard policies.
func SyncAccessMapToCasbin(
	checker *authcasbin.CasbinAuthorizationChecker,
	newMap, oldMap gormdb.AccessMap,
) {
	// Collect all roles to clear: union of old and new maps (excluding admin/owner).
	rolesToClear := make(map[string]bool)
	for role := range oldMap {
		if role != authentities.RoleAdmin && role != authentities.RoleOwner {
			rolesToClear[role] = true
		}
	}
	for role := range newMap {
		if role != authentities.RoleAdmin && role != authentities.RoleOwner {
			rolesToClear[role] = true
		}
	}

	// Remove all per-slug policies for roles being updated/removed.
	for role := range rolesToClear {
		// Remove policies from the old map's slugs.
		if oldResources, ok := oldMap[role]; ok {
			for slug := range oldResources {
				for _, odrl := range odrlActions {
					_ = checker.RemovePermission(role, odrl, slug)
				}
			}
		}
		// Also remove wildcard in case it was set.
		for _, odrl := range odrlActions {
			_ = checker.RemovePermission(role, odrl, "*")
		}
	}

	// Add policies from the new map.
	for role, resources := range newMap {
		if role == authentities.RoleAdmin || role == authentities.RoleOwner {
			continue
		}
		for slug, actions := range resources {
			for _, action := range actions {
				if odrl, ok := odrlActions[action]; ok {
					_ = checker.AddPermission(role, odrl, slug)
				}
			}
		}
	}

	SeedAdminPolicies(checker)
}

// SeedAdminPolicies ensures admin and owner roles have wildcard access.
// Idempotent — safe to call on every startup.
func SeedAdminPolicies(checker *authcasbin.CasbinAuthorizationChecker) {
	for _, role := range []string{authentities.RoleAdmin, authentities.RoleOwner} {
		for _, odrl := range odrlActions {
			_ = checker.AddPermission(role, odrl, "*")
		}
	}
}
