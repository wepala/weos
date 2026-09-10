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
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/infrastructure/storage"

	"github.com/segmentio/ksuid"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"

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
	if err := storage.ValidateAccountID(params.AccountID); err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}
	safeName := storage.SanitizeFilename(params.Filename)
	key := storage.ObjectKey(params.AccountID, id, safeName)

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

// deleteWorkers is how many objects are deleted at once. GCS has no batch
// delete, and one object at a time is thirty to a hundred milliseconds each,
// so an account with a thousand photos would take a minute or more inside one
// request; this bounds the walk to a few seconds without flooding the bucket.
const deleteWorkers = 16

// DeleteAccountFolder lists every object under accounts/<accountID>/ and
// deletes each one, deleteWorkers at a time. GCS has no folders, so the
// prefix is the folder, and an account with nothing stored lists nothing and
// is not an error. A delete that fails stops the walk: an object left behind
// is data that was promised gone, so the caller must see the failure and run
// the deletion again.
func (s *gcsFileService) DeleteAccountFolder(ctx context.Context, accountID string) error {
	if err := storage.ValidateAccountID(accountID); err != nil {
		return fmt.Errorf("invalid account ID: %w", err)
	}
	prefix := storage.AccountPrefix(accountID)
	bucket := s.client.Bucket(s.bucket)
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(deleteWorkers)
	it := bucket.Objects(ctx, &gcsstorage.Query{Prefix: prefix})
	var deleted atomic.Int64
	listed := 0
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			// A failed delete has already cancelled ctx; report that rather
			// than the listing it interrupted.
			if waitErr := group.Wait(); waitErr != nil {
				return waitErr
			}
			return fmt.Errorf("list GCS objects under %s: %w", prefix, err)
		}
		listed++
		name := attrs.Name
		group.Go(func() error {
			if err := bucket.Object(name).Delete(ctx); err != nil && !errors.Is(err, gcsstorage.ErrObjectNotExist) {
				return fmt.Errorf("delete GCS object %s: %w", name, err)
			}
			deleted.Add(1)
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	s.logger.Info(ctx, "account folder removed from GCS",
		"bucket", s.bucket, "prefix", prefix, "objects", deleted.Load(), "listed", listed)
	return nil
}
