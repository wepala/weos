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
	"weos/pkg/identity"

	"go.uber.org/fx"
)

type WebsiteService interface {
	Create(ctx context.Context, cmd CreateWebsiteCommand) (*entities.Website, error)
	GetByID(ctx context.Context, id string) (*entities.Website, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.Website], error)
	Update(ctx context.Context, cmd UpdateWebsiteCommand) (*entities.Website, error)
	Delete(ctx context.Context, cmd DeleteWebsiteCommand) error
}

type websiteService struct {
	repo   repositories.WebsiteRepository
	logger entities.Logger
}

func ProvideWebsiteService(params struct {
	fx.In
	Repo   repositories.WebsiteRepository
	Logger entities.Logger
}) WebsiteService {
	return &websiteService{
		repo:   params.Repo,
		logger: params.Logger,
	}
}

func (s *websiteService) Create(
	ctx context.Context, cmd CreateWebsiteCommand,
) (*entities.Website, error) {
	if cmd.Slug == "" {
		cmd.Slug = identity.Slugify(cmd.Name)
	}
	entity, err := new(entities.Website).With(cmd.Name, cmd.URL, cmd.Slug)
	if err != nil {
		return nil, fmt.Errorf("failed to create website: %w", err)
	}
	if err := s.repo.Save(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "website created", "id", entity.GetID())
	return entity, nil
}

func (s *websiteService) GetByID(
	ctx context.Context, id string,
) (*entities.Website, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *websiteService) List(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Website], error) {
	return s.repo.FindAll(ctx, cursor, limit)
}

func (s *websiteService) Update(
	ctx context.Context, cmd UpdateWebsiteCommand,
) (*entities.Website, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if err := entity.Restore(
		entity.GetID(), cmd.Name, entity.Slug(), cmd.URL, cmd.Description,
		cmd.Language, cmd.Status, entity.CreatedAt(),
	); err != nil {
		return nil, fmt.Errorf("failed to update website: %w", err)
	}
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "website updated", "id", entity.GetID())
	return entity, nil
}

func (s *websiteService) Delete(
	ctx context.Context, cmd DeleteWebsiteCommand,
) error {
	if err := s.repo.Delete(ctx, cmd.ID); err != nil {
		return err
	}
	s.logger.Info(ctx, "website deleted", "id", cmd.ID)
	return nil
}
