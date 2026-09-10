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
	"strings"
	"sync/atomic"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/infrastructure/storage"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/segmentio/ksuid"
	"golang.org/x/sync/errgroup"
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
	if err := storage.ValidateID(id); err != nil {
		return nil, fmt.Errorf("invalid upload ID: %w", err)
	}
	if err := storage.ValidateAccountID(params.AccountID); err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}
	safeName := storage.SanitizeFilename(params.Filename)
	key := storage.ObjectKey(params.AccountID, id, safeName)

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

	// Use path-style URL to avoid TLS cert mismatch with dotted bucket names
	// and to work across non-standard endpoints (GovCloud, China, dualstack).
	url := fmt.Sprintf("https://s3.%s.amazonaws.com/%s/%s", s.region, s.bucket, key)

	s.logger.Info(ctx, "file uploaded to S3",
		"bucket", s.bucket, "region", s.region, "key", key, "size", cr.n)

	return &services.UploadResult{
		ID:          id,
		URL:         url,
		Filename:    safeName,
		ContentType: params.ContentType,
		Size:        cr.n,
	}, nil
}

// deleteBatchSize is the most keys one DeleteObjects call accepts.
const deleteBatchSize = 1000

// deleteBatchWorkers is how many DeleteObjects calls are in flight at once.
// A page of the listing is up to a thousand keys, one batch, so this only
// matters across pages; it keeps a large account from serialising a batch
// per round trip.
const deleteBatchWorkers = 4

// DeleteAccountFolder walks every page of ListObjectsV2 under
// accounts/<accountID>/ and deletes the keys in batches of a thousand, the
// most one DeleteObjects call accepts, a few batches at a time. A key S3
// reports it could not delete fails the call: a file left behind is data that
// was promised gone, so the caller must see it and run the deletion again.
func (s *s3FileService) DeleteAccountFolder(ctx context.Context, accountID string) error {
	if err := storage.ValidateAccountID(accountID); err != nil {
		return fmt.Errorf("invalid account ID: %w", err)
	}
	prefix := storage.AccountPrefix(accountID)
	var deleted atomic.Int64
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(deleteBatchWorkers)
	pages := s3sdk.NewListObjectsV2Paginator(s.client, &s3sdk.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			// A failed batch has already cancelled ctx; report that rather
			// than the listing it interrupted.
			if waitErr := group.Wait(); waitErr != nil {
				return waitErr
			}
			return fmt.Errorf("list S3 objects under %s: %w", prefix, err)
		}
		keys := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, object := range page.Contents {
			if object.Key == nil {
				continue
			}
			keys = append(keys, types.ObjectIdentifier{Key: object.Key})
		}
		for start := 0; start < len(keys); start += deleteBatchSize {
			batch := keys[start:min(start+deleteBatchSize, len(keys))]
			group.Go(func() error {
				n, err := s.deleteBatch(ctx, batch)
				if err != nil {
					return err
				}
				deleted.Add(int64(n))
				return nil
			})
		}
	}
	if err := group.Wait(); err != nil {
		return err
	}
	s.logger.Info(ctx, "account folder removed from S3",
		"bucket", s.bucket, "region", s.region, "prefix", prefix, "objects", deleted.Load())
	return nil
}

// deleteBatch deletes one batch of at most deleteBatchSize keys and reports
// how many went. S3 answers a partial failure with a 200 carrying per-key
// errors, so the count of errors is what decides, not the call's own error.
func (s *s3FileService) deleteBatch(ctx context.Context, keys []types.ObjectIdentifier) (int, error) {
	out, err := s.client.DeleteObjects(ctx, &s3sdk.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{Objects: keys, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return 0, fmt.Errorf("delete S3 objects: %w", err)
	}
	if len(out.Errors) > 0 {
		failed := make([]string, 0, len(out.Errors))
		for _, e := range out.Errors {
			failed = append(failed, fmt.Sprintf("%s: %s", aws.ToString(e.Key), aws.ToString(e.Message)))
		}
		return 0, fmt.Errorf("delete S3 objects: %d of %d keys were not deleted: %s",
			len(out.Errors), len(keys), strings.Join(failed, "; "))
	}
	return len(keys), nil
}
