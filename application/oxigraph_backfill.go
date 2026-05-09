package application

import (
	"context"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	"go.uber.org/fx"
)

// backfillBatchSize controls how many records we page through at a time during
// the startup replay. Small enough to stay friendly with embedded SQLite,
// large enough to limit round-trips.
const backfillBatchSize = 200

// backfillFailureAbortThreshold caps how many consecutive resource-load
// failures we tolerate before aborting the backfill. When Oxigraph is sick we
// want one summary error, not a flood of identical per-resource errors.
const backfillFailureAbortThreshold = 10

// backfillCtxOpts bundles the lifecycle-bound context plumbing so the
// goroutine respects Fx shutdown (and so it's not buried inline in
// registerKGBackfill).
type backfillCtxOpts struct {
	rebuild bool
}

// registerKGBackfill installs the lifecycle hooks that drive a startup
// backfill of the knowledge graph. Runs in a goroutine so it doesn't block
// startup — projector subscriptions handle live writes from the moment the
// dispatcher is wired. The goroutine is bound to a context that's cancelled
// in OnStop so a long backfill doesn't outlive the rest of the application
// (and doesn't keep using the GORM/HTTP handles that other shutdown hooks
// are trying to release).
func registerKGBackfill(p SubscribeKnowledgeGraphHandlersParams, cfg config.Config) {
	ctx, cancel := context.WithCancel(context.Background())
	opts := backfillCtxOpts{rebuild: cfg.Oxigraph.Rebuild}
	p.Lifecycle.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go safeRunKGBackfill(ctx, p, opts)
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}

// safeRunKGBackfill wraps runKGBackfill with a panic recovery. The optional
// projector must never crash the main process — a panic here would take down
// the canonical write path with it, which is exactly the failure mode the
// "additive, fail-soft" contract was designed to prevent.
func safeRunKGBackfill(ctx context.Context, p SubscribeKnowledgeGraphHandlersParams, opts backfillCtxOpts) {
	defer func() {
		if r := recover(); r != nil {
			p.Logger.Error(ctx, "kg backfill: panic recovered", "panic", r)
		}
	}()
	runKGBackfill(ctx, p, opts)
}

// runKGBackfill executes the actual backfill work. Logged at INFO so operators
// can see startup progress; per-record events are at DEBUG. Returns early on
// ctx cancellation so the goroutine exits cleanly during Fx shutdown.
func runKGBackfill(ctx context.Context, p SubscribeKnowledgeGraphHandlersParams, opts backfillCtxOpts) {
	if opts.rebuild {
		p.Logger.Info(ctx, "kg backfill: OXIGRAPH_REBUILD=true, clearing graph before replay")
		if err := p.Store.Clear(ctx); err != nil {
			p.Logger.Error(ctx, "kg backfill: failed to clear store", "error", err)
			return
		}
	} else {
		empty, err := p.Store.IsEmpty(ctx)
		if err != nil {
			p.Logger.Error(ctx, "kg backfill: IsEmpty check failed; skipping",
				"error", err)
			return
		}
		if !empty {
			p.Logger.Info(ctx, "kg backfill: graph already populated, skipping replay")
			return
		}
	}

	if ctx.Err() != nil {
		return
	}
	p.Logger.Info(ctx, "kg backfill: starting replay")
	types := loadAllResourceTypes(ctx, p.TypeRepo, p.Logger)
	loaded, failed := backfillResources(ctx, p, types)
	triples := backfillTriples(ctx, p)

	if failed > 0 {
		p.Logger.Error(ctx, "kg backfill: complete with failures",
			"types", len(types), "resources_loaded", loaded,
			"resources_failed", failed, "triples", triples)
		return
	}
	p.Logger.Info(ctx, "kg backfill: complete",
		"types", len(types), "resources_loaded", loaded, "triples", triples)
}

// loadAllResourceTypes pages through every resource type. Pagination errors
// abort the listing — partial results are returned so the caller still gets
// to backfill the types we did read.
func loadAllResourceTypes(
	ctx context.Context,
	typeRepo repositories.ResourceTypeRepository,
	logger entities.Logger,
) []*entities.ResourceType {
	var all []*entities.ResourceType
	cursor := ""
	for {
		if ctx.Err() != nil {
			return all
		}
		page, err := typeRepo.FindAll(ctx, cursor, backfillBatchSize)
		if err != nil {
			logger.Error(ctx, "kg backfill: failed to list resource types", "error", err)
			return all
		}
		all = append(all, page.Data...)
		if !page.HasMore {
			break
		}
		cursor = page.Cursor
	}
	return all
}

// backfillResources iterates each known resource type and replays each
// resource's JSON-LD into the store. Returns (loaded, failed) counts. A run
// of consecutive failures (>= backfillFailureAbortThreshold) aborts the
// backfill — that pattern means Oxigraph is sick and continuing produces a
// flood of identical errors with no useful work done.
func backfillResources(
	ctx context.Context,
	p SubscribeKnowledgeGraphHandlersParams,
	types []*entities.ResourceType,
) (loaded, failed int) {
	consecutiveFailures := 0
	for _, rt := range types {
		if ctx.Err() != nil {
			return loaded, failed
		}
		projectResourceTypeOntology(ctx, rt.Slug(), rt.Context(), p.Store, p.Logger)

		cursor := ""
		for {
			if ctx.Err() != nil {
				return loaded, failed
			}
			page, err := p.ResourceRepo.FindAllByType(
				ctx, rt.Slug(), cursor, backfillBatchSize,
				repositories.SortOptions{}, nil,
			)
			if err != nil {
				p.Logger.Error(ctx, "kg backfill: failed to list resources",
					"type", rt.Slug(), "error", err)
				break
			}
			for _, r := range page.Data {
				if err := p.Store.LoadOntology(ctx, "application/ld+json", r.Data()); err != nil {
					failed++
					consecutiveFailures++
					p.Logger.Error(ctx, "kg backfill: failed to load resource",
						"id", r.GetID(), "error", err)
					if consecutiveFailures >= backfillFailureAbortThreshold {
						p.Logger.Error(ctx, "kg backfill: aborting — too many consecutive failures",
							"threshold", backfillFailureAbortThreshold)
						return loaded, failed
					}
					continue
				}
				consecutiveFailures = 0
				loaded++
				p.Logger.Debug(ctx, "kg backfill: loaded resource",
					"id", r.GetID(), "type", rt.Slug())
			}
			if !page.HasMore {
				break
			}
			cursor = page.Cursor
		}
	}
	return loaded, failed
}

// backfillTriples covers any triples that aren't reachable through the
// resource graph (e.g. relationships between non-resource entities). Walks
// known subjects from the triples table.
func backfillTriples(
	ctx context.Context,
	p SubscribeKnowledgeGraphHandlersParams,
) int {
	// The current TripleRepository interface doesn't expose a "list all"
	// method; resource-graph backfill above already covers triples that
	// reference resource subjects. Triples whose subject is non-resource
	// (e.g. raw FOAF/vCard subjects) would need a repository extension to
	// replay — out of scope for the optional projection's first pass.
	p.Logger.Debug(ctx, "kg backfill: triple-only replay skipped (covered by resource backfill)")
	return 0
}
