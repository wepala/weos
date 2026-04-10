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

package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"weos/domain/entities"
	"weos/domain/services"
	"weos/infrastructure/storage"

	"github.com/segmentio/ksuid"
)

type localFileService struct {
	basePath string
	baseURL  string
	logger   entities.Logger
}

// New creates a FileService that stores files on the local filesystem.
// basePath is the directory to write files into; baseURL is the URL prefix
// used to construct the public URL for each uploaded file.
func New(basePath, baseURL string, logger entities.Logger) services.FileService {
	return &localFileService{
		basePath: basePath,
		baseURL:  baseURL,
		logger:   logger,
	}
}

func (s *localFileService) Upload(
	ctx context.Context, filename string, contentType string, reader io.Reader,
) (*services.UploadResult, error) {
	if err := os.MkdirAll(s.basePath, 0o755); err != nil {
		return nil, fmt.Errorf("create upload directory: %w", err)
	}

	id := ksuid.New().String()
	safeName := storage.SanitizeFilename(filename)
	diskName := id + "-" + safeName
	fullPath := filepath.Join(s.basePath, diskName)

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}

	written, err := io.Copy(f, reader)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("write file: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("sync file to disk: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("close file: %w", err)
	}

	s.logger.Info(ctx, "file uploaded to local storage",
		"path", fullPath, "size", written)

	return &services.UploadResult{
		ID:          id,
		URL:         s.baseURL + "/" + diskName,
		Filename:    safeName,
		ContentType: contentType,
		Size:        written,
	}, nil
}
