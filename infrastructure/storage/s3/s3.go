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

package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"weos/domain/entities"
	"weos/domain/services"
	"weos/infrastructure/storage"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3FileService struct {
	client *s3.Client
	bucket string
	region string
	logger entities.Logger
}

// New creates a FileService backed by AWS S3.
func New(client *s3.Client, bucket, region string, logger entities.Logger) services.FileService {
	return &s3FileService{
		client: client,
		bucket: bucket,
		region: region,
		logger: logger,
	}
}

func (s *s3FileService) Upload(
	ctx context.Context, filename string, contentType string, reader io.Reader,
) (*services.UploadResult, error) {
	id, key := storage.GenerateObjectKeyWithID("uploads", filename)

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read upload body: %w", err)
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return nil, fmt.Errorf("upload to S3: %w", err)
	}

	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, key)

	s.logger.Info(ctx, "file uploaded to S3",
		"bucket", s.bucket, "key", key, "size", len(body))

	return &services.UploadResult{
		ID:          id,
		URL:         url,
		Filename:    storage.SanitizeFilename(filename),
		ContentType: contentType,
		Size:        int64(len(body)),
	}, nil
}
