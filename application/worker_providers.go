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
	"fmt"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// subscriberGroupTag is the Fx value-group name through which providers
// contribute SubscriberGroup definitions. The Manager collects the whole group.
const subscriberGroupTag = `group:"subscriber_groups"`

// WorkerModule provides the background subscriber runtime: the checkpoint and
// parking stores, the in-process wake notifier, the Manager, and the lifecycle
// hook that runs/drains the workers (gated by WorkerConfig.RunInProcess).
//
// It is part of the application Module so every entry point (serve, CLI, tests)
// can construct the Manager — but only processes that set RunInProcess actually
// start background processing.
func WorkerModule() fx.Option {
	return fx.Options(
		fx.Provide(ProvideInProcessNotifier),
		fx.Provide(ProvideCheckpointStore),
		fx.Provide(ProvideEnsureCheckpoint),
		fx.Provide(ProvideParkingLot),
		fx.Provide(ProvideWorkerManager),
		fx.Invoke(runWorkerManager),
	)
}

// ProvideInProcessNotifier supplies the singleton in-process commit notifier.
// On SQLite it is wired to the event store (see DecorateNotifyingEventStore) so
// background subscribers wake on commit; on Postgres it is unused (the database
// NOTIFYs on commit itself) but harmless to provide.
func ProvideInProcessNotifier() *subscriptions.InProcessNotifier {
	return subscriptions.NewInProcessNotifier()
}

// ProvideCheckpointStore supplies the GORM checkpoint store, auto-migrating the
// subscriber_checkpoints table.
func ProvideCheckpointStore(db *gorm.DB) (subscriptions.CheckpointStore, error) {
	store, err := subscriptions.NewGormCheckpointStore(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create checkpoint store: %w", err)
	}
	return store, nil
}

// ProvideEnsureCheckpoint supplies the create-if-missing checkpoint primitive
// behind SubscriberGroup.StartAtHead. It writes the same subscriber_checkpoints
// table the GORM checkpoint store owns (pericarp exports the model), with an
// insert that does nothing when the row exists — so it can never move a
// checkpoint, only seed a brand-new one.
func ProvideEnsureCheckpoint(db *gorm.DB) EnsureCheckpointFunc {
	return func(ctx context.Context, subscriber string, position int64) error {
		err := db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "subscriber"}},
			DoNothing: true,
		}).Create(&subscriptions.GormCheckpointModel{
			Subscriber: subscriber,
			Position:   position,
			UpdatedAt:  time.Now(),
		}).Error
		if err != nil {
			return fmt.Errorf("ensure checkpoint for %q at %d: %w", subscriber, position, err)
		}
		return nil
	}
}

// ProvideParkingLot supplies the GORM parking lot, auto-migrating the
// parked_events table. Poison events that exhaust their retries land here so
// the events behind them keep flowing; operators list and replay them via the
// worker CLI.
func ProvideParkingLot(db *gorm.DB) (subscriptions.ParkingLot, error) {
	lot, err := subscriptions.NewGormParkingLot(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create parking lot: %w", err)
	}
	return lot, nil
}

// workerManagerParams collects the Manager's dependencies, including every
// SubscriberGroup contributed via the value group.
type workerManagerParams struct {
	fx.In
	EventStore  domain.EventStore
	Checkpoints subscriptions.CheckpointStore
	Ensure      EnsureCheckpointFunc
	Parking     subscriptions.ParkingLot
	Notifier    *subscriptions.InProcessNotifier
	Config      config.Config
	Logger      entities.Logger
	Groups      []SubscriberGroup `group:"subscriber_groups"`
}

// ProvideWorkerManager constructs the subscriber Manager from the registered
// groups. It does not start processing — runWorkerManager wires that into the
// Fx lifecycle.
func ProvideWorkerManager(p workerManagerParams) (*Manager, error) {
	var rebuild []string
	// OXIGRAPH_REBUILD is now a checkpoint reset: the serving process resets the
	// oxigraph checkpoint at startup so its catch-up replays history into a
	// freshly-cleared graph — the same replay mechanism as recovery and the CLI.
	if p.Config.Oxigraph.Active() && p.Config.Oxigraph.Rebuild {
		rebuild = append(rebuild, "oxigraph")
	}
	return NewManager(ManagerParams{
		EventStore:       p.EventStore,
		Checkpoints:      p.Checkpoints,
		Parking:          p.Parking,
		Notifier:         p.Notifier,
		Config:           p.Config,
		Logger:           p.Logger,
		Groups:           p.Groups,
		RebuildOnStart:   rebuild,
		EnsureCheckpoint: p.Ensure,
	})
}

// AsSubscriberGroup tags a SubscriberGroup constructor so its result joins the
// "subscriber_groups" value group the Manager collects. Use it to register a
// peripheral handler group:
//
//	fx.Provide(application.AsSubscriberGroup(provideOxigraphGroup))
func AsSubscriberGroup(constructor any) any {
	return fx.Annotate(constructor, fx.ResultTags(subscriberGroupTag))
}

// AsSubscriberGroups tags a constructor returning []SubscriberGroup so its
// elements are flattened into the "subscriber_groups" value group. Use it for
// providers that contribute zero or more groups depending on configuration
// (e.g. an Oxigraph group only when the store is active).
func AsSubscriberGroups(constructor any) any {
	return fx.Annotate(constructor, fx.ResultTags(`group:"subscriber_groups,flatten"`))
}

// runWorkerManager wires the Manager into the Fx lifecycle. Background
// processing only starts when WorkerConfig.RunInProcess is set (the serve
// command enables it); otherwise the Manager is still constructed — so CLI
// commands can inspect and reset checkpoints — but no workers run.
func runWorkerManager(lc fx.Lifecycle, m *Manager, cfg config.Config, logger entities.Logger) {
	if !cfg.Worker.RunInProcess {
		logger.Debug(context.Background(),
			"worker: RunInProcess is false, background subscribers will not start in this process")
		return
	}
	lc.Append(fx.Hook{
		OnStart: m.Start,
		OnStop:  m.Stop,
	})
}

// DecorateNotifyingEventStore wraps the event store so each successful Append
// wakes background subscribers immediately, on SQLite / single-process
// deployments. On Postgres the store NOTIFYs on commit itself, so the store is
// returned unwrapped and PostgresListener handles cross-process wake-ups.
func DecorateNotifyingEventStore(
	primary domain.EventStore,
	notifier *subscriptions.InProcessNotifier,
	cfg config.Config,
) domain.EventStore {
	if cfg.IsPostgres() {
		return primary
	}
	return subscriptions.NewNotifyingEventStore(primary, notifier.Notify)
}
