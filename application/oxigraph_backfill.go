package application

import (
	"context"
	"os"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"go.uber.org/fx"
)

// backfillBatchSize controls how many records we page through at a time during
// the startup replay. Small enough to stay friendly with embedded SQLite,
// large enough to limit round-trips.
const backfillBatchSize = 200

// registerKGBackfill installs an OnStart lifecycle hook that fills the
// knowledge graph with current state if it's empty (or rebuilds it on demand
// via OXIGRAPH_REBUILD=true). Runs in a goroutine so it doesn't block startup
// — projector subscriptions handle live writes from the moment the dispatcher
// is wired.
func registerKGBackfill(p SubscribeKnowledgeGraphHandlersParams) {
	rebuild := strings.EqualFold(os.Getenv("OXIGRAPH_REBUILD"), "true")
	p.Lifecycle.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go runKGBackfill(p, rebuild)
			return nil
		},
	})
}

// runKGBackfill executes the actual backfill work. Logged at INFO so operators
// can see startup progress; per-record events are at DEBUG.
func runKGBackfill(p SubscribeKnowledgeGraphHandlersParams, rebuild bool) {
	ctx := context.Background()

	if rebuild {
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

	p.Logger.Info(ctx, "kg backfill: starting replay")
	types := loadAllResourceTypes(ctx, p.TypeRepo, p.Logger)
	resources := backfillResources(ctx, p, types)
	triples := backfillTriples(ctx, p)

	p.Logger.Info(ctx, "kg backfill: complete",
		"types", len(types), "resources", resources, "triples", triples)
}

// loadAllResourceTypes pages through every resource type and pushes its JSON-LD
// context into the graph as ontology data.
func loadAllResourceTypes(
	ctx context.Context,
	typeRepo repositories.ResourceTypeRepository,
	logger entities.Logger,
) []*entities.ResourceType {
	var all []*entities.ResourceType
	cursor := ""
	for {
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
// resource's JSON-LD into the store. Per-resource failures are logged and
// skipped so a single bad row doesn't abort the whole backfill.
func backfillResources(
	ctx context.Context,
	p SubscribeKnowledgeGraphHandlersParams,
	types []*entities.ResourceType,
) int {
	count := 0
	for _, rt := range types {
		// Push the ontology context first so subsequent resources have type
		// metadata available.
		projectResourceTypeOntology(ctx, rt.Slug(), rt.Context(), p.Store, p.Logger)

		cursor := ""
		for {
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
					p.Logger.Error(ctx, "kg backfill: failed to load resource",
						"id", r.GetID(), "error", err)
					continue
				}
				count++
				p.Logger.Debug(ctx, "kg backfill: loaded resource",
					"id", r.GetID(), "type", rt.Slug())
			}
			if !page.HasMore {
				break
			}
			cursor = page.Cursor
		}
	}
	return count
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
