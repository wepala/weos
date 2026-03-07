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
	"weos/domain/repositories"

	"github.com/labstack/echo/v4"
)

type TemplateHandler struct {
	service application.TemplateService
}

func NewTemplateHandler(service application.TemplateService) *TemplateHandler {
	return &TemplateHandler{service: service}
}

type CreateTemplateRequest struct {
	ThemeID string `json:"theme_id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
}

type UpdateTemplateRequest struct {
	ThemeID     string `json:"theme_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	FilePath    string `json:"file_path"`
	Status      string `json:"status"`
}

type TemplateResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

func (h *TemplateHandler) Create(c echo.Context) error {
	var req CreateTemplateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Create(
		c.Request().Context(),
		application.CreateTemplateCommand{
			ThemeID: req.ThemeID,
			Name:    req.Name,
			Slug:    req.Slug,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toTemplateResponse(entity))
}

func (h *TemplateHandler) Get(c echo.Context) error {
	entity, err := h.service.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "template not found"})
	}
	return c.JSON(http.StatusOK, toTemplateResponse(entity))
}

func (h *TemplateHandler) List(c echo.Context) error {
	cursor := c.QueryParam("cursor")
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	themeID := c.QueryParam("theme_id")

	if themeID != "" {
		res, err := h.service.ListByThemeID(
			c.Request().Context(), themeID, cursor, limit)
		if err != nil {
			return c.JSON(http.StatusInternalServerError,
				map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, templateListResponse(res))
	}

	res, err := h.service.List(c.Request().Context(), cursor, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, templateListResponse(res))
}

func (h *TemplateHandler) Update(c echo.Context) error {
	var req UpdateTemplateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Update(
		c.Request().Context(),
		application.UpdateTemplateCommand{
			ID:          c.Param("id"),
			ThemeID:     req.ThemeID,
			Name:        req.Name,
			Slug:        req.Slug,
			Description: req.Description,
			FilePath:    req.FilePath,
			Status:      req.Status,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toTemplateResponse(entity))
}

func (h *TemplateHandler) Delete(c echo.Context) error {
	cmd := application.DeleteTemplateCommand{ID: c.Param("id")}
	if err := h.service.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func toTemplateResponse(e *entities.Template) TemplateResponse {
	return TemplateResponse{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		FilePath:    e.FilePath(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt().Format(time.RFC3339),
	}
}

func templateListResponse(
	res repositories.PaginatedResponse[*entities.Template],
) map[string]interface{} {
	items := make([]TemplateResponse, 0, len(res.Data))
	for _, e := range res.Data {
		items = append(items, toTemplateResponse(e))
	}
	return map[string]interface{}{
		"data":     items,
		"cursor":   res.Cursor,
		"has_more": res.HasMore,
	}
}
