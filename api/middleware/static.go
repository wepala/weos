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

package middleware

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
)

// StaticConfig holds configuration for the static file middleware.
type StaticConfig struct {
	// Filesystem is the embedded filesystem containing static assets.
	Filesystem fs.FS
	// Root is the root directory within the filesystem (e.g., "dist").
	Root string
}

// Static returns Echo middleware that serves embedded static files with SPA fallback.
// Requests with the /api prefix are skipped and passed to the next handler.
// For all other paths, the middleware tries to serve the requested file from the
// embedded filesystem. If the file is not found, it falls back to index.html
// for client-side SPA routing.
func Static(cfg StaticConfig) echo.MiddlewareFunc {
	root, err := fs.Sub(cfg.Filesystem, cfg.Root)
	if err != nil {
		panic("static middleware: invalid root directory: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(root))

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			reqPath := c.Request().URL.Path

			// Skip API and MCP routes - let them pass through to registered handlers
			if strings.HasPrefix(reqPath, "/api") || strings.HasPrefix(reqPath, "/mcp") {
				return next(c)
			}

			// Clean the path and strip leading slash
			filePath := path.Clean(strings.TrimPrefix(reqPath, "/"))
			if filePath == "." || filePath == "" {
				filePath = "index.html"
			}

			// Try to open the requested file to check if it exists.
			f, err := root.Open(filePath)
			if err == nil {
				_ = f.Close()
				setCacheHeaders(c, filePath)
				fileServer.ServeHTTP(c.Response(), c.Request())
				return nil
			}

			// SPA fallback: serve index.html only for missing files.
			// Return 500 for real IO errors (permissions, corrupt embed, etc.).
			if !errors.Is(err, fs.ErrNotExist) {
				return c.String(http.StatusInternalServerError, "internal server error")
			}
			setCacheHeaders(c, "index.html")
			c.Request().URL.Path = "/"
			fileServer.ServeHTTP(c.Response(), c.Request())
			return nil
		}
	}
}

// setCacheHeaders sets appropriate Cache-Control headers based on file extension.
// Static assets (JS, CSS, fonts, images) get long cache with immutable directive.
// HTML files get no-cache to ensure the latest version is always served.
func setCacheHeaders(c echo.Context, filePath string) {
	ext := path.Ext(filePath)
	switch ext {
	case ".js", ".css", ".woff", ".woff2", ".ttf", ".eot",
		".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp":
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
}
