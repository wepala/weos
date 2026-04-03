package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/pkg/utils"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// subscribeTripleHandlers registers event handlers for triple projection and auto-extraction.
func subscribeTripleHandlers(
	d *domain.EventDispatcher,
	tripleRepo repositories.TripleRepository,
	tripleService TripleService,
	resourceRepo repositories.ResourceRepository,
	rtRepo repositories.ResourceTypeRepository,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	// Project Triple.Created events to the triples read-model table.
	if err := domain.Subscribe(d, "Triple.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.TripleCreated]) error {
			p := env.Payload
			logger.Info(ctx, "projecting Triple.Created",
				"subject", p.Subject, "predicate", p.Predicate, "object", p.Object)
			return tripleRepo.SaveTriple(ctx, p.Subject, p.Predicate, p.Object)
		},
	); err != nil {
		return fmt.Errorf("triple created handler: %w", err)
	}

	// Project Triple.Deleted events.
	if err := domain.Subscribe(d, "Triple.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.TripleDeleted]) error {
			p := env.Payload
			logger.Info(ctx, "projecting Triple.Deleted",
				"subject", p.Subject, "predicate", p.Predicate, "object", p.Object)
			return tripleRepo.DeleteTriple(ctx, p.Subject, p.Predicate, p.Object)
		},
	); err != nil {
		return fmt.Errorf("triple deleted handler: %w", err)
	}

	// Auto-extract triples from resource data on creation.
	if err := domain.Subscribe(d, "Resource.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceCreated]) error {
			return autoExtractTriples(ctx, env.AggregateID, env.Payload.TypeSlug,
				env.Payload.Data, tripleService, rtRepo, logger)
		},
	); err != nil {
		return fmt.Errorf("resource triple extraction handler: %w", err)
	}

	// Reconcile triples on resource update.
	if err := domain.Subscribe(d, "Resource.Updated",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceUpdated]) error {
			resource, err := resourceRepo.FindByID(ctx, env.AggregateID)
			if err != nil {
				logger.Error(ctx, "failed to find resource for triple reconciliation",
					"id", env.AggregateID, "error", err)
				return nil
			}
			return reconcileTriples(ctx, env.AggregateID, resource.TypeSlug(),
				env.Payload.Data, tripleRepo, tripleService, rtRepo, logger)
		},
	); err != nil {
		return fmt.Errorf("resource triple reconciliation handler: %w", err)
	}

	// Clean up triples on resource deletion.
	if err := domain.Subscribe(d, "Resource.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceDeleted]) error {
			logger.Info(ctx, "cleaning up triples for deleted resource", "id", env.AggregateID)
			existing, err := tripleRepo.FindBySubject(ctx, env.AggregateID)
			if err != nil {
				return fmt.Errorf("failed to find triples for cleanup: %w", err)
			}
			for _, t := range existing {
				if err := tripleService.Unlink(ctx, t.Subject, t.Predicate, t.Object); err != nil {
					logger.Error(ctx, "failed to unlink triple on delete",
						"subject", t.Subject, "predicate", t.Predicate, "object", t.Object, "error", err)
				}
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("resource triple cleanup handler: %w", err)
	}

	// Sync triple changes to projection table FK and display columns.
	if err := domain.Subscribe(d, "Triple.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.TripleCreated]) error {
			return syncTripleToProjection(ctx, env.Payload.Subject, env.Payload.Predicate,
				env.Payload.Object, rtRepo, resourceRepo, projMgr, logger)
		},
	); err != nil {
		return fmt.Errorf("triple projection sync created handler: %w", err)
	}

	if err := domain.Subscribe(d, "Triple.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.TripleDeleted]) error {
			return syncTripleToProjection(ctx, env.Payload.Subject, env.Payload.Predicate,
				"", rtRepo, resourceRepo, projMgr, logger)
		},
	); err != nil {
		return fmt.Errorf("triple projection sync deleted handler: %w", err)
	}

	// Propagate display value changes when a referenced entity is updated.
	if err := domain.Subscribe(d, "Resource.Updated",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceUpdated]) error {
			return propagateDisplayValues(ctx, env.AggregateID, env.Payload.Data,
				projMgr, logger)
		},
	); err != nil {
		return fmt.Errorf("display value propagation handler: %w", err)
	}

	return nil
}

