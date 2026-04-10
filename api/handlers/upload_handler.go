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
	"fmt"
	"io"
	"net/http"

	"weos/application"
	"weos/domain/entities"

	"github.com/labstack/echo/v4"
)

// sniffBufSize is the number of bytes read for http.DetectContentType.
const sniffBufSize = 512

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
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return respondError(c, http.StatusRequestEntityTooLarge, "file exceeds maximum upload size")
		}
		return respondError(c, http.StatusBadRequest, "missing or invalid file field")
	}

	file, err := fh.Open()
	if err != nil {
		return respondError(c, http.StatusBadRequest, "failed to read uploaded file")
	}
	defer func() { _ = file.Close() }()

	// Derive Content-Type server-side by sniffing the first 512 bytes
	// rather than trusting the client-supplied multipart header, which
	// could specify text/html or image/svg+xml to enable stored XSS.
	contentType, err := detectContentType(file)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to detect file type")
	}

	ctx := c.Request().Context()
	params := application.UploadParams{
		Filename:    fh.Filename,
		ContentType: contentType,
	}
	result, err := h.fileService.Upload(ctx, params, file)
	if err != nil {
		h.logger.Error(ctx, "file upload failed",
			"filename", fh.Filename, "contentType", contentType, "error", err)
		return respondError(c, http.StatusInternalServerError, "file upload failed")
	}

	return respond(c, http.StatusCreated, result)
}

// detectContentType reads up to 512 bytes from the file to sniff the MIME
// type, then seeks back to the start so the full file can still be read.
// Returns an error if the file cannot be rewound after sniffing, since
// proceeding would silently corrupt the stored file (missing sniffed bytes).
func detectContentType(file io.ReadSeeker) (string, error) {
	buf := make([]byte, sniffBufSize)
	n, _ := file.Read(buf)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind after content-type sniff: %w", err)
	}
	if n == 0 {
		return "application/octet-stream", nil
	}
	return http.DetectContentType(buf[:n]), nil
}
