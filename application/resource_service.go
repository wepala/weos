package application

import (
	"context"
	"encoding/json"
	"fmt"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/pkg/identity"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
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
	ListFlat(ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[map[string]any], error)
	ListByField(ctx context.Context, typeSlug, fieldName, fieldValue string) (
		repositories.PaginatedResponse[*entities.Resource], error)
	ListWithFilters(ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
		cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[*entities.Resource], error)
	ListFlatWithFilters(ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
		cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[map[string]any], error)
	Update(ctx context.Context, cmd UpdateResourceCommand) (*entities.Resource, error)
	Delete(ctx context.Context, cmd DeleteResourceCommand) error
}

type resourceService struct {
	repo        repositories.ResourceRepository
	typeRepo    repositories.ResourceTypeRepository
	permRepo    repositories.ResourcePermissionRepository
	accountRepo authrepos.AccountRepository
	eventStore  domain.EventStore
	dispatcher  *domain.EventDispatcher
	logger      entities.Logger
	behaviors   ResourceBehaviorRegistry
}

func (s *resourceService) behaviorFor(slug string) entities.ResourceBehavior {
	if b, ok := s.behaviors[slug]; ok {
		return b
	}
	return entities.DefaultBehavior{}
}

func ProvideResourceService(params struct {
	fx.In
	Repo        repositories.ResourceRepository
	TypeRepo    repositories.ResourceTypeRepository
	PermRepo    repositories.ResourcePermissionRepository
	AccountRepo authrepos.AccountRepository
	EventStore  domain.EventStore
	Dispatcher  *domain.EventDispatcher
	Logger      entities.Logger
	Behaviors   ResourceBehaviorRegistry
}) ResourceService {
	return &resourceService{
		repo:        params.Repo,
		typeRepo:    params.TypeRepo,
		permRepo:    params.PermRepo,
		accountRepo: params.AccountRepo,
		eventStore:  params.EventStore,
		dispatcher:  params.Dispatcher,
		logger:      params.Logger,
		behaviors:   params.Behaviors,
	}
}

func (s *resourceService) Create(
	ctx context.Context, cmd CreateResourceCommand,
) (*entities.Resource, error) {
	rt, err := s.typeRepo.FindBySlug(ctx, cmd.TypeSlug)
	if err != nil {
		return nil, fmt.Errorf("resource type %q not found: %w", cmd.TypeSlug, err)
	}

	behavior := s.behaviorFor(cmd.TypeSlug)

	data, err := behavior.BeforeCreate(ctx, cmd.Data, rt)
	if err != nil {
		return nil, fmt.Errorf("behavior BeforeCreate rejected: %w", err)
	}

	if err := validateAgainstSchema(rt.Schema(), data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	var createdBy, accountID string
	if ident := auth.AgentFromCtx(ctx); ident != nil {
		createdBy = ident.AgentID
		accountID = ident.ActiveAccountID
	}

	entityID := identity.NewResource(cmd.TypeSlug)
	refProps := ExtractReferenceProperties(rt.Schema(), rt.Context())
	graphData, err := BuildResourceGraph(data, refProps, entityID, rt.Name(), rt.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to build resource graph: %w", err)
	}

	entity, err := new(entities.Resource).With(entityID, cmd.TypeSlug, graphData, createdBy, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	if err := behavior.BeforeCreateCommit(ctx, entity); err != nil {
		return nil, fmt.Errorf("behavior BeforeCreateCommit rejected: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource: %w", err)
	}

	if err := behavior.AfterCreate(ctx, entity); err != nil {
		s.logger.Error(ctx, "behavior AfterCreate failed", "id", entity.GetID(), "error", err)
	}

	s.logger.Info(ctx, "resource created", "id", entity.GetID(), "type", cmd.TypeSlug)
	return entity, nil
}

func (s *resourceService) buildVisibilityScope(ctx context.Context) *repositories.VisibilityScope {
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return nil
	}
	return &repositories.VisibilityScope{
		AgentID:   identity.AgentID,
		AccountID: identity.ActiveAccountID,
		IsAdmin:   false, // per-user scoping: lists always filter by creator + permissions
	}
}

func (s *resourceService) checkInstanceAccess(
	ctx context.Context, entity *entities.Resource, action string,
) error {
	identity := auth.AgentFromCtx(ctx)
	if identity == nil {
		return nil // system context (CLI/MCP) — allow
	}
	// Admin/owner bypass: only if the caller is admin/owner in the RESOURCE's account
	if entity.AccountID() != "" {
		role, _ := s.accountRepo.FindMemberRole(ctx, entity.AccountID(), identity.AgentID)
		if role == "admin" || role == "owner" {
			return nil
		}
	}
	// Creator access
	if entity.CreatedBy() == identity.AgentID {
		return nil
	}
	// Explicit permission grant
	if has, _ := s.permRepo.HasPermission(ctx, entity.GetID(), identity.AgentID, action); has {
		return nil
	}
	// Backward compatibility: pre-migration resources with no owner
	if entity.CreatedBy() == "" {
		return nil
	}
	return entities.ErrAccessDenied
}

func (s *resourceService) GetByID(
	ctx context.Context, id string,
) (*entities.Resource, error) {
	entity, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkInstanceAccess(ctx, entity, "read"); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *resourceService) List(
	ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return s.repo.FindAllByType(ctx, typeSlug, cursor, limit, sort, s.buildVisibilityScope(ctx))
}

func (s *resourceService) ListByField(
	ctx context.Context, typeSlug, fieldName, fieldValue string,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	items, err := s.repo.FindAllByTypeAndField(ctx, typeSlug, fieldName, fieldValue)
	if err != nil {
		return repositories.PaginatedResponse[*entities.Resource]{}, err
	}
	return repositories.PaginatedResponse[*entities.Resource]{
		Data:    items,
		HasMore: false,
	}, nil
}

func (s *resourceService) ListFlat(
	ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	return s.repo.FindAllByTypeFlat(ctx, typeSlug, cursor, limit, sort, s.buildVisibilityScope(ctx))
}

func (s *resourceService) ListFlatWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	scope := s.buildVisibilityScope(ctx)
	return s.repo.FindAllByTypeFlatWithFilters(ctx, typeSlug, filters, cursor, limit, sort, scope)
}

