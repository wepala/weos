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
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/labstack/echo/v4"
)

type ResourceHandler struct {
	resourceService     application.ResourceService
	resourceTypeService application.ResourceTypeService
}

func NewResourceHandler(
	rs application.ResourceService,
	rts application.ResourceTypeService,
) *ResourceHandler {
	return &ResourceHandler{
		resourceService:     rs,
		resourceTypeService: rts,
	}
}

type ResourceResponse struct {
	ID        string          `json:"id"`
	TypeSlug  string          `json:"type_slug"`
	Data      json.RawMessage `json:"data"`
	Status    string          `json:"status"`
	CreatedAt string          `json:"created_at"`
}

func (h *ResourceHandler) Create(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	if _, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "resource type not found"})
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "failed to read request body"})
	}

	entity, err := h.resourceService.Create(
		c.Request().Context(),
		application.CreateResourceCommand{TypeSlug: typeSlug, Data: json.RawMessage(body)},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return respondWithResource(c, http.StatusCreated, entity)
}

func (h *ResourceHandler) Get(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	if _, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "resource type not found"})
	}

	entity, err := h.resourceService.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "resource not found"})
	}
	return respondWithResourceData(c, http.StatusOK, entity)
}

func (h *ResourceHandler) List(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	if _, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "resource type not found"})
	}

	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	result, err := h.resourceService.List(c.Request().Context(), typeSlug, cursor, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}

	wantsLD := wantsJSONLD(c)
	items := make([]json.RawMessage, 0, len(result.Data))
	for _, e := range result.Data {
		if wantsLD {
			items = append(items, e.Data())
		} else {
			simplified, _ := entities.SimplifyJSONLD(e.Data())
			items = append(items, simplified)
		}
	}
	return c.JSON(http.StatusOK, map[string]any{
		"data":     items,
		"cursor":   result.Cursor,
		"has_more": result.HasMore,
	})
}

func (h *ResourceHandler) Update(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	if _, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "resource type not found"})
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "failed to read request body"})
	}

	entity, err := h.resourceService.Update(
		c.Request().Context(),
		application.UpdateResourceCommand{ID: c.Param("id"), Data: json.RawMessage(body)},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return respondWithResource(c, http.StatusOK, entity)
}

func (h *ResourceHandler) Delete(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	if _, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "resource type not found"})
	}

	cmd := application.DeleteResourceCommand{ID: c.Param("id")}
	if err := h.resourceService.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func wantsJSONLD(c echo.Context) bool {
	accept := c.Request().Header.Get("Accept")
	return strings.Contains(accept, "application/ld+json")
}

func respondWithResourceData(c echo.Context, status int, entity *entities.Resource) error {
	if wantsJSONLD(c) {
		return c.JSONBlob(status, entity.Data())
	}
	simplified, err := entities.SimplifyJSONLD(entity.Data())
	if err != nil {
		return c.JSONBlob(status, entity.Data())
	}
	return c.JSONBlob(status, simplified)
}

func respondWithResource(c echo.Context, status int, entity *entities.Resource) error {
	return c.JSON(status, ResourceResponse{
		ID:        entity.GetID(),
		TypeSlug:  entity.TypeSlug(),
		Data:      entity.Data(),
		Status:    entity.Status(),
		CreatedAt: entity.CreatedAt().Format(time.RFC3339),
	})
}