// syncTripleToProjection updates a projection table FK column and its display column
// when a triple is created or deleted.
func syncTripleToProjection(
	ctx context.Context,
	subject, predicate string,
	objectID string,
	rtRepo repositories.ResourceTypeRepository,
	resourceRepo repositories.ResourceRepository,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	typeSlug := extractTypeSlugFromResourceID(subject)
	if typeSlug == "" || !projMgr.HasProjectionTable(typeSlug) {
		return nil
	}

	rt, err := rtRepo.FindBySlug(ctx, typeSlug)
	if err != nil {
		logger.Error(ctx, "failed to find resource type for projection sync",
			"typeSlug", typeSlug, "error", err)
		return nil
	}

	// Find which reference property maps to this predicate.
	refProps := ExtractReferenceProperties(rt.Schema(), rt.Context())
	for _, rp := range refProps {
		if rp.PredicateIRI != predicate {
			continue
		}
		colName := utils.CamelToSnake(rp.PropertyName)

		// Update the FK column.
		var fkValue any
		if objectID != "" {
			fkValue = objectID
		}
		if err := projMgr.UpdateColumn(ctx, typeSlug, subject, colName, fkValue); err != nil {
			logger.Error(ctx, "failed to sync triple to projection",
				"subject", subject, "column", colName, "error", err)
		}

		// Resolve and update the display column.
		displayCol := colName + "_display"
		var displayValue any
		if objectID != "" {
			displayValue = resolveDisplayValue(ctx, objectID, rp.DisplayProperty, resourceRepo)
		}
		if err := projMgr.UpdateColumn(ctx, typeSlug, subject, displayCol, displayValue); err != nil {
			logger.Error(ctx, "failed to sync display column to projection",
				"subject", subject, "column", displayCol, "error", err)
		}
		return nil
	}
	return nil
}

// propagateDisplayValues updates _display columns in all projection tables that reference
// the updated resource. Uses the reverse-reference index to find affected types and
// performs a bulk SQL update per referencing type.
func propagateDisplayValues(
	ctx context.Context,
	resourceID string,
	data json.RawMessage,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	typeSlug := extractTypeSlugFromResourceID(resourceID)
	if typeSlug == "" {
		return nil
	}

	reverseRefs := projMgr.ReverseReferences(typeSlug)
	if len(reverseRefs) == 0 {
		return nil
	}

	// Extract entity node to read display property values.
	entityNode := ExtractEntityNode(data)
	var node map[string]any
	if json.Unmarshal(entityNode, &node) != nil {
		return nil
	}

	for _, ref := range reverseRefs {
		val, ok := node[ref.DisplayProperty]
		if !ok {
			continue
		}
		displayStr := fmt.Sprint(val)
		if err := projMgr.UpdateColumnByFK(
			ctx, ref.TypeSlug, ref.FKColumn, resourceID, ref.DisplayColumn, displayStr,
		); err != nil {
			logger.Error(ctx, "failed to propagate display value",
				"targetType", ref.TypeSlug, "column", ref.DisplayColumn, "error", err)
		}
	}
	return nil
}

// resolveDisplayValue looks up a resource by ID and extracts a property from its entity node.
func resolveDisplayValue(
	ctx context.Context,
	resourceID, displayProperty string,
	resourceRepo repositories.ResourceRepository,
) string {
	resource, err := resourceRepo.FindByID(ctx, resourceID)
	if err != nil {
		return ""
	}

	entityNode := ExtractEntityNode(resource.Data())
	var node map[string]any
	if json.Unmarshal(entityNode, &node) != nil {
		return ""
	}

	if val, ok := node[displayProperty]; ok {
		return fmt.Sprint(val)
	}
	return ""
}

