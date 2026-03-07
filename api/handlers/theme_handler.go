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

type ThemeHandler struct {
	service application.ThemeService
}

func NewThemeHandler(service application.ThemeService) *ThemeHandler {
	return &ThemeHandler{service: service}
}

type CreateThemeRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type UpdateThemeRequest struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Description  string `json:"description"`
	Version      string `json:"version"`
	ThumbnailURL string `json:"thumbnail_url"`
	Status       string `json:"status"`
}

type ThemeResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Description  string `json:"description,omitempty"`
	Version      string `json:"version,omitempty"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
}

func (h *ThemeHandler) Create(c echo.Context) error {
	var req CreateThemeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Create(
		c.Request().Context(),
		application.CreateThemeCommand{Name: req.Name, Slug: req.Slug},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, toThemeResponse(entity))
}

func (h *ThemeHandler) Get(c echo.Context) error {
	entity, err := h.service.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound,
			map[string]string{"error": "theme not found"})
	}
	return c.JSON(http.StatusOK, toThemeResponse(entity))
}

func (h *ThemeHandler) List(c echo.Context) error {
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
	items := make([]ThemeResponse, 0, len(result.Data))
	for _, e := range result.Data {
		items = append(items, toThemeResponse(e))
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"data":     items,
		"cursor":   result.Cursor,
		"has_more": result.HasMore,
	})
}

func (h *ThemeHandler) Update(c echo.Context) error {
	var req UpdateThemeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	entity, err := h.service.Update(
		c.Request().Context(),
		application.UpdateThemeCommand{
			ID:           c.Param("id"),
			Name:         req.Name,
			Slug:         req.Slug,
			Description:  req.Description,
			Version:      req.Version,
			ThumbnailURL: req.ThumbnailURL,
			Status:       req.Status,
		},
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, toThemeResponse(entity))
}

func (h *ThemeHandler) Delete(c echo.Context) error {
	cmd := application.DeleteThemeCommand{ID: c.Param("id")}
	if err := h.service.Delete(c.Request().Context(), cmd); err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *ThemeHandler) Upload(c echo.Context) error {
	file, err := c.FormFile("theme")
	if err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "theme zip file is required"})
	}

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": "failed to open uploaded file"})
	}
	defer src.Close()

	result, err := h.service.Upload(
		c.Request().Context(),
		application.UploadThemeCommand{
			ZipReader: src,
			ZipSize:   file.Size,
			Name:      c.FormValue("name"),
			FileName:  file.Filename,
		},
	)
	if err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": err.Error()})
	}

	templates := make([]map[string]string, 0, len(result.Templates))
	for _, t := range result.Templates {
		templates = append(templates, map[string]string{
			"id":   t.GetID(),
			"name": t.Name(),
			"slug": t.Slug(),
			"file": result.TemplateFiles[t.GetID()],
		})
	}

	files := make([]map[string]interface{}, 0, len(result.Files))
	for _, f := range result.Files {
		files = append(files, map[string]interface{}{
			"path": f.Path,
			"size": f.Size,
		})
	}

	themeID := result.Theme.GetID()
	links := map[string]string{
		"self":           "/api/themes/" + themeID,
		"update_theme":   "/api/themes/" + themeID,
		"delete_theme":   "/api/themes/" + themeID,
		"list_templates": "/api/templates?theme_id=" + themeID,
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"theme":     toThemeResponse(result.Theme),
		"templates": templates,
		"files":     files,
		"links":     links,
	})
}

func toThemeResponse(e *entities.Theme) ThemeResponse {
	return ThemeResponse{
		ID:           e.GetID(),
		Name:         e.Name(),
		Slug:         e.Slug(),
		Description:  e.Description(),
		Version:      e.Version(),
		ThumbnailURL: e.ThumbnailURL(),
		Status:       e.Status(),
		CreatedAt:    e.CreatedAt().Format(time.RFC3339),
	}
}
