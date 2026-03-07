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

type PageService interface {
	Create(ctx context.Context, cmd CreatePageCommand) (*entities.Page, error)
	GetByID(ctx context.Context, id string) (*entities.Page, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.Page], error)
	ListByWebsiteID(
		ctx context.Context, websiteID, cursor string, limit int,
	) (repositories.PaginatedResponse[*entities.Page], error)
	Update(ctx context.Context, cmd UpdatePageCommand) (*entities.Page, error)
	Delete(ctx context.Context, cmd DeletePageCommand) error
}

type pageService struct {
	repo   repositories.PageRepository
	logger entities.Logger
}

func ProvidePageService(params struct {
	fx.In
	Repo   repositories.PageRepository
	Logger entities.Logger
}) PageService {
	return &pageService{
		repo:   params.Repo,
		logger: params.Logger,
	}
}

func (s *pageService) Create(
	ctx context.Context, cmd CreatePageCommand,
) (*entities.Page, error) {
	if cmd.Slug == "" {
		cmd.Slug = identity.Slugify(cmd.Name)
	}
	websiteSlug := identity.ExtractWebsiteSlug(cmd.WebsiteID)
	entity, err := new(entities.Page).With(cmd.Name, cmd.Slug, websiteSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}
	if err := s.repo.Save(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "page created", "id", entity.GetID())
	return entity, nil
}

func (s *pageService) GetByID(
	ctx context.Context, id string,
) (*entities.Page, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *pageService) List(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Page], error) {
	return s.repo.FindAll(ctx, cursor, limit)
}

func (s *pageService) ListByWebsiteID(
	ctx context.Context, websiteID, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Page], error) {
	return s.repo.FindByWebsiteID(ctx, websiteID, cursor, limit)
}

func (s *pageService) Update(
	ctx context.Context, cmd UpdatePageCommand,
) (*entities.Page, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if err := entity.Restore(
		entity.GetID(), cmd.Name, cmd.Slug, cmd.Description,
		cmd.Template, cmd.Status, cmd.Position, entity.CreatedAt(),
	); err != nil {
		return nil, fmt.Errorf("failed to update page: %w", err)
	}
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "page updated", "id", entity.GetID())
	return entity, nil
}

func (s *pageService) Delete(
	ctx context.Context, cmd DeletePageCommand,
) error {
	if err := s.repo.Delete(ctx, cmd.ID); err != nil {
		return err
	}
	s.logger.Info(ctx, "page deleted", "id", cmd.ID)
	return nil
}
