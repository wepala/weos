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
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"

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
		return respondError(c, http.StatusNotFound, "resource type not found")
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return respondError(c, http.StatusBadRequest, "failed to read request body")
	}

	entity, err := h.resourceService.Create(
		c.Request().Context(),
		application.CreateResourceCommand{TypeSlug: typeSlug, Data: json.RawMessage(body)},
	)
	if err != nil {
		if errors.Is(err, entities.ErrAccessDenied) {
			return respondForbidden(c)
		}
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respondWithResource(c, http.StatusCreated, entity)
}

func (h *ResourceHandler) Get(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	rt, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug)
	if err != nil {
		return respondError(c, http.StatusNotFound, "resource type not found")
	}

	entity, err := h.resourceService.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, entities.ErrAccessDenied) {
			return respondForbidden(c)
		}
		return respondError(c, http.StatusNotFound, "resource not found")
	}
	return respondWithResourceData(c, http.StatusOK, entity, rt.Context())
}

func (h *ResourceHandler) List(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	if _, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); err != nil {
		return respondError(c, http.StatusNotFound, "resource type not found")
	}

	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit")) //nolint:errcheck // defaults to 0, handled below
	if limit <= 0 {
		limit = 20
	}
	sort := repositories.SortOptions{
		SortBy:    c.QueryParam("sort_by"),
		SortOrder: c.QueryParam("sort_order"),
	}
	filters := parseFilters(c)

	// Support legacy filter_field/filter_value as eq shorthand
	if ff := c.QueryParam("filter_field"); ff != "" {
		if fv := c.QueryParam("filter_value"); fv != "" {
			filters = append(filters, repositories.FilterCondition{
				Field: ff, Operator: "eq", Value: fv,
			})
		}
	}

	// Use flat projection queries for standard list requests.
	// Fall back to entity-based queries for JSON-LD requests.
	if !wantsJSONLD(c) {
		return h.listFlat(c, typeSlug, filters, cursor, limit, sort)
	}

	var result repositories.PaginatedResponse[*entities.Resource]
	var err error
	if len(filters) > 0 {
		result, err = h.resourceService.ListWithFilters(
			c.Request().Context(), typeSlug, filters, cursor, limit, sort)
	} else {
		result, err = h.resourceService.List(c.Request().Context(), typeSlug, cursor, limit, sort)
	}
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}

	items := make([]json.RawMessage, 0, len(result.Data))
	for _, e := range result.Data {
		items = append(items, e.Data())
	}
	return respondPaginated(c, http.StatusOK, items, result.Cursor, result.HasMore)
}

// listFlat returns flat projection rows directly for list views.
func (h *ResourceHandler) listFlat(
	c echo.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) error {
	var result repositories.PaginatedResponse[map[string]any]
	var err error
	if len(filters) > 0 {
		result, err = h.resourceService.ListFlatWithFilters(
			c.Request().Context(), typeSlug, filters, cursor, limit, sort)
	} else {
		result, err = h.resourceService.ListFlat(
			c.Request().Context(), typeSlug, cursor, limit, sort)
	}
	if err != nil {
		// Fall back to entity-based list if no projection table exists.
		return h.listEntities(c, typeSlug, filters, cursor, limit, sort)
	}

	return respondPaginated(c, http.StatusOK, result.Data, result.Cursor, result.HasMore)
}

// listEntities returns entity-based results with simplified JSON-LD.
func (h *ResourceHandler) listEntities(
	c echo.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) error {
	var result repositories.PaginatedResponse[*entities.Resource]
	var err error
	if len(filters) > 0 {
		result, err = h.resourceService.ListWithFilters(
			c.Request().Context(), typeSlug, filters, cursor, limit, sort)
	} else {
		result, err = h.resourceService.List(c.Request().Context(), typeSlug, cursor, limit, sort)
	}
	if err != nil {
		return respondError(c, http.StatusInternalServerError, err.Error())
	}

	var ldCtx json.RawMessage
	if rt, lookupErr := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); lookupErr == nil && rt != nil {
		ldCtx = rt.Context()
	}
	items := make([]json.RawMessage, 0, len(result.Data))
	for _, e := range result.Data {
		simplified, simplifyErr := entities.SimplifyJSONLD(e.Data(), ldCtx)
		if simplifyErr != nil {
			items = append(items, e.Data())
			continue
		}
		items = append(items, simplified)
	}
	return respondPaginated(c, http.StatusOK, items, result.Cursor, result.HasMore)
}

// parseFilters extracts _filter[field][operator]=value query params.
func parseFilters(c echo.Context) []repositories.FilterCondition {
	var filters []repositories.FilterCondition
	for key, values := range c.QueryParams() {
		if !strings.HasPrefix(key, "_filter[") || !strings.HasSuffix(key, "]") {
			continue
		}
		// Parse _filter[field][op]
		inner := key[8 : len(key)-1] // strip "_filter[" and trailing "]"
		parts := strings.SplitN(inner, "][", 2)
		if len(parts) != 2 {
			continue
		}
		field, op := parts[0], parts[1]
		if field == "" || op == "" || len(values) == 0 {
			continue
		}
		filters = append(filters, repositories.FilterCondition{
			Field: field, Operator: op, Value: values[0],
		})
	}
	return filters
}

func (h *ResourceHandler) Update(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	if _, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); err != nil {
		return respondError(c, http.StatusNotFound, "resource type not found")
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return respondError(c, http.StatusBadRequest, "failed to read request body")
	}

	entity, err := h.resourceService.Update(
		c.Request().Context(),
		application.UpdateResourceCommand{ID: c.Param("id"), Data: json.RawMessage(body)},
	)
	if err != nil {
		if errors.Is(err, entities.ErrAccessDenied) {
			return respondForbidden(c)
		}
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return respondWithResource(c, http.StatusOK, entity)
}

func (h *ResourceHandler) Delete(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	if _, err := h.resourceTypeService.GetBySlug(c.Request().Context(), typeSlug); err != nil {
		return respondError(c, http.StatusNotFound, "resource type not found")
	}

	cmd := application.DeleteResourceCommand{ID: c.Param("id")}
	if err := h.resourceService.Delete(c.Request().Context(), cmd); err != nil {
		if errors.Is(err, entities.ErrAccessDenied) {
			return respondForbidden(c)
		}
		return respondError(c, http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func wantsJSONLD(c echo.Context) bool {
	accept := c.Request().Header.Get("Accept")
	return strings.Contains(accept, "application/ld+json")
}

func respondWithResourceData(
	c echo.Context, status int, entity *entities.Resource, ldCtx json.RawMessage,
) error {
	if wantsJSONLD(c) {
		return respondRaw(c, status, entity.Data())
	}
	simplified, err := entities.SimplifyJSONLD(entity.Data(), ldCtx)
	if err != nil {
		return respondRaw(c, status, entity.Data())
	}
	return respondRaw(c, status, simplified)
}

func respondWithResource(c echo.Context, status int, entity *entities.Resource) error {
	return respond(c, status, ResourceResponse{
		ID:        entity.GetID(),
		TypeSlug:  entity.TypeSlug(),
		Data:      entity.Data(),
		Status:    entity.Status(),
		CreatedAt: entity.CreatedAt().Format(time.RFC3339),
	})
}
