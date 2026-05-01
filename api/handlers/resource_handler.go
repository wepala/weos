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

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/labstack/echo/v4"
)

type ResourceHandler struct {
	resourceService     application.ResourceService
	resourceTypeService application.ResourceTypeService
	logger              entities.Logger
}

func NewResourceHandler(
	rs application.ResourceService,
	rts application.ResourceTypeService,
	logger entities.Logger,
) *ResourceHandler {
	return &ResourceHandler{
		resourceService:     rs,
		resourceTypeService: rts,
		logger:              logger,
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
		if errors.Is(err, application.ErrValidation) {
			return respondError(c, http.StatusBadRequest, err.Error())
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

	// Prefer the flat projection path for non-JSON-LD requests so the response
	// includes denormalized `<field>Display` columns for x-resource-type refs.
	// JSON-LD clients still get the canonical entity via respondWithResourceData.
	ctx := c.Request().Context()
	id := c.Param("id")
	if !wantsJSONLD(c) {
		row, flatErr := h.resourceService.GetFlat(ctx, typeSlug, id)
		switch {
		case flatErr == nil && row != nil:
			return respond(c, http.StatusOK, row)
		case errors.Is(flatErr, entities.ErrAccessDenied):
			return respondForbidden(c)
		case errors.Is(flatErr, repositories.ErrNoProjectionTable),
			errors.Is(flatErr, repositories.ErrNotFound):
			// Legitimate fall-through: no projection table or row missing.
			// The canonical-entity path below may still find it (dual-read
			// during migrations, pre-projection data).
		default:
			if flatErr != nil {
				h.logger.Error(ctx, "flat resource lookup failed",
					"typeSlug", typeSlug, "id", id, "error", flatErr)
				return respondError(c, http.StatusInternalServerError,
					"failed to load resource")
			}
		}
	}

	entity, err := h.resourceService.GetByID(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, entities.ErrAccessDenied):
			return respondForbidden(c)
		case errors.Is(err, repositories.ErrNotFound):
			return respondError(c, http.StatusNotFound, "resource not found")
		default:
			h.logger.Error(ctx, "canonical resource lookup failed",
				"typeSlug", typeSlug, "id", id, "error", err)
			return respondError(c, http.StatusInternalServerError, "failed to load resource")
		}
	}
	// Type-slug mismatch guard: a URL like /resources/course/<id-of-a-product>
	// must not return the product entity simplified with the course context.
	// GetByID does not enforce this on its own, and the flat path's mismatch
	// signal (ErrNotFound) collides with "row missing" in the fall-through
	// switch above — so the canonical path needs its own explicit check.
	if entity.TypeSlug() != typeSlug {
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
		if errors.Is(err, application.ErrValidation) {
			return respondError(c, http.StatusBadRequest, err.Error())
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
	// 204 No Content intentionally has no body; any accumulated messages are not sent.
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
		// JSON-LD clients expect a valid JSON-LD document at the top level,
		// so we bypass the envelope and return the raw data directly.
		return c.Blob(status, "application/ld+json", entity.Data())
	}
	simplified, err := entities.SimplifyJSONLD(entity.Data(), ldCtx)
	if err != nil {
		entities.AddMessage(c.Request().Context(), entities.Message{
			Type: "warning",
			Text: "response contains unsimplified JSON-LD payload due to simplification error",
		})
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
