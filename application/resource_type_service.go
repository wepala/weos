package application

import (
	"context"
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
	repo       repositories.ResourceTypeRepository
	projMgr    repositories.ProjectionManager
	eventStore domain.EventStore
	dispatcher *domain.EventDispatcher
	registry   *PresetRegistry
	logger     entities.Logger
}

func ProvideResourceTypeService(params struct {
	fx.In
	Repo       repositories.ResourceTypeRepository
	ProjMgr    repositories.ProjectionManager
	EventStore domain.EventStore
	Dispatcher *domain.EventDispatcher
	Registry   *PresetRegistry
	Logger     entities.Logger
}) ResourceTypeService {
	return &resourceTypeService{
		repo:       params.Repo,
		projMgr:    params.ProjMgr,
		eventStore: params.EventStore,
		dispatcher: params.Dispatcher,
		registry:   params.Registry,
		logger:     params.Logger,
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
		if err == nil {
			if !update {
				result.Skipped = append(result.Skipped, pt.Slug)
				continue
			}
			_, err := s.Update(ctx, UpdateResourceTypeCommand{
				ID:          existing.GetID(),
				Name:        pt.Name,
				Slug:        pt.Slug,
				Description: pt.Description,
				Context:     pt.Context,
				Schema:      pt.Schema,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to update resource type %q: %w", pt.Slug, err)
			}
			result.Updated = append(result.Updated, pt.Slug)
			continue
		}
		_, err = s.Create(ctx, CreateResourceTypeCommand(pt))
		if err != nil {
			return nil, fmt.Errorf("failed to create resource type %q: %w", pt.Slug, err)
		}
		result.Created = append(result.Created, pt.Slug)
	}
	return result, nil
}
