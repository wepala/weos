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

package application

import (
	"context"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/features"
	"github.com/wepala/weos/v3/internal/config"
)

// ProvideFeatureCacheInvalidator selects how invalidations travel, following
// the same shape as the knowledge-graph store's provider: choose at boot from
// configuration, and degrade with one logged error rather than failing the
// boot.
//
// On SQLite the resolver's own in-process eviction is returned directly. That
// is not a fallback or a reduced mode — SQLite is single-process with
// serialized writers, so there is no second cache in existence to reach, and
// local eviction is complete and exact.
//
// On Postgres the resolver is wrapped with a LISTEN/NOTIFY broadcast, because
// Postgres is the only deployment that can have replicas. If the listener
// cannot be established the instance says so once and keeps the in-process
// invalidator: still correct locally, and other replicas converge when their
// cached sets reach the maximum age. It never degrades to doing nothing, which
// would break the rule that a change reaches sessions already open.
func ProvideFeatureCacheInvalidator(
	cfg config.Config,
	resolver *FeatureResolver,
	db *gorm.DB,
	logger entities.Logger,
	lc fx.Lifecycle,
) repositories.FeatureCacheInvalidator {
	if !cfg.IsPostgres() {
		return resolver
	}

	notifier := features.NewNotifyInvalidator(resolver, db, cfg.Features.NotifyChannel, logger)

	listenCtx, stopListening := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := notifier.Listen(listenCtx, cfg.DatabaseDSN); err != nil && listenCtx.Err() == nil {
					logger.Error(listenCtx,
						"the feature-cache listener stopped; this replica will converge only "+
							"when its cached sets reach the maximum age",
						"error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			stopListening()
			return nil
		},
	})

	return notifier
}
