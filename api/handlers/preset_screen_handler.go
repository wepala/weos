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
	"io"
	"io/fs"
	"net/http"
	"strings"

	"weos/application"

	"github.com/labstack/echo/v4"
)

// PresetScreenHandler serves compiled screen modules (.mjs) from preset
// Screens filesystems.
type PresetScreenHandler struct {
	registry *application.PresetRegistry
}

// NewPresetScreenHandler creates a handler that serves screen files from the
// preset registry.
func NewPresetScreenHandler(registry *application.PresetRegistry) *PresetScreenHandler {
	return &PresetScreenHandler{registry: registry}
}

// Serve handles GET /api/resource-types/presets/:name/screens/*filepath.
// It looks up the preset, opens the requested file from its Screens FS,
// and serves it with appropriate headers. Only .mjs files are served.
func (h *PresetScreenHandler) Serve(c echo.Context) error {
	name := c.Param("name")
	preset, ok := h.registry.Get(name)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "preset not found"})
	}
	if preset.Screens == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "preset has no screens"})
	}

	filePath := c.Param("*")
	filePath = strings.TrimPrefix(filePath, "/")
	if filePath == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file path required"})
	}
	if !strings.HasSuffix(filePath, ".mjs") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "only .mjs files are served"})
	}
	if !fs.ValidPath(filePath) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid path"})
	}
	if strings.Count(filePath, "/") != 1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid screen path"})
	}

	f, err := preset.Screens.Open(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "screen file not found"})
		}
		c.Logger().Errorf("preset screen handler: open %s/%s: %v", name, filePath, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	defer func() { _ = f.Close() }()

	c.Response().Header().Set("Content-Type", "application/javascript; charset=utf-8")
	c.Response().Header().Set("Cache-Control", "private, max-age=3600")
	c.Response().WriteHeader(http.StatusOK)
	if _, copyErr := io.Copy(c.Response(), f); copyErr != nil {
		c.Logger().Errorf("preset screen handler: io.Copy %s/%s: %v", name, filePath, copyErr)
	}
	return nil
}
