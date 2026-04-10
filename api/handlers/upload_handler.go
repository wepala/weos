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

	"weos/application"
	"weos/domain/entities"

	"github.com/labstack/echo/v4"
)

// UploadHandler handles file upload requests.
type UploadHandler struct {
	fileService    application.FileService
	logger         entities.Logger
	maxUploadBytes int64
}

// NewUploadHandler creates a new UploadHandler.
// maxUploadBytes limits the request body size; use 0 for the default (50 MB).
func NewUploadHandler(
	fileService application.FileService, logger entities.Logger, maxUploadBytes int64,
) *UploadHandler {
	if maxUploadBytes <= 0 {
		maxUploadBytes = 50 << 20 // 50 MB
	}
	return &UploadHandler{
		fileService:    fileService,
		logger:         logger,
		maxUploadBytes: maxUploadBytes,
	}
}

// Upload accepts a multipart file upload and stores it via the FileService.
func (h *UploadHandler) Upload(c echo.Context) error {
	// Enforce upload size limit before reading any data.
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, h.maxUploadBytes)

	fh, err := c.FormFile("file")
	if err != nil {
		if err.Error() == "http: request body too large" {
			return respondError(c, http.StatusRequestEntityTooLarge, "file exceeds maximum upload size")
		}
		return respondError(c, http.StatusBadRequest, "missing or invalid file field")
	}

	file, err := fh.Open()
	if err != nil {
		return respondError(c, http.StatusBadRequest, "failed to read uploaded file")
	}
	defer func() { _ = file.Close() }()

	contentType := fh.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	ctx := c.Request().Context()
	result, err := h.fileService.Upload(ctx, fh.Filename, contentType, file)
	if err != nil {
		h.logger.Error(ctx, "file upload failed",
			"filename", fh.Filename, "contentType", contentType, "error", err)
		return respondError(c, http.StatusInternalServerError, "file upload failed")
	}

	return respond(c, http.StatusCreated, result)
}
