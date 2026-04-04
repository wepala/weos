package application

import (
	"context"
	"encoding/json"
	"fmt"

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
	RTRepo       repositories.ResourceTypeRepository
	ResourceRepo repositories.ResourceRepository
	TripleRepo   repositories.TripleRepository
	ProjMgr      repositories.ProjectionManager
	TripleSvc    TripleService
	Logger       entities.Logger
}) error {
	if err := subscribeResourceTypeHandlers(
		params.Dispatcher, params.RTRepo, params.ProjMgr, params.Logger,
	); err != nil {
		return fmt.Errorf("resource type handlers: %w", err)
	}
	if err := subscribeResourceHandlers(
		params.Dispatcher, params.ResourceRepo, params.Logger,
	); err != nil {
		return fmt.Errorf("resource handlers: %w", err)
	}
	// Triple handlers must run AFTER resource handlers so projections exist before triples sync.
	if err := subscribeTripleHandlers(
		params.Dispatcher, params.TripleRepo, params.TripleSvc,
		params.ResourceRepo, params.RTRepo, params.ProjMgr, params.Logger,
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
			if !jsonld.IsAbstract(p.Context) {
				if err := projMgr.EnsureTable(ctx, p.Slug, p.Schema, p.Context); err != nil {
					logger.Error(ctx, "failed to create projection table",
						"slug", p.Slug, "error", err)
				}
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
			if !jsonld.IsAbstract(p.Context) {
				if err := projMgr.EnsureTable(ctx, p.Slug, p.Schema, p.Context); err != nil {
					logger.Error(ctx, "failed to update projection table",
						"slug", p.Slug, "error", err)
				}
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

// --- Resource projection handlers ---

func subscribeResourceHandlers(
	d *domain.EventDispatcher,
	repo repositories.ResourceRepository,
	logger entities.Logger,
) error {
	if err := domain.Subscribe(d, "Resource.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceCreated]) error {
			p := env.Payload
			entity := &entities.Resource{}
			if err := entity.Restore(
				env.AggregateID, p.TypeSlug, "active",
				json.RawMessage(p.Data), p.CreatedBy, p.AccountID,
				p.Timestamp, env.SequenceNo,
			); err != nil {
				return err
			}
			logger.Info(ctx, "projecting Resource.Created", "id", env.AggregateID)
			return repo.Save(ctx, entity)
		},
	); err != nil {
		return err
	}

	if err := domain.Subscribe(d, "Resource.Updated",
		func(ctx context.Context, env domain.EventEnvelope[entities.ResourceUpdated]) error {
			existing, err := repo.FindByID(ctx, env.AggregateID)
			if err != nil {
				return fmt.Errorf("projection read failed: %w", err)
			}
			if err := existing.Restore(
				env.AggregateID, existing.TypeSlug(), existing.Status(),
				json.RawMessage(env.Payload.Data), existing.CreatedBy(), existing.AccountID(),
				existing.CreatedAt(), env.SequenceNo,
			); err != nil {
				return err
			}
			logger.Info(ctx, "projecting Resource.Updated", "id", env.AggregateID)
			return repo.Update(ctx, existing)
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
