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
	"errors"
	"net/http"

	"weos/application"
	"weos/domain/repositories"

	"github.com/labstack/echo/v4"
)

type ResourceTypePresetHandler struct {
	service application.ResourceTypeService
}

func NewResourceTypePresetHandler(service application.ResourceTypeService) *ResourceTypePresetHandler {
	return &ResourceTypePresetHandler{service: service}
}

type presetResponse struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Types       []string            `json:"types"`
	Screens     map[string][]string `json:"screens,omitempty"`
}

func (h *ResourceTypePresetHandler) List(c echo.Context) error {
	defs := h.service.ListPresets()
	out := make([]presetResponse, 0, len(defs))
	for _, d := range defs {
		slugs := make([]string, len(d.Types))
		for i, t := range d.Types {
			slugs[i] = t.Slug
		}
		out = append(out, presetResponse{
			Name:        d.Name,
			Description: d.Description,
			Types:       slugs,
			Screens:     d.ScreenManifest(),
		})
	}
	return respond(c, http.StatusOK, out)
}

func (h *ResourceTypePresetHandler) Install(c echo.Context) error {
	name := c.Param("name")
	update := c.QueryParam("update") == "true"
	result, err := h.service.InstallPreset(c.Request().Context(), name, update)
	if err != nil {
		return respondError(c, http.StatusBadRequest, err.Error())
	}
	return respond(c, http.StatusOK, result)
}

func (h *ResourceTypePresetHandler) ListBehaviors(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	behaviors, err := h.service.ListBehaviors(c.Request().Context(), typeSlug)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return c.JSON(http.StatusNotFound,
				map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": "failed to list behaviors"})
	}
	return c.JSON(http.StatusOK, map[string]any{"data": behaviors})
}

func (h *ResourceTypePresetHandler) SetBehaviors(c echo.Context) error {
	typeSlug := c.Param("typeSlug")
	var body struct {
		Slugs *[]string `json:"slugs"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "invalid request body"})
	}
	if body.Slugs == nil {
		return c.JSON(http.StatusBadRequest,
			map[string]string{"error": "slugs field is required"})
	}
	ctx := c.Request().Context()
	if err := h.service.SetBehaviors(ctx, typeSlug, *body.Slugs); err != nil {
		if errors.Is(err, application.ErrForbidden) {
			return c.JSON(http.StatusForbidden,
				map[string]string{"error": err.Error()})
		}
		if errors.Is(err, repositories.ErrNotFound) {
			return c.JSON(http.StatusNotFound,
				map[string]string{"error": err.Error()})
		}
		if errors.Is(err, application.ErrValidation) {
			return c.JSON(http.StatusBadRequest,
				map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError,
			map[string]string{"error": "failed to set behaviors"})
	}
	return c.JSON(http.StatusOK, map[string]bool{"success": true})
}
