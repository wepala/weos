package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/pkg/jsonld"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// SubscribeKnowledgeGraphHandlersParams bundles the dependencies of the
// optional Oxigraph projector. Lifecycle is injected so the projector can also
// drive the startup backfill (see oxigraph_backfill.go).
type SubscribeKnowledgeGraphHandlersParams struct {
	fx.In

	Lifecycle    fx.Lifecycle
	Dispatcher   *domain.EventDispatcher
	EventStore   domain.EventStore
	Store        repositories.KnowledgeGraphStore
	ResourceRepo repositories.ResourceRepository
	TypeRepo     repositories.ResourceTypeRepository
	TripleRepo   repositories.TripleRepository
	Config       config.Config
	Logger       entities.Logger
}

// SubscribeKnowledgeGraphHandlers wires the Oxigraph projector to the same
// events the existing `triples`/projection handlers consume. When the store is
// not active (Oxigraph not configured), this is a no-op so the application
// boots normally without the optional dependency.
//
// Failures inside individual handlers log at ERROR but do not propagate to
// the dispatcher — Oxigraph is an additive read-model and must never block
// the canonical write path.
func SubscribeKnowledgeGraphHandlers(p SubscribeKnowledgeGraphHandlersParams) error {
	if !p.Store.Active() {
		p.Logger.Debug(context.Background(),
			"knowledge graph: store inactive, skipping projector subscription")
		return nil
	}
	p.Logger.Info(context.Background(),
		"knowledge graph: subscribing Oxigraph projector to resource/triple/type events")

	if err := domain.Subscribe(p.Dispatcher, "Triple.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.TripleCreated]) error {
			t := env.Payload
			p.Logger.Debug(ctx, "kg projecting Triple.Created",
				"subject", t.Subject, "predicate", t.Predicate, "object", t.Object)
			if err := p.Store.AddTriples(ctx, []repositories.Triple{
				{Subject: t.Subject, Predicate: t.Predicate, Object: t.Object},
			}); err != nil {
				p.Logger.Error(ctx, "kg failed to project Triple.Created",
					"subject", t.Subject, "predicate", t.Predicate, "error", err)
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("kg triple created handler: %w", err)
	}

	if err := domain.Subscribe(p.Dispatcher, "Triple.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.TripleDeleted]) error {
			t := env.Payload
			p.Logger.Debug(ctx, "kg projecting Triple.Deleted",
				"subject", t.Subject, "predicate", t.Predicate, "object", t.Object)
			if err := p.Store.RemoveTriples(ctx, []repositories.Triple{
				{Subject: t.Subject, Predicate: t.Predicate, Object: t.Object},
			}); err != nil {
				p.Logger.Error(ctx, "kg failed to project Triple.Deleted",
					"subject", t.Subject, "predicate", t.Predicate, "error", err)
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("kg triple deleted handler: %w", err)
	}

	if err := domain.Subscribe(p.Dispatcher, "Resource.Published",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourcePublished]) error {
			projectResourcePublished(ctx, env, p.EventStore, p.Store, p.Logger)
			return nil
		},
	); err != nil {
		return fmt.Errorf("kg resource published handler: %w", err)
	}

	if err := domain.Subscribe(p.Dispatcher, "Resource.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceDeleted]) error {
			p.Logger.Debug(ctx, "kg projecting Resource.Deleted", "id", env.AggregateID)
			if err := p.Store.RemoveSubject(ctx, env.AggregateID); err != nil {
				p.Logger.Error(ctx, "kg failed to project Resource.Deleted",
					"id", env.AggregateID, "error", err)
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("kg resource deleted handler: %w", err)
	}

	if err := domain.Subscribe(p.Dispatcher, "ResourceType.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceTypeCreated]) error {
			projectResourceTypeOntology(ctx, env.Payload.Slug, env.Payload.Context, p.Store, p.Logger)
			return nil
		},
	); err != nil {
		return fmt.Errorf("kg resource type created handler: %w", err)
	}

	if err := domain.Subscribe(p.Dispatcher, "ResourceType.Updated",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceTypeUpdated]) error {
			projectResourceTypeOntology(ctx, env.Payload.Slug, env.Payload.Context, p.Store, p.Logger)
			return nil
		},
	); err != nil {
		return fmt.Errorf("kg resource type updated handler: %w", err)
	}

	registerKGBackfill(p, p.Config)
	return nil
}

