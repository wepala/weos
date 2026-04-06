package application

import (
	"context"
	"errors"
	"fmt"

	"weos/domain/entities"
	"weos/domain/repositories"

	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

var reservedSlugs = map[string]bool{
	"persons":        true,
	"organizations":  true,
	"health":         true,
	"resource-types": true,
	"websites":       true,
	"pages":          true,
	"sections":       true,
	"themes":         true,
	"templates":      true,
}

// ReservedResourceTypeSlugs returns the set of slugs that cannot be used as
// resource type identifiers because they conflict with API route prefixes.
func ReservedResourceTypeSlugs() map[string]bool {
	cp := make(map[string]bool, len(reservedSlugs))
	for k, v := range reservedSlugs {
		cp[k] = v
	}
	return cp
}

type ResourceTypeService interface {
	Create(ctx context.Context, cmd CreateResourceTypeCommand) (*entities.ResourceType, error)
	GetByID(ctx context.Context, id string) (*entities.ResourceType, error)
	GetBySlug(ctx context.Context, slug string) (*entities.ResourceType, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.ResourceType], error)
	Update(ctx context.Context, cmd UpdateResourceTypeCommand) (*entities.ResourceType, error)
	Delete(ctx context.Context, cmd DeleteResourceTypeCommand) error
	ListPresets() []PresetDefinition
	InstallPreset(ctx context.Context, presetName string, update bool) (*InstallPresetResult, error)
}

type resourceTypeService struct {
	repo        repositories.ResourceTypeRepository
	projMgr     repositories.ProjectionManager
	eventStore  domain.EventStore
	dispatcher  *domain.EventDispatcher
	registry    *PresetRegistry
	logger      entities.Logger
	resourceSvc ResourceService
}

func ProvideResourceTypeService(params struct {
	fx.In
	Repo        repositories.ResourceTypeRepository
	ProjMgr     repositories.ProjectionManager
	EventStore  domain.EventStore
	Dispatcher  *domain.EventDispatcher
	Registry    *PresetRegistry
	Logger      entities.Logger
	ResourceSvc ResourceService
}) ResourceTypeService {
	return &resourceTypeService{
		repo:        params.Repo,
		projMgr:     params.ProjMgr,
		eventStore:  params.EventStore,
		dispatcher:  params.Dispatcher,
		registry:    params.Registry,
		logger:      params.Logger,
		resourceSvc: params.ResourceSvc,
	}
}

func (s *resourceTypeService) Create(
	ctx context.Context, cmd CreateResourceTypeCommand,
) (*entities.ResourceType, error) {
	if reservedSlugs[cmd.Slug] {
		return nil, fmt.Errorf("slug %q is reserved", cmd.Slug)
	}
	entity, err := new(entities.ResourceType).With(
		cmd.Name, cmd.Slug, cmd.Description, cmd.Context, cmd.Schema,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource type: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource type: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource type: %w", err)
	}

	s.logger.Info(ctx, "resource type created", "id", entity.GetID())
	return entity, nil
}

func (s *resourceTypeService) GetByID(
	ctx context.Context, id string,
) (*entities.ResourceType, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *resourceTypeService) GetBySlug(
	ctx context.Context, slug string,
) (*entities.ResourceType, error) {
	return s.repo.FindBySlug(ctx, slug)
}

func (s *resourceTypeService) List(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	return s.repo.FindAll(ctx, cursor, limit)
}

func (s *resourceTypeService) Update(
	ctx context.Context, cmd UpdateResourceTypeCommand,
) (*entities.ResourceType, error) {
	if reservedSlugs[cmd.Slug] {
		return nil, fmt.Errorf("slug %q is reserved", cmd.Slug)
	}
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if err := entity.Update(
		cmd.Name, cmd.Slug, cmd.Description, cmd.Status, cmd.Context, cmd.Schema,
	); err != nil {
		return nil, fmt.Errorf("failed to update resource type: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track resource type: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit resource type update: %w", err)
	}

	s.logger.Info(ctx, "resource type updated", "id", entity.GetID())
	return entity, nil
}

func (s *resourceTypeService) Delete(
	ctx context.Context, cmd DeleteResourceTypeCommand,
) error {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return err
	}
	if err := entity.MarkDeleted(); err != nil {
		return fmt.Errorf("failed to mark resource type deleted: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return fmt.Errorf("failed to track resource type: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit resource type deletion: %w", err)
	}

	s.logger.Info(ctx, "resource type deleted", "id", cmd.ID)
	return nil
}

func (s *resourceTypeService) ListPresets() []PresetDefinition {
	return s.registry.List()
}

func (s *resourceTypeService) InstallPreset(
	ctx context.Context, presetName string, update bool,
) (*InstallPresetResult, error) {
	preset, ok := s.registry.Get(presetName)
	if !ok {
		return nil, fmt.Errorf("unknown preset %q", presetName)
	}
	result := &InstallPresetResult{}
	for _, pt := range preset.Types {
		existing, err := s.GetBySlug(ctx, pt.Slug)
		switch {
		case err == nil:
			if !update {
				result.Skipped = append(result.Skipped, pt.Slug)
				continue
			}
			_, uErr := s.Update(ctx, UpdateResourceTypeCommand{
				ID:          existing.GetID(),
				Name:        pt.Name,
				Slug:        pt.Slug,
				Description: pt.Description,
				Status:      existing.Status(),
				Context:     pt.Context,
				Schema:      pt.Schema,
			})
			if uErr != nil {
				return result, fmt.Errorf("failed to update resource type %q: %w", pt.Slug, uErr)
			}
			result.Updated = append(result.Updated, pt.Slug)
		case errors.Is(err, repositories.ErrNotFound):
			_, cErr := s.Create(ctx, CreateResourceTypeCommand{
				Name: pt.Name, Slug: pt.Slug, Description: pt.Description,
				Context: pt.Context, Schema: pt.Schema,
			})
			if cErr != nil {
				return result, fmt.Errorf("failed to create resource type %q: %w", pt.Slug, cErr)
			}
			result.Created = append(result.Created, pt.Slug)
			s.seedFixtures(ctx, pt, result)
		default:
			return result, fmt.Errorf("failed to look up resource type %q: %w", pt.Slug, err)
		}
	}
	return result, nil
}

// seedFixtures creates resources from the preset type's fixture data.
// Fixtures require a schema on the resource type for validation.
// Failures are logged but do not prevent the rest of the preset from installing.
// Built-in fixtures seeded at startup (via ensureBuiltInResourceTypes) use a
// background context and have no owner — they are intentionally global/public.
func (s *resourceTypeService) seedFixtures(
	ctx context.Context, pt PresetResourceType, result *InstallPresetResult,
) {
	if len(pt.Fixtures) == 0 {
		return
	}
	if len(pt.Schema) == 0 {
		s.logger.Error(ctx, "cannot seed fixtures without a schema", "slug", pt.Slug)
		return
	}
	if result.Seeded == nil {
		result.Seeded = make(map[string]int)
	}
	count := 0
	for i, fixture := range pt.Fixtures {
		// Schema validation is handled by ResourceService.Create.
		_, err := s.resourceSvc.Create(ctx, CreateResourceCommand{
			TypeSlug: pt.Slug,
			Data:     fixture,
		})
		if err != nil {
			s.logger.Error(ctx, "failed to seed fixture",
				"slug", pt.Slug, "index", i, "error", err)
			continue
		}
		count++
	}
	result.Seeded[pt.Slug] = count
	if count > 0 {
		s.logger.Info(ctx, "seeded fixture data", "slug", pt.Slug, "count", count)
	}
}
