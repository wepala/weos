package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// subscribeTripleHandlers registers event handlers that project triple events
// to the triples read-model table.
func subscribeTripleHandlers(
	d *domain.EventDispatcher,
	tripleRepo repositories.TripleRepository,
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