// projectResourcePublished replays the transaction events for a Resource.Published
// envelope, builds the full JSON-LD state, and pushes it into the knowledge
// graph. We reuse buildStateFromTransaction so the projection sees the same
// data the GORM projection writes — including all edges.
func projectResourcePublished(
	ctx context.Context,
	env domain.EventEnvelope[entities.ResourcePublished],
	eventStore domain.EventStore,
	store repositories.KnowledgeGraphStore,
	logger entities.Logger,
) {
	if env.TransactionID == "" {
		logger.Error(ctx, "kg Resource.Published has empty transaction id",
			"id", env.AggregateID)
		return
	}
	txEvents, err := eventStore.GetEventsByTransactionID(ctx, env.TransactionID)
	if err != nil {
		logger.Error(ctx, "kg failed to load transaction for Resource.Published",
			"id", env.AggregateID, "error", err)
		return
	}
	state := buildStateFromTransaction(ctx, txEvents, env.AggregateID, env.SequenceNo, logger)

	if state.IsDelete {
		logger.Debug(ctx, "kg skipping Resource.Published (delete)", "id", env.AggregateID)
		return
	}
	if state.Data == nil {
		logger.Error(ctx, "kg Resource.Published produced no data", "id", env.AggregateID)
		return
	}

	// Drop any prior projection for this subject so an update fully replaces
	// the previous graph (covers properties that disappeared from the JSON-LD).
	if err := store.RemoveSubject(ctx, env.AggregateID); err != nil {
		logger.Error(ctx, "kg failed to clear prior subject", "id", env.AggregateID, "error", err)
		// Continue: LoadOntology below merges into the default graph, and
		// RDF triples are a set, so a leftover prior triple at worst lingers
		// until the next update — preferable to a partially-projected state.
	}

	if err := store.LoadOntology(ctx, "application/ld+json", state.Data); err != nil {
		logger.Error(ctx, "kg failed to load resource JSON-LD",
			"id", env.AggregateID, "error", err)
		return
	}
	logger.Debug(ctx, "kg projected Resource.Published",
		"id", env.AggregateID, "transactionID", env.TransactionID)
}

// projectResourceTypeOntology loads the JSON-LD `@context` of a resource type
// into the knowledge graph so SPARQL queries can resolve the predicates and
// classes that resources reference. Also emits explicit ontology triples
// (rdf:type, rdfs:subClassOf) so kg_describe_class can walk the subclass
// chain even on engines that don't auto-extract them from the JSON-LD
// document.
func projectResourceTypeOntology(
	ctx context.Context,
	slug string,
	rawContext json.RawMessage,
	store repositories.KnowledgeGraphStore,
	logger entities.Logger,
) {
	if len(rawContext) == 0 {
		return
	}
	logger.Debug(ctx, "kg projecting ResourceType ontology", "slug", slug)
	if err := store.LoadOntology(ctx, "application/ld+json", rawContext); err != nil {
		logger.Error(ctx, "kg failed to load resource type ontology",
			"slug", slug, "error", err)
	}
	emitExplicitOntologyTriples(ctx, slug, rawContext, store, logger)
}

// emitExplicitOntologyTriples adds the canonical `rdf:type rdfs:Class` and
// `rdfs:subClassOf <parent>` triples for a resource type, plus a
// `rdfs:label` from the slug. Belt-and-suspenders to the JSON-LD load:
// some serializers don't fully expand context-only declarations into
// triples, and the description tools rely on these being concrete RDF.
func emitExplicitOntologyTriples(
	ctx context.Context,
	slug string,
	rawContext json.RawMessage,
	store repositories.KnowledgeGraphStore,
	logger entities.Logger,
) {
	classIRI := "urn:type:" + slug
	triples := []repositories.Triple{
		{
			Subject:   classIRI,
			Predicate: "http://www.w3.org/1999/02/22-rdf-syntax-ns#type",
			Object:    "http://www.w3.org/2000/01/rdf-schema#Class",
		},
		{
			Subject:   classIRI,
			Predicate: "http://www.w3.org/2000/01/rdf-schema#label",
			Object:    fmt.Sprintf("%q", slug),
		},
	}
	if parent := jsonld.SubClassOf(rawContext); parent != "" {
		triples = append(triples, repositories.Triple{
			Subject:   classIRI,
			Predicate: "http://www.w3.org/2000/01/rdf-schema#subClassOf",
			Object:    "urn:type:" + parent,
		})
	}
	if err := store.AddTriples(ctx, triples); err != nil {
		logger.Error(ctx, "kg failed to emit explicit ontology triples",
			"slug", slug, "error", err)
	}
}
