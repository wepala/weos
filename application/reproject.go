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
	"encoding/json"
	"fmt"

	"github.com/wepala/weos/v3/domain/entities"
	gormprov "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/infrastructure/events"
	"github.com/wepala/weos/v3/infrastructure/logging"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// This file is the replay rail for the SYNCHRONOUS projections (issue #442).
//
// The critical read models — resource types, canonical resources, triples,
// and the per-type projection tables — are written inline at commit time
// (subscribeEventHandlers) so the SPA gets read-your-writes. That dispatcher
// only ever sees events the moment THIS process commits them, which means an
// event store that arrived by other means (pericarp export → import, a
// restored backup) has a full history and empty projections, and nothing to
// rebuild them: `worker checkpoint reset` covers only the checkpointed
// background groups. Reproject closes that gap by streaming the global feed
// through the very same handler set the write path uses.
//
// Deliberately NOT built on application.Module: constructing the full module
// runs ensureBuiltInResourceTypes at start, which decides "does this type
// exist" from the projection tables — empty on exactly the store reproject
// exists to fix — and would append brand-new ResourceType.Created events
// (fresh IDs, duplicate slugs) into the imported history before replay runs
// (issue #443). ReprojectModule assembles only what replay needs.
//
// Also deliberately NOT a SubscriberGroup: Manager.Start launches every
// registered group as a live loop, and these projections are already written
// synchronously on every commit — a live duplicate would double-write, and
// its GetEventsByTransactionID reads could observe a transaction whose
// per-aggregate appends haven't all landed yet. Replay must stay a one-shot,
// operator-invoked pass.

// ReprojectRuntime bundles what Reproject needs; populate it from an fx app
// built with ReprojectModule.
type ReprojectRuntime struct {
	fx.In
	EventStore domain.EventStore
	Dispatcher *domain.EventDispatcher
	Logger     entities.Logger
}

// ReprojectModule is the narrow assembly behind `worker reproject`: config,
// logging, the gorm DB, the event store (undecorated — a one-shot replay has
// no background subscribers to wake), the four projection dependencies, and a
// fresh dispatcher wired with ONLY the synchronous projection handlers.
func ReprojectModule(cfg config.Config) fx.Option {
	return fx.Module("reproject",
		fx.Provide(func() config.Config { return cfg }),
		fx.Provide(logging.ProvideZapLogger),
		fx.Provide(logging.ProvideLogger),
		fx.Provide(events.ProvideEventDispatcher),
		fx.Provide(gormprov.ProvideGormDB),
		fx.Provide(gormprov.ProvideEventStore),
		fx.Provide(gormprov.ProvideResourceTypeRepository),
		fx.Provide(gormprov.ProvideProjectionManager),
		fx.Provide(gormprov.ProvideResourceRepository),
		fx.Provide(gormprov.ProvideTripleRepository),
		fx.Invoke(subscribeEventHandlers),
	)
}

// ReprojectOptions tune a replay run.
type ReprojectOptions struct {
	// AfterPosition resumes a run: only events with a global Position
	// strictly greater are replayed. 0 replays everything. The handlers are
	// idempotent (upserts / IF NOT EXISTS), so re-running from 0 after an
	// interruption is safe — merely wasteful.
	AfterPosition int64
	// BatchSize is events per ReadAfter call; <= 0 uses 500.
	BatchSize int
}

// ReprojectResult reports what a replay run did.
type ReprojectResult struct {
	Dispatched   int   // events replayed through the projection handlers
	Skipped      int   // events with no synchronous projection handler
	LastPosition int64 // last successfully processed global position
	Head         int64 // feed head captured at the start of the run
}

// Reproject streams the event store's global feed, in position order, through
// the synchronous projection handlers. The head is captured once up front so
// the run terminates even against a concurrently-writing server (writes after
// the snapshot already projected inline via the live path) — though the
// intended use is the runbook's: server stopped, freshly imported store.
func Reproject(ctx context.Context, rt ReprojectRuntime, opts ReprojectOptions) (ReprojectResult, error) {
	batch := opts.BatchSize
	if batch <= 0 {
		batch = 500
	}
	res := ReprojectResult{LastPosition: opts.AfterPosition}

	head, err := rt.EventStore.HeadPosition(ctx)
	if err != nil {
		return res, fmt.Errorf("reproject: head position: %w", err)
	}
	res.Head = head

	pos := opts.AfterPosition
	for pos < head {
		envs, err := rt.EventStore.ReadAfter(ctx, pos, batch)
		if err != nil {
			return res, fmt.Errorf("reproject: read after position %d: %w", pos, err)
		}
		if len(envs) == 0 {
			break
		}
		for i := range envs {
			env := envs[i]
			if env.Position > head {
				// Written after our snapshot — the live path projected it.
				pos = head
				break
			}
			pos = env.Position
			typed, handled, convErr := typedForReplay(env)
			if convErr != nil {
				return res, fmt.Errorf(
					"reproject: stopped at position %d (%s on %s): %w — fix the cause, then resume with --after-position %d",
					env.Position, env.EventType, env.AggregateID, convErr, res.LastPosition)
			}
			if !handled {
				res.Skipped++
				res.LastPosition = env.Position
				continue
			}
			if err := rt.Dispatcher.Dispatch(ctx, typed); err != nil {
				return res, fmt.Errorf(
					"reproject: stopped at position %d (%s on %s): %w — fix the cause, then resume with --after-position %d",
					env.Position, env.EventType, env.AggregateID, err, res.LastPosition)
			}
			res.Dispatched++
			res.LastPosition = env.Position
		}
		rt.Logger.Info(ctx, "reproject: progress",
			"position", pos, "head", head, "dispatched", res.Dispatched, "skipped", res.Skipped)
	}
	return res, nil
}

// typedForReplay converts a store-loaded envelope (payload deserialized as
// map[string]any) into the typed envelope the synchronous handlers expect.
// Subscribe[T]'s wrapper does a plain env.Payload.(T) assertion — it performs
// no conversion — so replay must hand it the concrete VALUE type. Events with
// no synchronous handler return handled=false and are skipped (the
// checkpointed groups own them, with their own replay rail).
func typedForReplay(env domain.EventEnvelope[any]) (domain.EventEnvelope[any], bool, error) {
	switch env.EventType {
	case "ResourceType.Created":
		return retype[entities.ResourceTypeCreated](env)
	case "ResourceType.Updated":
		return retype[entities.ResourceTypeUpdated](env)
	case "ResourceType.Deleted":
		return retype[entities.ResourceTypeDeleted](env)
	case "Resource.Published":
		return retype[entities.ResourcePublished](env)
	case "Resource.Deleted":
		return retype[entities.ResourceDeleted](env)
	case "Triple.Created":
		return retype[entities.TripleCreated](env)
	case "Triple.Deleted":
		return retype[entities.TripleDeleted](env)
	default:
		return env, false, nil
	}
}

// retype JSON-round-trips the deserialized payload into T. Untagged event
// structs (PascalCase map keys) and tagged ones (BasicTripleEvent's lowercase
// keys) both survive: encoding/json matches field names case-insensitively
// and tags exactly.
func retype[T any](env domain.EventEnvelope[any]) (domain.EventEnvelope[any], bool, error) {
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		return env, false, fmt.Errorf("re-marshal payload: %w", err)
	}
	var payload T
	if err := json.Unmarshal(raw, &payload); err != nil {
		return env, false, fmt.Errorf("decode payload as %T: %w", payload, err)
	}
	env.Payload = payload
	return env, true, nil
}
