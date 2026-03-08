// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package application

import (
	"context"
	"fmt"

	"weos/domain/entities"
	"weos/domain/repositories"

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
	repo   repositories.PersonRepository
	logger entities.Logger
}

func ProvidePersonService(params struct {
	fx.In
	Repo   repositories.PersonRepository
	Logger entities.Logger
}) PersonService {
	return &personService{
		repo:   params.Repo,
		logger: params.Logger,
	}
}

func (s *personService) Create(
	ctx context.Context, cmd CreatePersonCommand,
) (*entities.Person, error) {
	entity, err := new(entities.Person).With(cmd.GivenName, cmd.FamilyName, cmd.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to create person: %w", err)
	}
	if err := s.repo.Save(ctx, entity); err != nil {
		return nil, err
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
	if err := entity.Restore(
		entity.GetID(), cmd.GivenName, cmd.FamilyName, cmd.Email,
		cmd.AvatarURL, cmd.Status, entity.CreatedAt(), entity.GetSequenceNo(),
	); err != nil {
		return nil, fmt.Errorf("failed to update person: %w", err)
	}
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "person updated", "id", entity.GetID())
	return entity, nil
}

func (s *personService) Delete(
	ctx context.Context, cmd DeletePersonCommand,
) error {
	if err := s.repo.Delete(ctx, cmd.ID); err != nil {
		return err
	}
	s.logger.Info(ctx, "person deleted", "id", cmd.ID)
	return nil
}
