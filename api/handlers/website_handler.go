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

type WebsiteHandler struct {
	service application.WebsiteService
}

func NewWebsiteHandler(service application.WebsiteService) *WebsiteHandler {
	return &WebsiteHandler{service: service}
}

type CreateWebsiteRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Slug string `json:"slug"`
}

type UpdateWebsiteRequest struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Status      string `json:"status"`
}

type WebsiteResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Language    string `json:"language"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

func (h *WebsiteHandler) Create(c echo.Context) error {
	var req CreateWebsiteRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Create(
		c.Request().Context(),
		application.CreateWebsiteCommand{Name: req.Name, URL: req.URL, Slug: req.Slug},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toWebsiteResponse(entity))
}

func (h *WebsiteHandler) Get(c echo.Context) error {
	entity, err := h.service.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "website not found"})
	}
	return c.JSON(http.StatusOK, toWebsiteResponse(entity))
}

func (h *WebsiteHandler) List(c echo.Context) error {
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
	items := make([]WebsiteResponse, 0, len(result.Data))
	for _, e := range result.Data {
		items = append(items, toWebsiteResponse(e))
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"data":     items,
		"cursor":   result.Cursor,
		"has_more": result.HasMore,
	})
}

func (h *WebsiteHandler) Update(c echo.Context) error {
	var req UpdateWebsiteRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Update(
		c.Request().Context(),
		application.UpdateWebsiteCommand{
			ID:          c.Param("id"),
			Name:        req.Name,
			URL:         req.URL,
			Description: req.Description,
			Language:    req.Language,
			Status:      req.Status,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toWebsiteResponse(entity))
}

func (h *WebsiteHandler) Delete(c echo.Context) error {
	cmd := application.DeleteWebsiteCommand{ID: c.Param("id")}
	if err := h.service.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func toWebsiteResponse(e *entities.Website) WebsiteResponse {
	return WebsiteResponse{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		URL:         e.URL(),
		Description: e.Description(),
		Language:    e.Language(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt().Format(time.RFC3339),
	}
}
