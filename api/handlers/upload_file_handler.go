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
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"github.com/labstack/echo/v4"
)

// ServeUploadedFiles serves the files the local storage backend wrote under
// localPath. A file in an account folder, accounts/<account>/uploads/<name>,
// is served only to a caller whose active account is <account>. A flat file
// from before account folders existed is served to any caller the route's auth
// middleware admitted. Every other request gets the same 404 a missing file
// gets, so a probe cannot tell a file it may not read from one that does not
// exist.
//
// routePrefix is the full path the route is mounted at, ending in a slash. It
// is stripped from the request path, so it must match the registration.
func ServeUploadedFiles(routePrefix, localPath string, logger entities.Logger) echo.HandlerFunc {
	flatRoot := http.Dir(localPath)
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		name, ok := servableUploadName(routePrefix, c.Request().URL.Path, activeAccountID(c))
		if !ok {
			return uploadNotFound(c)
		}

		file, err := openUpload(localPath, flatRoot, name)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logger.Warn(ctx, "could not open uploaded file", "name", name, "error", err)
			}
			return uploadNotFound(c)
		}
		// The file is only read, so a failed close loses nothing.
		defer func() { _ = file.Close() }()

		info, err := file.Stat()
		if err != nil {
			logger.Warn(ctx, "could not stat uploaded file", "name", name, "error", err)
			return uploadNotFound(c)
		}
		if !info.Mode().IsRegular() {
			return uploadNotFound(c)
		}

		// Stop a stored file from running as a page: force a download, forbid
		// type sniffing, and deny it every resource if a browser renders it.
		// The same URL is 200 for its account and 404 for every other, so no
		// shared cache may keep the 200 and hand it to another account.
		header := c.Response().Header()
		header.Set("Content-Disposition", "attachment")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Content-Security-Policy", "default-src 'none'")
		header.Set("Cache-Control", "private, no-store")
		http.ServeContent(c.Response(), c.Request(), info.Name(), info.ModTime(), file)
		return nil
	}
}

// servableUploadName maps a request path to the name the file is opened by,
// and decides on that same decoded, cleaned name, so an encoded "../" cannot
// pass the account check and then resolve into another folder.
//
// Only two shapes are servable: a flat <name> at the root, and
// accounts/<accountID>/uploads/<name> in the caller's own account. "accounts"
// and "uploads" match exactly rather than case-folded, which also stops a
// case-insensitive filesystem from opening another account's folder through
// ACCOUNTS/<other>/.
func servableUploadName(routePrefix, requestPath, accountID string) (string, bool) {
	rest, ok := strings.CutPrefix(requestPath, routePrefix)
	if !ok {
		return "", false
	}
	cleaned := path.Clean("/" + rest)
	segments := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	switch {
	case len(segments) == 1 && segments[0] != "":
		return cleaned, true
	case len(segments) == 4 && segments[0] == "accounts" && accountID != "" &&
		segments[1] == accountID && segments[2] == "uploads":
		return cleaned, true
	default:
		return "", false
	}
}

// openUpload opens a name servableUploadName accepted. An account file opens
// through an os.Root at its account's uploads folder, so a symlink planted there
// that leads out of the folder is refused, not followed. A flat file opens as
// it did before account folders.
func openUpload(localPath string, flatRoot http.FileSystem, name string) (http.File, error) {
	dir, file := path.Split(name)
	if dir == "/" {
		return flatRoot.Open(name)
	}
	root, err := os.OpenRoot(filepath.Join(localPath, filepath.FromSlash(dir)))
	if err != nil {
		return nil, err
	}
	// A file opened through the root stays open after the root closes, and the
	// root is only read, so a failed close loses nothing.
	defer func() { _ = root.Close() }()
	return root.Open(file)
}

func activeAccountID(c echo.Context) string {
	if identity := auth.AgentFromCtx(c.Request().Context()); identity != nil {
		return identity.ActiveAccountID
	}
	return ""
}

func uploadNotFound(c echo.Context) error {
	return respondError(c, http.StatusNotFound, "file not found")
}
