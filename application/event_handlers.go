package application

import (
	"context"
	"encoding/json"
	"fmt"

	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// subscribeEventHandlers registers all projection event handlers with the dispatcher.
func subscribeEventHandlers(params struct {
	fx.In
	Dispatcher   *domain.EventDispatcher
	PersonRepo   repositories.PersonRepository
	OrgRepo      repositories.OrganizationRepository
	RTRepo       repositories.ResourceTypeRepository
	ResourceRepo repositories.ResourceRepository
	ProjMgr      repositories.ProjectionManager
	Logger       entities.Logger
}) error {
	if err := subscribePersonHandlers(
		params.Dispatcher, params.PersonRepo, params.Logger,
	); err != nil {
		return fmt.Errorf("person handlers: %w", err)
	}
	if err := subscribeOrganizationHandlers(
		params.Dispatcher, params.OrgRepo, params.Logger,
	); err != nil {
		return fmt.Errorf("organization handlers: %w", err)
	}
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
	return nil
}

// --- Person projection handlers ---

func subscribePersonHandlers(
	d *domain.EventDispatcher,
	repo repositories.PersonRepository,
	logger entities.Logger,
) error {
	if err := domain.Subscribe(d, "Person.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.PersonCreated]) error {
			p := env.Payload
			entity := &entities.Person{}
			if err := entity.Restore(
				env.AggregateID, p.GivenName, p.FamilyName, p.Email,
				"", "active", p.Timestamp, env.SequenceNo,
			); err != nil {
				return err
			}
			logger.Info(ctx, "projecting Person.Created", "id", env.AggregateID)
			return repo.Save(ctx, entity)
		},
	); err != nil {
		return err
	}

	if err := domain.Subscribe(d, "Person.Updated",
		func(ctx context.Context, env domain.EventEnvelope[entities.PersonUpdated]) error {
			existing, err := repo.FindByID(ctx, env.AggregateID)
			if err != nil {
				return fmt.Errorf("projection read failed: %w", err)
			}
			p := env.Payload
			if err := existing.Restore(
				env.AggregateID, p.GivenName, p.FamilyName, p.Email,
				p.AvatarURL, p.Status, existing.CreatedAt(), env.SequenceNo,
			); err != nil {
				return err
			}
			logger.Info(ctx, "projecting Person.Updated", "id", env.AggregateID)
			return repo.Update(ctx, existing)
		},
	); err != nil {
		return err
	}

	return domain.Subscribe(d, "Person.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.PersonDeleted]) error {
			logger.Info(ctx, "projecting Person.Deleted", "id", env.AggregateID)
			return repo.Delete(ctx, env.AggregateID)
		},
	)
}

// --- Organization projection handlers ---

func subscribeOrganizationHandlers(
	d *domain.EventDispatcher,
	repo repositories.OrganizationRepository,
	logger entities.Logger,
) error {
	if err := domain.Subscribe(d, "Organization.Created",
		func(ctx context.Context, env domain.EventEnvelope[entities.OrganizationCreated]) error {
			p := env.Payload
			entity := &entities.Organization{}
			if err := entity.Restore(
				env.AggregateID, p.Name, p.Slug, "", "", "", "active",
				p.Timestamp, env.SequenceNo,
			); err != nil {
				return err
			}
			logger.Info(ctx, "projecting Organization.Created", "id", env.AggregateID)
			return repo.Save(ctx, entity)
		},
	); err != nil {
		return err
	}

	if err := domain.Subscribe(d, "Organization.Updated",
		func(ctx context.Context, env domain.EventEnvelope[entities.OrganizationUpdated]) error {
			existing, err := repo.FindByID(ctx, env.AggregateID)
			if err != nil {
				return fmt.Errorf("projection read failed: %w", err)
			}
			p := env.Payload
			if err := existing.Restore(
				env.AggregateID, p.Name, p.Slug, p.Description,
				p.URL, p.LogoURL, p.Status, existing.CreatedAt(), env.SequenceNo,
			); err != nil {
				return err
			}
			logger.Info(ctx, "projecting Organization.Updated", "id", env.AggregateID)
			return repo.Update(ctx, existing)
		},
	); err != nil {
		return err
	}

	return domain.Subscribe(d, "Organization.Deleted",
		func(ctx context.Context, env domain.EventEnvelope[entities.OrganizationDeleted]) error {
			logger.Info(ctx, "projecting Organization.Deleted", "id", env.AggregateID)
			return repo.Delete(ctx, env.AggregateID)
		},
	)
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
			if err := projMgr.EnsureTable(ctx, p.Slug, p.Schema); err != nil {
				logger.Error(ctx, "failed to create projection table",
					"slug", p.Slug, "error", err)
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
			if err := projMgr.EnsureTable(ctx, p.Slug, p.Schema); err != nil {
				logger.Error(ctx, "failed to update projection table",
					"slug", p.Slug, "error", err)
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
				json.RawMessage(p.Data), p.Timestamp, env.SequenceNo,
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
				json.RawMessage(env.Payload.Data), existing.CreatedAt(), env.SequenceNo,
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
