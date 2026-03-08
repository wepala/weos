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

type OrganizationService interface {
	Create(ctx context.Context, cmd CreateOrganizationCommand) (*entities.Organization, error)
	GetByID(ctx context.Context, id string) (*entities.Organization, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.Organization], error)
	Update(ctx context.Context, cmd UpdateOrganizationCommand) (*entities.Organization, error)
	Delete(ctx context.Context, cmd DeleteOrganizationCommand) error
}

type organizationService struct {
	repo       repositories.OrganizationRepository
	eventStore domain.EventStore
	dispatcher *domain.EventDispatcher
	logger     entities.Logger
}

func ProvideOrganizationService(params struct {
	fx.In
	Repo       repositories.OrganizationRepository
	EventStore domain.EventStore
	Dispatcher *domain.EventDispatcher
	Logger     entities.Logger
}) OrganizationService {
	return &organizationService{
		repo:       params.Repo,
		eventStore: params.EventStore,
		dispatcher: params.Dispatcher,
		logger:     params.Logger,
	}
}

func (s *organizationService) Create(
	ctx context.Context, cmd CreateOrganizationCommand,
) (*entities.Organization, error) {
	entity, err := new(entities.Organization).With(cmd.Name, cmd.Slug)
	if err != nil {
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track organization: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit organization: %w", err)
	}

	s.logger.Info(ctx, "organization created", "id", entity.GetID())
	return entity, nil
}

func (s *organizationService) GetByID(
	ctx context.Context, id string,
) (*entities.Organization, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *organizationService) List(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Organization], error) {
	return s.repo.FindAll(ctx, cursor, limit)
}

func (s *organizationService) Update(
	ctx context.Context, cmd UpdateOrganizationCommand,
) (*entities.Organization, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if err := entity.Update(
		cmd.Name, cmd.Slug, cmd.Description, cmd.URL, cmd.LogoURL, cmd.Status,
	); err != nil {
		return nil, fmt.Errorf("failed to update organization: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track organization: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit organization update: %w", err)
	}

	s.logger.Info(ctx, "organization updated", "id", entity.GetID())
	return entity, nil
}

func (s *organizationService) Delete(
	ctx context.Context, cmd DeleteOrganizationCommand,
) error {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return err
	}
	if err := entity.MarkDeleted(); err != nil {
		return fmt.Errorf("failed to mark organization deleted: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return fmt.Errorf("failed to track organization: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit organization deletion: %w", err)
	}

	s.logger.Info(ctx, "organization deleted", "id", cmd.ID)
	return nil
}