func (s *resourceService) ListWithFilters(
	ctx context.Context, typeSlug string, filters []repositories.FilterCondition,
	cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	scope := s.buildVisibilityScope(ctx)
	return s.repo.FindAllByTypeWithFilters(ctx, typeSlug, filters, cursor, limit, sort, scope)
}

func (s *resourceService) Update(
	ctx context.Context, cmd UpdateResourceCommand,
) (*entities.Resource, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	if err := s.checkInstanceAccess(ctx, entity, "modify"); err != nil {
		return nil, err
	}

	rt, err := s.typeRepo.FindBySlug(ctx, entity.TypeSlug())
	if err != nil {
		return nil, fmt.Errorf("resource type %q not found: %w", entity.TypeSlug(), err)
	}

	behavior := s.behaviorFor(entity.TypeSlug())

	data, err := behavior.BeforeUpdate(ctx, entity, cmd.Data, rt)
	if err != nil {
		return nil, fmt.Errorf("behavior BeforeUpdate rejected: %w", err)
	}

	if err := validateAgainstSchema(rt.Schema(), data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	refProps := ExtractReferenceProperties(rt.Schema(), rt.Context())
	graphData, err := BuildResourceGraph(data, refProps, entity.GetID(), rt.Name(), rt.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to build resource graph: %w", err)
	}

	if err := entity.Update(graphData); err != nil {
		return nil, fmt.Errorf("failed to update resource: %w", err)
	}

	if err := behavior.BeforeUpdateCommit(ctx, entity); err != nil {
		return nil, fmt.Errorf("behavior BeforeUpdateCommit rejected: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource update: %w", err)
	}

	if err := behavior.AfterUpdate(ctx, entity); err != nil {
		s.logger.Error(ctx, "behavior AfterUpdate failed", "id", entity.GetID(), "error", err)
	}

	s.logger.Info(ctx, "resource updated", "id", entity.GetID())
	return entity, nil
}

//nolint:dupl // entity-specific delete with domain-specific error messages
func (s *resourceService) Delete(
	ctx context.Context, cmd DeleteResourceCommand,
) error {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return err
	}

	if err := s.checkInstanceAccess(ctx, entity, "delete"); err != nil {
		return err
	}

	behavior := s.behaviorFor(entity.TypeSlug())

	if err := behavior.BeforeDelete(ctx, entity); err != nil {
		return fmt.Errorf("behavior BeforeDelete rejected: %w", err)
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

	if err := behavior.AfterDelete(ctx, entity); err != nil {
		s.logger.Error(ctx, "behavior AfterDelete failed", "id", entity.GetID(), "error", err)
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
