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
	"strconv"
	"time"

	apimw "weos/api/middleware"
	"weos/application"
	"weos/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/labstack/echo/v4"
)

type ResourceTypeHandler struct {
	service     application.ResourceTypeService
	checker     *authcasbin.CasbinAuthorizationChecker
	accountRepo authrepos.AccountRepository
}

func NewResourceTypeHandler(
	service application.ResourceTypeService,
	checker *authcasbin.CasbinAuthorizationChecker,
	accountRepo authrepos.AccountRepository,
) *ResourceTypeHandler {
	return &ResourceTypeHandler{service: service, checker: checker, accountRepo: accountRepo}
}

type CreateResourceTypeRequest struct {
	Name    string          `json:"name"`
	Slug    string          `json:"slug"`
	Context json.RawMessage `json:"context,omitempty"`
}

type UpdateResourceTypeRequest struct {
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description"`
	Context     json.RawMessage `json:"context,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Status      string          `json:"status"`
}

type ResourceTypeResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description,omitempty"`
	Context     json.RawMessage `json:"context,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Status      string          `json:"status"`
	CreatedAt   string          `json:"created_at"`
}

func (h *ResourceTypeHandler) Create(c echo.Context) error {
	var req CreateResourceTypeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Create(
		c.Request().Context(),
		application.CreateResourceTypeCommand{
			Name: req.Name, Slug: req.Slug, Context: req.Context,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toResourceTypeResponse(entity))
}

func (h *ResourceTypeHandler) Get(c echo.Context) error {
	entity, err := h.service.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "resource type not found"})
	}
	return c.JSON(http.StatusOK, toResourceTypeResponse(entity))
}

func (h *ResourceTypeHandler) List(c echo.Context) error {
	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	result, err := h.service.List(c.Request().Context(), cursor, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}

	items := make([]ResourceTypeResponse, 0, len(result.Data))

	// Filter resource types by read permission when Casbin is configured.
	identity := auth.AgentFromCtx(c.Request().Context())
	role := apimw.GetUserRole(c.Request().Context(), h.accountRepo)
	skipFilter := role == authentities.RoleAdmin || role == authentities.RoleOwner || role == ""

	for _, e := range result.Data {
		if !skipFilter && identity != nil {
			perms, _ := h.checker.GetPermissions(c.Request().Context(), role)
			if len(perms) > 0 {
				var allowed bool
				if identity.ActiveAccountID != "" {
					allowed, _ = h.checker.IsAuthorizedInAccount(
						c.Request().Context(),
						identity.AgentID, identity.ActiveAccountID,
						authentities.ActionRead, e.Slug(),
					)
				} else {
					allowed, _ = h.checker.IsAuthorized(
						c.Request().Context(),
						identity.AgentID, authentities.ActionRead, e.Slug(),
					)
				}
				if !allowed {
					continue
				}
			}
		}
		items = append(items, toResourceTypeResponse(e))
	}

	return c.JSON(http.StatusOK, map[string]any{
		"data":     items,
		"cursor":   result.Cursor,
		"has_more": result.HasMore,
	})
}

func (h *ResourceTypeHandler) Update(c echo.Context) error {
	var req UpdateResourceTypeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Update(
		c.Request().Context(),
		application.UpdateResourceTypeCommand{
			ID:          c.Param("id"),
			Name:        req.Name,
			Slug:        req.Slug,
			Description: req.Description,
			Context:     req.Context,
			Schema:      req.Schema,
			Status:      req.Status,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toResourceTypeResponse(entity))
}

func (h *ResourceTypeHandler) Delete(c echo.Context) error {
	cmd := application.DeleteResourceTypeCommand{ID: c.Param("id")}
	if err := h.service.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func toResourceTypeResponse(e *entities.ResourceType) ResourceTypeResponse {
	return ResourceTypeResponse{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		Context:     e.Context(),
		Schema:      e.Schema(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt().Format(time.RFC3339),
	}
}
