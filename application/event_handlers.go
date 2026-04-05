package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/pkg/jsonld"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// subscribeEventHandlers registers all projection event handlers with the dispatcher.
func subscribeEventHandlers(params struct {
	fx.In
	Dispatcher   *domain.EventDispatcher
	EventStore   domain.EventStore
	RTRepo       repositories.ResourceTypeRepository
	ResourceRepo repositories.ResourceRepository
	TripleRepo   repositories.TripleRepository
	ProjMgr      repositories.ProjectionManager
	Logger       entities.Logger
}) error {
	if err := subscribeResourceTypeHandlers(
		params.Dispatcher, params.RTRepo, params.ProjMgr, params.Logger,
	); err != nil {
		return fmt.Errorf("resource type handlers: %w", err)
	}
	if err := subscribeResourceHandlers(
		params.Dispatcher, params.EventStore, params.ResourceRepo, params.ProjMgr, params.Logger,
	); err != nil {
		return fmt.Errorf("resource handlers: %w", err)
	}
	if err := subscribeTripleHandlers(
		params.Dispatcher, params.TripleRepo, params.Logger,
	); err != nil {
		return fmt.Errorf("triple handlers: %w", err)
	}
	return nil
}

// --- ResourceType projection handlers ---

func subscribeResourceTypeHandlers(
	d *domain.EventDispatcher,
	repo repositories.ResourceTypeRepository,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	if err := domain.Subscribe(d, "ResourceType.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceTypeCreated]) error {
			p := env.Payload
			entity := &entities.ResourceType{}
			if err := entity.Restore(
				env.AggregateID, p.Name, p.Slug, p.Description, "active",
				p.Context, p.Schema, p.Timestamp, env.SequenceNo,
			); err != nil {
				return err
			}
			if err := repo.Save(ctx, entity); err != nil {
				return err
			}
			// ensureProjection is idempotent (CREATE TABLE IF NOT EXISTS).
			// If it fails and the event is retried, repo.Save will fail with a
			// duplicate key, which the event dispatcher treats as already-processed.
			if err := ensureProjection(ctx, repo, projMgr, p.Slug, p.Schema, p.Context); err != nil {
				logger.Error(ctx, "failed to ensure projection for type",
					"slug", p.Slug, "error", err)
				return err
			}
			logger.Info(ctx, "projecting ResourceType.Created", "id", env.AggregateID)
			return nil
		},
	); err != nil {
		return err
	}

	if err := domain.Subscribe(d, "ResourceType.Updated",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceTypeUpdated]) error {
			existing, err := repo.FindByID(ctx, env.AggregateID)
			if err != nil {
				return fmt.Errorf("projection read failed: %w", err)
			}
			p := env.Payload
			if err := existing.Restore(
				env.AggregateID, p.Name, p.Slug, p.Description, p.Status,
				p.Context, p.Schema, existing.CreatedAt(), env.SequenceNo,
			); err != nil {
				return err
			}
			if err := repo.Update(ctx, existing); err != nil {
				return err
			}
			if err := ensureProjection(ctx, repo, projMgr, p.Slug, p.Schema, p.Context); err != nil {
				logger.Error(ctx, "failed to ensure projection for type",
					"slug", p.Slug, "error", err)
				return err
			}
			logger.Info(ctx, "projecting ResourceType.Updated", "id", env.AggregateID)
			return nil
		},
	); err != nil {
		return err
	}

	return domain.Subscribe(d, "ResourceType.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceTypeDeleted]) error {
			logger.Info(ctx, "projecting ResourceType.Deleted", "id", env.AggregateID)
			return repo.Delete(ctx, env.AggregateID)
		},
	)
}

// ensureProjection creates a projection table for the given type and, if the
// type declares rdfs:subClassOf, also ensures the parent's table exists.
// Every type (abstract or concrete) gets its own table. Ancestor tables are
// populated at resource-event time via dual-projection in the repository.
func ensureProjection(
	ctx context.Context,
	rtRepo repositories.ResourceTypeRepository,
	projMgr repositories.ProjectionManager,
	slug string,
	schema, ldContext json.RawMessage,
) error {
	if err := projMgr.EnsureTable(ctx, slug, schema, ldContext); err != nil {
		return err
	}
	// Also ensure ancestor tables exist for dual-projection.
	parentSlug := jsonld.SubClassOf(ldContext)
	if parentSlug == "" {
		return nil
	}
	parent, err := rtRepo.FindBySlug(ctx, parentSlug)
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil // parent not yet created; will be ensured when parent type arrives
		}
		return fmt.Errorf("failed to look up parent type %q: %w", parentSlug, err)
	}
	return projMgr.EnsureTable(ctx, parentSlug, parent.Schema(), parent.Context())
}

// --- Resource projection handlers ---

func subscribeResourceHandlers(
	d *domain.EventDispatcher,
	eventStore domain.EventStore,
	repo repositories.ResourceRepository,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	// Resource.Published handles the final projection write for resource creation, updates,
	// and deletes. It replays the transaction events to build the full resource state
	// including graph edges, then writes a single projection row.
	if err := domain.Subscribe(d, "Resource.Published",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourcePublished]) error {
			return handleResourcePublished(ctx, env, eventStore, repo, projMgr, logger)
		},
	); err != nil {
		return err
	}

	return domain.Subscribe(d, "Resource.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceDeleted]) error {
			logger.Info(ctx, "projecting Resource.Deleted", "id", env.AggregateID)
			return repo.Delete(ctx, env.AggregateID)
		},
	)
}