// extractTypeSlugFromResourceID extracts the type slug from a resource URN.
// Resource URN format: "urn:<typeSlug>:<ksuid>"
func extractTypeSlugFromResourceID(id string) string {
	parts := strings.Split(id, ":")
	if len(parts) == 3 && parts[0] == "urn" {
		switch parts[1] {
		case "person", "org", "theme", "type":
			return ""
		}
		return parts[1]
	}
	return ""
}

// autoExtractTriples extracts triples from resource data based on x-resource-type schema properties.
func autoExtractTriples(
	ctx context.Context,
	resourceID, typeSlug string,
	data json.RawMessage,
	tripleService TripleService,
	rtRepo repositories.ResourceTypeRepository,
	logger entities.Logger,
) error {
	rt, err := rtRepo.FindBySlug(ctx, typeSlug)
	if err != nil {
		return nil // resource type not found — nothing to extract
	}

	refProps := ExtractReferenceProperties(rt.Schema(), rt.Context())
	if len(refProps) == 0 {
		return nil
	}

	triples := ExtractTriplesFromData(refProps, data, resourceID)
	for _, t := range triples {
		if err := tripleService.Link(ctx, t.Subject, t.Predicate, t.Object); err != nil {
			logger.Error(ctx, "failed to auto-link triple",
				"subject", t.Subject, "predicate", t.Predicate, "object", t.Object, "error", err)
		}
	}
	return nil
}

// reconcileTriples diffs the current triples against the updated data and adds/removes as needed.
func reconcileTriples(
	ctx context.Context,
	resourceID, typeSlug string,
	data json.RawMessage,
	tripleRepo repositories.TripleRepository,
	tripleService TripleService,
	rtRepo repositories.ResourceTypeRepository,
	logger entities.Logger,
) error {
	rt, err := rtRepo.FindBySlug(ctx, typeSlug)
	if err != nil {
		return nil
	}

	refProps := ExtractReferenceProperties(rt.Schema(), rt.Context())
	if len(refProps) == 0 {
		return nil
	}

	// Build set of expected predicates from schema.
	schemaPredicates := make(map[string]bool)
	for _, rp := range refProps {
		schemaPredicates[rp.PredicateIRI] = true
	}

	// Extract new triples from updated data.
	newTriples := ExtractTriplesFromData(refProps, data, resourceID)
	newSet := make(map[string]bool)
	for _, t := range newTriples {
		newSet[t.Subject+"|"+t.Predicate+"|"+t.Object] = true
	}

	// Find existing triples for this subject.
	existing, err := tripleRepo.FindBySubject(ctx, resourceID)
	if err != nil {
		logger.Error(ctx, "failed to find existing triples for reconciliation",
			"id", resourceID, "error", err)
		return nil
	}

	// Remove stale triples (existing triples whose predicate is schema-derived but not in new set).
	for _, t := range existing {
		if !schemaPredicates[t.Predicate] {
			continue // not a schema-derived triple — leave it alone
		}
		key := t.Subject + "|" + t.Predicate + "|" + t.Object
		if !newSet[key] {
			if err := tripleService.Unlink(ctx, t.Subject, t.Predicate, t.Object); err != nil {
				logger.Error(ctx, "failed to unlink stale triple",
					"subject", t.Subject, "predicate", t.Predicate, "object", t.Object, "error", err)
			}
		}
	}

	// Add new triples.
	existingSet := make(map[string]bool)
	for _, t := range existing {
		existingSet[t.Subject+"|"+t.Predicate+"|"+t.Object] = true
	}
	for _, t := range newTriples {
		key := t.Subject + "|" + t.Predicate + "|" + t.Object
		if !existingSet[key] {
			if err := tripleService.Link(ctx, t.Subject, t.Predicate, t.Object); err != nil {
				logger.Error(ctx, "failed to link new triple",
					"subject", t.Subject, "predicate", t.Predicate, "object", t.Object, "error", err)
			}
		}
	}

	return nil
}
