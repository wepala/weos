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

	"weos/domain/entities"
	"weos/domain/services"
)

type compositeFileService struct {
	primary     services.FileService
	secondaries []services.FileService
	logger      entities.Logger
}

// NewComposite creates a FileService that fans out uploads to a primary
// and zero or more secondary backends. The primary result is returned;
// secondary failures are logged but do not cause the upload to fail.
// Upload data is spooled to a temporary file to avoid holding the entire
// body in memory during fan-out.
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
	ctx context.Context, filename string, contentType string, reader io.Reader,
) (*services.UploadResult, error) {
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

	result, err := c.primary.Upload(ctx, filename, contentType, tmp)
	if err != nil {
		return nil, fmt.Errorf("primary upload: %w", err)
	}

	for i, sec := range c.secondaries {
		if _, seekErr := tmp.Seek(0, io.SeekStart); seekErr != nil {
			c.logger.Warn(ctx, "failed to rewind spool for secondary",
				"backendIndex", i, "error", seekErr)
			continue
		}
		if _, secErr := sec.Upload(ctx, filename, contentType, tmp); secErr != nil {
			c.logger.Warn(ctx, "secondary upload failed",
				"backendIndex", i, "filename", filename,
				"contentType", contentType, "error", secErr)
		}
	}

	return result, nil
}
