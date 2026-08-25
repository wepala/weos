package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/jsonld"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// The Oxigraph projector mirrors resource/triple/type events into a SPARQL
// knowledge graph. It runs as a background checkpointed subscriber group (see
// peripheral_groups.go) rather than synchronously on the write path: a failure
// here never fails a user's request, and the subscriber retries — and, after
// bounded retries, parks — a persistently failing event. The projection helpers
// below therefore return errors (so the subscriber can retry/park) instead of
// swallowing them as the old synchronous handlers did.
//
// On first run the subscriber's checkpoint is 0, so it replays the whole event
// history into the graph — the backfill is just the initial catch-up, not a
// separate mechanism.

// projectResourcePublished replays the transaction for a Resource.Published
// event, rebuilds the full JSON-LD state, and replaces the subject in the graph.
// It reuses buildStateFromTransaction so the projection sees exactly the data
// the GORM projection writes, including all edges. Returns an error so a
// transient graph failure is retried by the subscriber.
func projectResourcePublished(
	ctx context.Context,
	event domain.EventEnvelope[any],
	eventStore domain.EventStore,
	store repositories.KnowledgeGraphStore,
	logger entities.Logger,
) error {
	if event.TransactionID == "" {
		return fmt.Errorf("kg: Resource.Published for %s has empty transaction id", event.AggregateID)
	}
	txEvents, err := eventStore.GetEventsByTransactionID(ctx, event.TransactionID)
	if err != nil {
		return fmt.Errorf("kg: load transaction for %s: %w", event.AggregateID, err)
	}
	state := buildStateFromTransaction(ctx, txEvents, event.AggregateID, event.SequenceNo, logger)

	if state.IsDelete {
		logger.Debug(ctx, "kg skipping Resource.Published (delete)", "id", event.AggregateID)
		return nil
	}
	if state.Data == nil {
		return fmt.Errorf("kg: Resource.Published for %s produced no data", event.AggregateID)
	}

	// Drop any prior projection for this subject so an update fully replaces the
	// previous graph (covers properties that disappeared from the JSON-LD).
	if err := store.RemoveSubject(ctx, event.AggregateID); err != nil {
		// Continue: LoadOntology below merges into the default graph and RDF
		// triples are a set, so a leftover prior triple at worst lingers until
		// the next update — preferable to failing the whole event over a clear.
		logger.Warn(ctx, "kg failed to clear prior subject", "id", event.AggregateID, "error", err)
	}

	// Inline a remote-string @context (e.g. "https://schema.org/") as @vocab so
	// the graph store parses the document offline — an embedded oxigraph has no
	// network to fetch a remote context, and @vocab expands the bare terms
	// identically (matching the resource-type ontology projection above).
	if err := store.LoadOntology(ctx, "application/ld+json",
		jsonld.InlineVocabContext(state.Data)); err != nil {
		return fmt.Errorf("kg: load resource JSON-LD for %s: %w", event.AggregateID, err)
	}
	logger.Debug(ctx, "kg projected Resource.Published",
		"id", event.AggregateID, "transactionID", event.TransactionID)
	return nil
}

// projectResourceTypeOntology loads the JSON-LD `@context` of a resource type
// into the knowledge graph so SPARQL queries can resolve the predicates and
// classes that resources reference. It also emits explicit ontology triples
// (rdf:type, rdfs:subClassOf) so kg_describe_class can walk the subclass chain
// even on engines that don't auto-extract them from the JSON-LD document.
// Returns an error so the subscriber can retry a transient graph failure.
func projectResourceTypeOntology(
	ctx context.Context,
	name, slug string,
	rawContext json.RawMessage,
	store repositories.KnowledgeGraphStore,
	logger entities.Logger,
) error {
	if len(rawContext) == 0 {
		return nil
	}
	logger.Debug(ctx, "kg projecting ResourceType ontology", "slug", slug)
	// Clear the class subject before reloading so an update replaces (rather
	// than accumulates) its ontology triples — otherwise a changed
	// rdfs:subClassOf leaves the old parent behind and makes class reasoning
	// stale, the same way resource projections clear their subject first.
	classIRI := resourceTypeClassIRI(name, slug, rawContext)
	if err := store.RemoveSubject(ctx, classIRI); err != nil {
		logger.Warn(ctx, "kg failed to clear prior resource type ontology",
			"slug", slug, "class", classIRI, "error", err)
	}
	// A type that has just DECLARED a class (issue #521) advertised the name
	// fallback until now; clear that subject too, or the old class lingers
	// beside the new one until the graph is rebuilt from scratch.
	if previous := resourceTypeClassIRI(name, slug, contextWithoutType(rawContext)); previous != classIRI {
		if err := store.RemoveSubject(ctx, previous); err != nil {
			logger.Warn(ctx, "kg failed to clear the previous resource type class",
				"slug", slug, "class", previous, "error", err)
		}
	}
	if err := store.LoadOntology(ctx, "application/ld+json", ontologyDocument(rawContext)); err != nil {
		return fmt.Errorf("kg: load resource type ontology for %s: %w", slug, err)
	}
	return emitExplicitOntologyTriples(ctx, name, slug, rawContext, store, logger)
}

