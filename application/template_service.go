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

type TemplateService interface {
	Create(ctx context.Context, cmd CreateTemplateCommand) (*entities.Template, error)
	GetByID(ctx context.Context, id string) (*entities.Template, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.Template], error)
	ListByThemeID(
		ctx context.Context, themeID, cursor string, limit int,
	) (repositories.PaginatedResponse[*entities.Template], error)
	Update(ctx context.Context, cmd UpdateTemplateCommand) (*entities.Template, error)
	Delete(ctx context.Context, cmd DeleteTemplateCommand) error
}

type templateService struct {
	repo   repositories.TemplateRepository
	logger entities.Logger
}

func ProvideTemplateService(params struct {
	fx.In
	Repo   repositories.TemplateRepository
	Logger entities.Logger
}) TemplateService {
	return &templateService{
		repo:   params.Repo,
		logger: params.Logger,
	}
}

func (s *templateService) Create(
	ctx context.Context, cmd CreateTemplateCommand,
) (*entities.Template, error) {
	themeSlug := identity.ExtractThemeSlug(cmd.ThemeID)
	entity, err := new(entities.Template).With(cmd.Name, cmd.Slug, themeSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to create template: %w", err)
	}
	if err := s.repo.Save(ctx, entity, cmd.ThemeID); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "template created", "id", entity.GetID())
	return entity, nil
}

func (s *templateService) GetByID(
	ctx context.Context, id string,
) (*entities.Template, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *templateService) List(
	ctx context.Context, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Template], error) {
	return s.repo.FindAll(ctx, cursor, limit)
}

func (s *templateService) ListByThemeID(
	ctx context.Context, themeID, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Template], error) {
	return s.repo.FindByThemeID(ctx, themeID, cursor, limit)
}

func (s *templateService) Update(
	ctx context.Context, cmd UpdateTemplateCommand,
) (*entities.Template, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if err := entity.Restore(
		entity.GetID(), cmd.Name, cmd.Slug, cmd.Description,
		cmd.FilePath, cmd.Status, entity.CreatedAt(), entity.GetSequenceNo(),
	); err != nil {
		return nil, fmt.Errorf("failed to update template: %w", err)
	}
	if err := s.repo.Update(ctx, entity, cmd.ThemeID); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "template updated", "id", entity.GetID())
	return entity, nil
}

func (s *templateService) Delete(
	ctx context.Context, cmd DeleteTemplateCommand,
) error {
	if err := s.repo.Delete(ctx, cmd.ID); err != nil {
		return err
	}
	s.logger.Info(ctx, "template deleted", "id", cmd.ID)
	return nil
}
