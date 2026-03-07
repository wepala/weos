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
	"net/http"
	"strconv"
	"time"

	"weos/application"
	"weos/domain/entities"

	"github.com/labstack/echo/v4"
)

type SectionHandler struct {
	service application.SectionService
}

func NewSectionHandler(service application.SectionService) *SectionHandler {
	return &SectionHandler{service: service}
}

type CreateSectionRequest struct {
	PageID string `json:"page_id"`
	Name   string `json:"name"`
	Slot   string `json:"slot"`
}

type UpdateSectionRequest struct {
	PageID     string `json:"page_id"`
	Name       string `json:"name"`
	Slot       string `json:"slot"`
	EntityType string `json:"entity_type"`
	Content    string `json:"content"`
	Position   int    `json:"position"`
}

type SectionResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Slot       string `json:"slot"`
	EntityType string `json:"entity_type,omitempty"`
	Content    string `json:"content,omitempty"`
	Position   int    `json:"position"`
	CreatedAt  string `json:"created_at"`
}

func (h *SectionHandler) Create(c echo.Context) error {
	var req CreateSectionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Create(
		c.Request().Context(),
		application.CreateSectionCommand{
			PageID: req.PageID,
			Name:   req.Name,
			Slot:   req.Slot,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toSectionResponse(entity))
}

func (h *SectionHandler) Get(c echo.Context) error {
	entity, err := h.service.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "section not found"})
	}
	return c.JSON(http.StatusOK, toSectionResponse(entity))
}

func (h *SectionHandler) List(c echo.Context) error {
	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	pageID := c.QueryParam("page_id")
	if pageID != "" {
		return h.listByPage(c, pageID, cursor, limit)
	}
	return h.listAll(c, cursor, limit)
}

func (h *SectionHandler) listAll(
	c echo.Context, cursor string, limit int,
) error {
	result, err := h.service.List(c.Request().Context(), cursor, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toSectionListResponse(
		result.Data, result.Cursor, result.HasMore))
}

func (h *SectionHandler) listByPage(
	c echo.Context, pageID, cursor string, limit int,
) error {
	result, err := h.service.ListByPageID(
		c.Request().Context(), pageID, cursor, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toSectionListResponse(
		result.Data, result.Cursor, result.HasMore))
}

func toSectionListResponse(
	data []*entities.Section, cursor string, hasMore bool,
) map[string]interface{} {
	items := make([]SectionResponse, 0, len(data))
	for _, e := range data {
		items = append(items, toSectionResponse(e))
	}
	return map[string]interface{}{
		"data":     items,
		"cursor":   cursor,
		"has_more": hasMore,
	}
}

func (h *SectionHandler) Update(c echo.Context) error {
	var req UpdateSectionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Update(
		c.Request().Context(),
		application.UpdateSectionCommand{
			ID:         c.Param("id"),
			PageID:     req.PageID,
			Name:       req.Name,
			Slot:       req.Slot,
			EntityType: req.EntityType,
			Content:    req.Content,
			Position:   req.Position,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toSectionResponse(entity))
}

func (h *SectionHandler) Delete(c echo.Context) error {
	cmd := application.DeleteSectionCommand{ID: c.Param("id")}
	if err := h.service.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func toSectionResponse(e *entities.Section) SectionResponse {
	return SectionResponse{
		ID:         e.GetID(),
		Name:       e.Name(),
		Slot:       e.Slot(),
		EntityType: e.EntityType(),
		Content:    e.Content(),
		Position:   e.Position(),
		CreatedAt:  e.CreatedAt().Format(time.RFC3339),
	}
}
