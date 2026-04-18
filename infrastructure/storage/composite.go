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

package storage

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/services"

	"github.com/segmentio/ksuid"
)

type compositeFileService struct {
	primary     services.FileService
	secondaries []services.FileService
	logger      entities.Logger
}

// NewComposite creates a FileService that fans out uploads to a primary
// and zero or more secondary backends. A single ID is pre-generated and
// shared across all backends so replicas are correlated. The result from
// the first secondary that succeeds is returned (providing an app-hosted
// URL), falling back to the primary result. Upload data is spooled to a
// temporary file to avoid holding the entire body in memory.
func NewComposite(
	primary services.FileService,
	secondaries []services.FileService,
	logger entities.Logger,
) services.FileService {
	return &compositeFileService{
		primary:     primary,
		secondaries: secondaries,
		logger:      logger,
	}
}

func (c *compositeFileService) Upload(
	ctx context.Context, params services.UploadParams, reader io.Reader,
) (*services.UploadResult, error) {
	// Pre-generate a shared ID so all backends use the same identifier,
	// making replicas traceable/correlated across primary and secondaries.
	if params.ID == "" {
		params.ID = ksuid.New().String()
	}

	// Spool to a temp file so concurrent large uploads don't exhaust RAM.
	tmp, err := os.CreateTemp("", "weos-upload-*")
	if err != nil {
		return nil, fmt.Errorf("create temp spool file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if _, err := io.Copy(tmp, reader); err != nil {
		return nil, fmt.Errorf("spool upload data: %w", err)
	}

	// Rewind for primary.
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind spool file: %w", err)
	}

	primaryResult, err := c.primary.Upload(ctx, params, tmp)
	if err != nil {
		return nil, fmt.Errorf("primary upload: %w", err)
	}

	// Upload to secondaries. Use the first secondary's result as the
	// returned result since secondaries (typically local) provide
	// app-hosted URLs that are directly accessible by clients, while
	// cloud primary URLs may require authentication.
	var returnResult *services.UploadResult
	for i, sec := range c.secondaries {
		if _, seekErr := tmp.Seek(0, io.SeekStart); seekErr != nil {
			c.logger.Warn(ctx, "failed to rewind spool for secondary",
				"backendIndex", i, "error", seekErr)
			continue
		}
		secResult, secErr := sec.Upload(ctx, params, tmp)
		if secErr != nil {
			c.logger.Warn(ctx, "secondary upload failed",
				"backendIndex", i, "filename", params.Filename,
				"contentType", params.ContentType, "error", secErr)
			continue
		}
		if returnResult == nil {
			returnResult = secResult
		}
	}

	// If a secondary provided an app-hosted URL, prefer it; otherwise
	// fall back to the primary result.
	if returnResult != nil {
		// Preserve the primary's size if the secondary didn't report one
		// (e.g., cloud backends may not return size).
		if returnResult.Size == 0 {
			returnResult.Size = primaryResult.Size
		}
		return returnResult, nil
	}

	return primaryResult, nil
}
