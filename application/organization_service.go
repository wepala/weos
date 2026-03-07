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

type OrganizationService interface {
	Create(ctx context.Context, cmd CreateOrganizationCommand) (*entities.Organization, error)
	GetByID(ctx context.Context, id string) (*entities.Organization, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.Organization], error)
	Update(ctx context.Context, cmd UpdateOrganizationCommand) (*entities.Organization, error)
	Delete(ctx context.Context, cmd DeleteOrganizationCommand) error
}

type organizationService struct {
	repo   repositories.OrganizationRepository
	logger entities.Logger
}

func ProvideOrganizationService(params struct {
	fx.In
	Repo   repositories.OrganizationRepository
	Logger entities.Logger
}) OrganizationService {
	return &organizationService{
		repo:   params.Repo,
		logger: params.Logger,
	}
}

func (s *organizationService) Create(
	ctx context.Context, cmd CreateOrganizationCommand,
) (*entities.Organization, error) {
	entity, err := new(entities.Organization).With(cmd.Name, cmd.Slug)
	if err != nil {
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}
	if err := s.repo.Save(ctx, entity); err != nil {
		return nil, err
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
	if err := entity.Restore(
		entity.GetID(), cmd.Name, cmd.Slug, cmd.Description,
		cmd.URL, cmd.LogoURL, cmd.Status, entity.CreatedAt(),
	); err != nil {
		return nil, fmt.Errorf("failed to update organization: %w", err)
	}
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "organization updated", "id", entity.GetID())
	return entity, nil
}

func (s *organizationService) Delete(
	ctx context.Context, cmd DeleteOrganizationCommand,
) error {
	if err := s.repo.Delete(ctx, cmd.ID); err != nil {
		return err
	}
	s.logger.Info(ctx, "organization deleted", "id", cmd.ID)
	return nil
}
