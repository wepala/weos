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

type PageHandler struct {
	service application.PageService
}

func NewPageHandler(service application.PageService) *PageHandler {
	return &PageHandler{service: service}
}

type CreatePageRequest struct {
	WebsiteID string `json:"website_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
}

type UpdatePageRequest struct {
	WebsiteID   string `json:"website_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Template    string `json:"template"`
	Position    int    `json:"position"`
	Status      string `json:"status"`
}

type PageResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	Template    string `json:"template,omitempty"`
	Position    int    `json:"position"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

func (h *PageHandler) Create(c echo.Context) error {
	var req CreatePageRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Create(
		c.Request().Context(),
		application.CreatePageCommand{
			WebsiteID: req.WebsiteID,
			Name:      req.Name,
			Slug:      req.Slug,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toPageResponse(entity))
}

func (h *PageHandler) Get(c echo.Context) error {
	entity, err := h.service.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "page not found"})
	}
	return c.JSON(http.StatusOK, toPageResponse(entity))
}

func (h *PageHandler) List(c echo.Context) error {
	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	websiteID := c.QueryParam("website_id")
	if websiteID != "" {
		return h.listByWebsite(c, websiteID, cursor, limit)
	}
	return h.listAll(c, cursor, limit)
}

func (h *PageHandler) listAll(
	c echo.Context, cursor string, limit int,
) error {
	result, err := h.service.List(c.Request().Context(), cursor, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toPageListResponse(result.Data,
		result.Cursor, result.HasMore))
}

func (h *PageHandler) listByWebsite(
	c echo.Context, websiteID, cursor string, limit int,
) error {
	result, err := h.service.ListByWebsiteID(
		c.Request().Context(), websiteID, cursor, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toPageListResponse(result.Data,
		result.Cursor, result.HasMore))
}

func toPageListResponse(
	data []*entities.Page, cursor string, hasMore bool,
) map[string]interface{} {
	items := make([]PageResponse, 0, len(data))
	for _, e := range data {
		items = append(items, toPageResponse(e))
	}
	return map[string]interface{}{
		"data":     items,
		"cursor":   cursor,
		"has_more": hasMore,
	}
}

func (h *PageHandler) Update(c echo.Context) error {
	var req UpdatePageRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Update(
		c.Request().Context(),
		application.UpdatePageCommand{
			ID:          c.Param("id"),
			WebsiteID:   req.WebsiteID,
			Name:        req.Name,
			Slug:        req.Slug,
			Description: req.Description,
			Template:    req.Template,
			Position:    req.Position,
			Status:      req.Status,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toPageResponse(entity))
}

func (h *PageHandler) Delete(c echo.Context) error {
	cmd := application.DeletePageCommand{ID: c.Param("id")}
	if err := h.service.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func toPageResponse(e *entities.Page) PageResponse {
	return PageResponse{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		Template:    e.Template(),
		Position:    e.Position(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt().Format(time.RFC3339),
	}
}