// txResourceState holds the resource state built from transaction events.
type txResourceState struct {
	Data      json.RawMessage
	TypeSlug  string
	CreatedBy string
	AccountID string
	CreatedAt time.Time
	MaxSeq    int
	IsCreate  bool
	IsDelete  bool
}

// buildStateFromTransaction walks transaction events and builds the resource state.
func buildStateFromTransaction(
	ctx context.Context,
	txEvents []domain.EventEnvelope[any],
	aggregateID string,
	baseSeq int,
	logger entities.Logger,
) txResourceState {
	state := txResourceState{MaxSeq: baseSeq}
	for _, e := range txEvents {
		if e.AggregateID != aggregateID {
			continue
		}
		if e.SequenceNo > state.MaxSeq {
			state.MaxSeq = e.SequenceNo
		}
		// Payloads from the event store are deserialized as map[string]any.
		// Map keys use Go field names (PascalCase) for untagged fields,
		// or json tag names (lowercase) for tagged fields like BasicTripleEvent.
		m, ok := e.Payload.(map[string]any)
		if !ok {
			logger.Error(ctx, "unexpected event payload type",
				"eventType", e.EventType, "payloadType", fmt.Sprintf("%T", e.Payload))
			continue
		}
		switch e.EventType {
		case "Resource.Created":
			state.IsCreate = true
			state.TypeSlug, _ = m["TypeSlug"].(string)
			state.Data = marshalField(m["Data"])
			state.CreatedBy, _ = m["CreatedBy"].(string)
			state.AccountID, _ = m["AccountID"].(string)
			if ts, ok := m["Timestamp"].(string); ok {
				state.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
			}
		case "Resource.Updated":
			state.Data = marshalField(m["Data"])
		case "Resource.Deleted":
			state.IsDelete = true
		case "Triple.Created":
			predicate, _ := m["predicate"].(string)
			object, _ := m["object"].(string)
			if predicate != "" && state.Data != nil {
				updated, err := AddEdgeToGraph(state.Data, predicate, object, aggregateID)
				if err != nil {
					logger.Error(ctx, "failed to add edge to graph",
						"subject", aggregateID, "predicate", predicate, "error", err)
					continue
				}
				state.Data = updated
			}
		case "Triple.Deleted":
			predicate, _ := m["predicate"].(string)
			object, _ := m["object"].(string)
			if predicate != "" && state.Data != nil {
				updated, err := RemoveEdgeFromGraph(state.Data, predicate, object)
				if err != nil {
					logger.Error(ctx, "failed to remove edge from graph",
						"subject", aggregateID, "predicate", predicate, "error", err)
					continue
				}
				state.Data = updated
			}
		}
	}
	return state
}

func handleResourcePublished(
	ctx context.Context,
	env domain.EventEnvelope[entities.ResourcePublished],
	eventStore domain.EventStore,
	repo repositories.ResourceRepository,
	projMgr repositories.ProjectionManager,
	logger entities.Logger,
) error {
	txID := env.TransactionID
	if txID == "" {
		logger.Error(ctx, "Resource.Published event has empty TransactionID",
			"aggregateID", env.AggregateID, "sequenceNo", env.SequenceNo)
		return fmt.Errorf("Resource.Published event has empty TransactionID for aggregate %s", env.AggregateID)
	}

	txEvents, err := eventStore.GetEventsByTransactionID(ctx, txID)
	if err != nil {
		return fmt.Errorf("failed to load transaction events: %w", err)
	}

	state := buildStateFromTransaction(ctx, txEvents, env.AggregateID, env.SequenceNo, logger)

	// Delete flow: the Resource.Deleted handler already removes the projection row.
	if state.IsDelete {
		logger.Info(ctx, "projecting Resource.Published (delete)",
			"id", env.AggregateID, "transactionID", txID, "sequenceNo", state.MaxSeq)
		return nil
	}

	if state.Data == nil {
		return fmt.Errorf("no resource data found in transaction %s for aggregate %s", txID, env.AggregateID)
	}

	entity := &entities.Resource{}
	if state.IsCreate {
		if err := entity.Restore(
			env.AggregateID, state.TypeSlug, "active",
			state.Data, state.CreatedBy, state.AccountID,
			state.CreatedAt, state.MaxSeq,
		); err != nil {
			return err
		}
		logger.Info(ctx, "projecting Resource.Published (create)",
			"id", env.AggregateID, "transactionID", txID, "sequenceNo", state.MaxSeq)
		if err := repo.Save(ctx, entity); err != nil {
			return err
		}
		return propagateDisplayValues(ctx, env.AggregateID, state.Data, projMgr, logger)
	}

	// Update: read existing resource for fields not in the transaction events.
	existing, err := repo.FindByID(ctx, env.AggregateID)
	if err != nil {
		return fmt.Errorf("projection read failed: %w", err)
	}
	if err := existing.Restore(
		env.AggregateID, existing.TypeSlug(), existing.Status(),
		state.Data, existing.CreatedBy(), existing.AccountID(),
		existing.CreatedAt(), state.MaxSeq,
	); err != nil {
		return err
	}
	logger.Info(ctx, "projecting Resource.Published (update)",
		"id", env.AggregateID, "transactionID", txID, "sequenceNo", state.MaxSeq)
	if err := repo.Update(ctx, existing); err != nil {
		return err
	}
	return propagateDisplayValues(ctx, env.AggregateID, state.Data, projMgr, logger)
}

// marshalField re-marshals a deserialized JSON value back to json.RawMessage.
// Event store payloads are deserialized as map[string]any; fields that were
// originally json.RawMessage (e.g., ResourceCreated.Data) need re-serialization.
// Returns nil if the value is nil or marshaling fails.
func marshalField(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
