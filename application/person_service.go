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

type PersonService interface {
	Create(ctx context.Context, cmd CreatePersonCommand) (*entities.Person, error)
	GetByID(ctx context.Context, id string) (*entities.Person, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.Person], error)
	Update(ctx context.Context, cmd UpdatePersonCommand) (*entities.Person, error)
	Delete(ctx context.Context, cmd DeletePersonCommand) error
}

type personService struct {
	repo       repositories.PersonRepository
	eventStore domain.EventStore
	dispatcher *domain.EventDispatcher
	logger     entities.Logger
}

func ProvidePersonService(params struct {
	fx.In
	Repo       repositories.PersonRepository
	EventStore domain.EventStore
	Dispatcher *domain.EventDispatcher
	Logger     entities.Logger
}) PersonService {
	return &personService{
		repo:       params.Repo,
		eventStore: params.EventStore,
		dispatcher: params.Dispatcher,
		logger:     params.Logger,
	}
}

func (s *personService) Create(
	ctx context.Context, cmd CreatePersonCommand,
) (*entities.Person, error) {
	entity, err := new(entities.Person).With(cmd.GivenName, cmd.FamilyName, cmd.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to create person: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track person: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit person: %w", err)
	}

	s.logger.Info(ctx, "person created", "id", entity.GetID())
	return entity, nil
}

func (s *personService) GetByID(
	ctx context.Context, id string,
) (*entities.Person, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *personService) List(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Person], error) {
	return s.repo.FindAll(ctx, cursor, limit)
}

func (s *personService) Update(
	ctx context.Context, cmd UpdatePersonCommand,
) (*entities.Person, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if err := entity.Update(
		cmd.GivenName, cmd.FamilyName, cmd.Email, cmd.AvatarURL, cmd.Status,
	); err != nil {
		return nil, fmt.Errorf("failed to update person: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return nil, fmt.Errorf("failed to track person: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit person update: %w", err)
	}

	s.logger.Info(ctx, "person updated", "id", entity.GetID())
	return entity, nil
}

func (s *personService) Delete(
	ctx context.Context, cmd DeletePersonCommand,
) error {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return err
	}
	if err := entity.MarkDeleted(); err != nil {
		return fmt.Errorf("failed to mark person deleted: %w", err)
	}

	uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
	if err := uow.Track(entity); err != nil {
		return fmt.Errorf("failed to track person: %w", err)
	}
	if err := uow.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit person deletion: %w", err)
	}

	s.logger.Info(ctx, "person deleted", "id", cmd.ID)
	return nil
}
