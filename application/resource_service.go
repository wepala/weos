package application

import (
	"context"
	"encoding/json"
	"fmt"

	"weos/domain/entities"
	"weos/domain/repositories"

	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.uber.org/fx"
)

type ResourceService interface {
	Create(ctx context.Context, cmd CreateResourceCommand) (*entities.Resource, error)
	GetByID(ctx context.Context, id string) (*entities.Resource, error)
	List(ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[*entities.Resource], error)
	ListWithFilters(ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
		cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[*entities.Resource], error)
	Update(ctx context.Context, cmd UpdateResourceCommand) (*entities.Resource, error)
	Delete(ctx context.Context, cmd DeleteResourceCommand) error
}

type resourceService struct {
	repo       repositories.ResourceRepository
	typeRepo   repositories.ResourceTypeRepository
	eventStore domain.EventStore
	dispatcher *domain.EventDispatcher
	logger     entities.Logger
}

func ProvideResourceService(params struct {
	fx.In
	Repo       repositories.ResourceRepository
	TypeRepo   repositories.ResourceTypeRepository
	EventStore domain.EventStore
	Dispatcher *domain.EventDispatcher
	Logger     entities.Logger
}) ResourceService {
	return &resourceService{
		repo:       params.Repo,
		typeRepo:   params.TypeRepo,
		eventStore: params.EventStore,
		dispatcher: params.Dispatcher,
		logger:     params.Logger,
	}
}

func (s *resourceService) Create(
	ctx context.Context, cmd CreateResourceCommand,
) (*entities.Resource, error) {
	rt, err := s.typeRepo.FindBySlug(ctx, cmd.TypeSlug)
	if err != nil {
		return nil, fmt.Errorf("resource type %q not found: %w", cmd.TypeSlug, err)
	}

	if err := validateAgainstSchema(rt.Schema(), cmd.Data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	entity, err := new(entities.Resource).With(cmd.TypeSlug, cmd.Data, rt.Context(), rt.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource: %w", err)
	}

	s.logger.Info(ctx, "resource created", "id", entity.GetID(), "type", cmd.TypeSlug)
	return entity, nil
}

func (s *resourceService) GetByID(
	ctx context.Context, id string,
) (*entities.Resource, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *resourceService) List(
	ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return s.repo.FindAllByType(ctx, typeSlug, cursor, limit, sort)
}

func (s *resourceService) ListWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return s.repo.FindAllByTypeWithFilters(ctx, typeSlug, filters, cursor, limit, sort)
}

func (s *resourceService) Update(
	ctx context.Context, cmd UpdateResourceCommand,
) (*entities.Resource, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	rt, err := s.typeRepo.FindBySlug(ctx, entity.TypeSlug())
	if err != nil {
		return nil, fmt.Errorf("resource type %q not found: %w", entity.TypeSlug(), err)
	}

	if err := validateAgainstSchema(rt.Schema(), cmd.Data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	enriched, err := entities.InjectJSONLDForUpdate(cmd.Data, entity.GetID(), rt.Name(), rt.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to inject JSON-LD fields: %w", err)
	}

	if err := entity.Update(enriched); err != nil {
		return nil, fmt.Errorf("failed to update resource: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource update: %w", err)
	}

	s.logger.Info(ctx, "resource updated", "id", entity.GetID())
	return entity, nil
}

func (s *resourceService) Delete(
	ctx context.Context, cmd DeleteResourceCommand,
) error {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return err
	}
	if err := entity.MarkDeleted(); err != nil {
		return fmt.Errorf("failed to mark resource deleted: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return fmt.Errorf("failed to track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit resource deletion: %w", err)
	}

	s.logger.Info(ctx, "resource deleted", "id", cmd.ID)
	return nil
}

func validateAgainstSchema(schema, data json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}

	var schemaDoc any
	if err := json.Unmarshal(schema, &schemaDoc); err != nil {
		return fmt.Errorf("invalid schema JSON: %w", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", schemaDoc); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("invalid JSON data: %w", err)
	}
	return sch.Validate(v)
}
