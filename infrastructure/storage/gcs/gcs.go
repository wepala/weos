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

package gcs

import (
	"context"
	"fmt"
	"io"

	"weos/domain/entities"
	"weos/domain/services"
	"weos/infrastructure/storage"

	"github.com/segmentio/ksuid"

	gcsstorage "cloud.google.com/go/storage"
)

type gcsFileService struct {
	client *gcsstorage.Client
	bucket string
	logger entities.Logger
}

// New creates a FileService backed by Google Cloud Storage.
func New(client *gcsstorage.Client, bucket string, logger entities.Logger) services.FileService {
	return &gcsFileService{
		client: client,
		bucket: bucket,
		logger: logger,
	}
}

func (s *gcsFileService) Upload(
	ctx context.Context, params services.UploadParams, reader io.Reader,
) (*services.UploadResult, error) {
	id := params.ID
	if id == "" {
		id = ksuid.New().String()
	}
	if err := storage.ValidateID(id); err != nil {
		return nil, fmt.Errorf("invalid upload ID: %w", err)
	}
	safeName := storage.SanitizeFilename(params.Filename)
	key := "uploads/" + id + "-" + safeName

	obj := s.client.Bucket(s.bucket).Object(key)
	w := obj.NewWriter(ctx)
	w.ContentType = params.ContentType

	written, err := io.Copy(w, reader)
	if err != nil {
		if closeErr := w.Close(); closeErr != nil {
			s.logger.Warn(ctx, "GCS writer close also failed", "closeError", closeErr)
		}
		return nil, fmt.Errorf("write to GCS: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close GCS writer: %w", err)
	}

	url := fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, key)

	s.logger.Info(ctx, "file uploaded to GCS",
		"bucket", s.bucket, "key", key, "size", written)

	return &services.UploadResult{
		ID:          id,
		URL:         url,
		Filename:    safeName,
		ContentType: params.ContentType,
		Size:        written,
	}, nil
}
