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
	"context"
	"fmt"
	"io"

	"weos/domain/entities"
	"weos/domain/services"
	"weos/infrastructure/storage"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/segmentio/ksuid"
)

type s3FileService struct {
	client *s3sdk.Client
	bucket string
	region string
	logger entities.Logger
}

// New creates a FileService backed by AWS S3.
func New(client *s3sdk.Client, bucket, region string, logger entities.Logger) services.FileService {
	return &s3FileService{
		client: client,
		bucket: bucket,
		region: region,
		logger: logger,
	}
}

// countingReader wraps an io.Reader and counts bytes read.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (s *s3FileService) Upload(
	ctx context.Context, params services.UploadParams, reader io.Reader,
) (*services.UploadResult, error) {
	id := params.ID
	if id == "" {
		id = ksuid.New().String()
	}
	safeName := storage.SanitizeFilename(params.Filename)
	key := "uploads/" + id + "-" + safeName

	cr := &countingReader{r: reader}

	_, err := s.client.PutObject(ctx, &s3sdk.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        cr,
		ContentType: aws.String(params.ContentType),
	})
	if err != nil {
		return nil, fmt.Errorf("upload to S3: %w", err)
	}

	s.logger.Info(ctx, "file uploaded to S3",
		"bucket", s.bucket, "key", key, "size", cr.n)

	return &services.UploadResult{
		ID:          id,
		Filename:    safeName,
		ContentType: params.ContentType,
		Size:        cr.n,
	}, nil
}
