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

package services

import (
	"context"
	"io"
)

// UploadParams holds parameters for an upload operation.
// When ID is non-empty, backends must use it as the upload identifier
// instead of generating their own, ensuring correlation across replicas.
type UploadParams struct {
	Filename    string
	ContentType string
	// ID is an optional caller-supplied identifier. When set, backends use
	// this value as the upload ID to ensure consistent identification across
	// primary and secondary replicas.
	ID string
}

// UploadResult contains the metadata returned after a successful file upload.
type UploadResult struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// FileService defines the interface for uploading files to a storage backend.
type FileService interface {
	Upload(ctx context.Context, params UploadParams, reader io.Reader) (*UploadResult, error)
}