// ontologyDocument is the type's context as a JSON-LD document the graph
// store can take without side effects: a context-only document, `{"@context":
// …}`, with WeOS control entries (`weos:termAliases`, `weos:adoptedTerms`,
// `weos:abstract`, `rdfs:subClassOf`…) removed.
//
// Loading the raw context as a document was doing two things wrong. A bare
// `"@type":"foaf:Person"` beside its prefix definition — not inside an
// @context — minted a fresh blank node typed with the literal `foaf:Person`
// on every boot, so the graph filled with anonymous nodes under an
// unexpanded IRI. And an array or boolean under a term key is not a valid
// term definition, so a type whose terms had ever been ADOPTED could be
// refused outright by a strict parser and lose its class. The class triples
// the graph needs come from emitExplicitOntologyTriples, which reads the raw
// context; this document contributes prefixes and nothing else.
func ontologyDocument(rawContext json.RawMessage) json.RawMessage {
	ctx := contextWithoutControlKeys(rawContext)
	out, err := json.Marshal(map[string]any{"@context": json.RawMessage(ctx)})
	if err != nil {
		return rawContext
	}
	return out
}

// contextWithoutControlKeys drops the WeOS control entries from a context.
func contextWithoutControlKeys(rawContext json.RawMessage) json.RawMessage {
	var ctx map[string]any
	if json.Unmarshal(rawContext, &ctx) != nil {
		return rawContext
	}
	for key := range ctx {
		if jsonld.ControlKeywords[key] {
			delete(ctx, key)
		}
	}
	out, err := json.Marshal(ctx)
	if err != nil {
		return rawContext
	}
	return out
}

// contextWithoutType is the context as it stood before a class was declared
// on it, so the class the name fallback advertised can be found and cleared.
func contextWithoutType(rawContext json.RawMessage) json.RawMessage {
	var ctx map[string]any
	if json.Unmarshal(rawContext, &ctx) != nil {
		return rawContext
	}
	delete(ctx, "@type")
	out, err := json.Marshal(ctx)
	if err != nil {
		return rawContext
	}
	return out
}

// resourceTypeClassIRI returns the RDF class IRI that resources of this type
// are assigned as their rdf:type — the context @type expanded against @vocab,
// exactly as BuildResourceGraph computes it (falling back to the type Name,
// then the slug). kg_list_classes advertises this IRI and
// kg_describe_class / kg_search_entities filter on it, so it MUST match what
// resources carry. Types with no resolvable vocab fall back to
// `urn:type:<slug>` so context-less types still get a stable class IRI.
func resourceTypeClassIRI(name, slug string, rawContext json.RawMessage) string {
	vocab, _ := jsonld.ParseContext(rawContext)
	typeName := name
	var ctx map[string]any
	if json.Unmarshal(rawContext, &ctx) == nil {
		if ct, ok := ctx["@type"].(string); ok && ct != "" {
			typeName = ct
		}
	}
	if typeName == "" {
		typeName = slug
	}
	iri := jsonld.ExpandIRI(typeName, vocab, ctx)
	if !strings.Contains(iri, "://") && !strings.HasPrefix(iri, "urn:") {
		return "urn:type:" + slug
	}
	return iri
}

// emitExplicitOntologyTriples adds the canonical `rdf:type rdfs:Class` and
// `rdfs:subClassOf <parent>` triples for a resource type, plus a `rdfs:label`
// from the slug. Belt-and-suspenders to the JSON-LD load: some serializers
// don't fully expand context-only declarations into triples, and the
// description tools rely on these being concrete RDF.
func emitExplicitOntologyTriples(
	ctx context.Context,
	name, slug string,
	rawContext json.RawMessage,
	store repositories.KnowledgeGraphStore,
	logger entities.Logger,
) error {
	// Declare the class under the SAME IRI resources are typed with (context
	// @type expanded against @vocab), so kg_list_classes advertises a class
	// that kg_describe_class / kg_search_entities can actually match against
	// projected resources.
	classIRI := resourceTypeClassIRI(name, slug, rawContext)
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
		// Expand the parent against the same context so the subClassOf chain
		// links to the parent's actual class IRI (kg_describe_class walks
		// rdfs:subClassOf*).
		vocab, _ := jsonld.ParseContext(rawContext)
		var ctxMap map[string]any
		_ = json.Unmarshal(rawContext, &ctxMap)
		triples = append(triples, repositories.Triple{
			Subject:   classIRI,
			Predicate: "http://www.w3.org/2000/01/rdf-schema#subClassOf",
			Object:    jsonld.ExpandIRI(parent, vocab, ctxMap),
		})
	}
	if err := store.AddTriples(ctx, triples); err != nil {
		logger.Error(ctx, "kg failed to emit explicit ontology triples",
			"slug", slug, "error", err)
		return fmt.Errorf("kg: emit ontology triples for %s: %w", slug, err)
	}
	return nil
}
