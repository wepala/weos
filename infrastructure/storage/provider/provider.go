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

package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/infrastructure/storage"
	"github.com/wepala/weos/v3/infrastructure/storage/gcs"
	"github.com/wepala/weos/v3/infrastructure/storage/local"
	s3backend "github.com/wepala/weos/v3/infrastructure/storage/s3"
	"github.com/wepala/weos/v3/internal/config"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"

	gcsstorage "cloud.google.com/go/storage"
	"go.uber.org/fx"
)

// ProvideFileService builds a FileService from the application config.
// Local storage is always available. When a cloud bucket is configured,
// it becomes the primary backend and local acts as a secondary replica.
// The local upload directory is eagerly created at startup via MkdirAll.
func ProvideFileService(params struct {
	fx.In
	Lifecycle fx.Lifecycle
	Config    config.Config
	Logger    entities.Logger
}) (services.FileService, error) {
	cfg := params.Config.Storage

	// Eagerly create the local upload directory at startup so missing-path
	// errors surface immediately rather than on the first upload request.
	if err := os.MkdirAll(cfg.LocalPath, 0o750); err != nil {
		return nil, fmt.Errorf("cannot create local storage path %q: %w", cfg.LocalPath, err)
	}
	svc, closeFn, err := NewFileService(cfg, params.Logger)
	if err != nil {
		return nil, err
	}
	params.Lifecycle.Append(fx.Hook{
		OnStop: func(_ context.Context) error { return closeFn() },
	})
	return svc, nil
}

// NewFileService builds the FileService cfg names, as ProvideFileService
// does, without creating the local upload directory: a command that only
// reads the stores, such as the preview of `weos maintenance adopt-account`,
// writes nothing. The returned function releases the cloud client, if one
// was opened.
func NewFileService(
	cfg config.StorageConfig, logger entities.Logger,
) (services.FileService, func() error, error) {
	noClose := func() error { return nil }
	localSvc := local.New(cfg.LocalPath, "/api/uploads/files", logger)

	// Warn if both cloud backends are configured.
	if cfg.GCSBucket != "" && cfg.S3Bucket != "" {
		logger.Warn(context.Background(),
			"both GCS and S3 buckets configured; using GCS as primary",
			"gcsBucket", cfg.GCSBucket, "s3Bucket", cfg.S3Bucket)
	}

	switch {
	case cfg.GCSBucket != "":
		return buildGCSComposite(cfg, logger, localSvc)
	case cfg.S3Bucket != "":
		svc, err := buildS3Composite(cfg, logger, localSvc)
		return svc, noClose, err
	default:
		logger.Info(context.Background(), "using local-only file storage",
			"path", cfg.LocalPath)
		return localSvc, noClose, nil
	}
}

func buildGCSComposite(
	cfg config.StorageConfig, logger entities.Logger, localSvc services.FileService,
) (services.FileService, func() error, error) {
	ctx := context.Background()
	client, err := gcsstorage.NewClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create GCS client: %w", err)
	}
	gcsSvc := gcs.New(client, cfg.GCSBucket, logger)
	logger.Info(ctx, "GCS file storage enabled", "bucket", cfg.GCSBucket)
	return storage.NewComposite(gcsSvc, []services.FileService{localSvc}, logger), client.Close, nil
}

func buildS3Composite(
	cfg config.StorageConfig, logger entities.Logger, localSvc services.FileService,
) (services.FileService, error) {
	ctx := context.Background()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.S3Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	s3Client := s3sdk.NewFromConfig(awsCfg)
	s3Svc := s3backend.New(s3Client, cfg.S3Bucket, cfg.S3Region, logger)
	logger.Info(ctx, "S3 file storage enabled",
		"bucket", cfg.S3Bucket, "region", cfg.S3Region)
	return storage.NewComposite(s3Svc, []services.FileService{localSvc}, logger), nil
}
