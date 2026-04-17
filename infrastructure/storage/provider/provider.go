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

	"github.com/wepala/weos/domain/entities"
	"github.com/wepala/weos/domain/services"
	"github.com/wepala/weos/infrastructure/storage"
	"github.com/wepala/weos/infrastructure/storage/gcs"
	"github.com/wepala/weos/infrastructure/storage/local"
	s3backend "github.com/wepala/weos/infrastructure/storage/s3"
	"github.com/wepala/weos/internal/config"

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
	logger := params.Logger

	// Eagerly create the local upload directory at startup so missing-path
	// errors surface immediately rather than on the first upload request.
	if err := os.MkdirAll(cfg.LocalPath, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create local storage path %q: %w", cfg.LocalPath, err)
	}

	localSvc := local.New(cfg.LocalPath, "/api/uploads/files", logger)

	// Warn if both cloud backends are configured.
	if cfg.GCSBucket != "" && cfg.S3Bucket != "" {
		logger.Warn(context.Background(),
			"both GCS and S3 buckets configured; using GCS as primary",
			"gcsBucket", cfg.GCSBucket, "s3Bucket", cfg.S3Bucket)
	}

	switch {
	case cfg.GCSBucket != "":
		return buildGCSComposite(params.Lifecycle, cfg, logger, localSvc)
	case cfg.S3Bucket != "":
		return buildS3Composite(cfg, logger, localSvc)
	default:
		logger.Info(context.Background(), "using local-only file storage",
			"path", cfg.LocalPath)
		return localSvc, nil
	}
}

func buildGCSComposite(
	lc fx.Lifecycle, cfg config.StorageConfig,
	logger entities.Logger, localSvc services.FileService,
) (services.FileService, error) {
	ctx := context.Background()
	client, err := gcsstorage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCS client: %w", err)
	}
	lc.Append(fx.Hook{
		OnStop: func(_ context.Context) error { return client.Close() },
	})
	gcsSvc := gcs.New(client, cfg.GCSBucket, logger)
	logger.Info(ctx, "GCS file storage enabled", "bucket", cfg.GCSBucket)
	return storage.NewComposite(gcsSvc, []services.FileService{localSvc}, logger), nil
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
