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

type ResourceTypeService interface {
	Create(ctx context.Context, cmd CreateResourceTypeCommand) (*entities.ResourceType, error)
	GetByID(ctx context.Context, id string) (*entities.ResourceType, error)
	GetBySlug(ctx context.Context, slug string) (*entities.ResourceType, error)
	List(ctx context.Context, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.ResourceType], error)
	Update(ctx context.Context, cmd UpdateResourceTypeCommand) (*entities.ResourceType, error)
	Delete(ctx context.Context, cmd DeleteResourceTypeCommand) error
	ListPresets() []PresetDefinition
	InstallPreset(ctx context.Context, presetName string) (*InstallPresetResult, error)
}

type resourceTypeService struct {
	repo    repositories.ResourceTypeRepository
	projMgr repositories.ProjectionManager
	logger  entities.Logger
}

func ProvideResourceTypeService(params struct {
	fx.In
	Repo    repositories.ResourceTypeRepository
	ProjMgr repositories.ProjectionManager
	Logger  entities.Logger
}) ResourceTypeService {
	return &resourceTypeService{
		repo:    params.Repo,
		projMgr: params.ProjMgr,
		logger:  params.Logger,
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
	if err := s.repo.Save(ctx, entity); err != nil {
		return nil, err
	}
	if err := s.projMgr.EnsureTable(ctx, cmd.Slug, cmd.Schema); err != nil {
		s.logger.Error(ctx, "failed to create projection table", "slug", cmd.Slug, "error", err)
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
	if err := entity.Restore(
		entity.GetID(), cmd.Name, cmd.Slug, cmd.Description, cmd.Status,
		cmd.Context, cmd.Schema, entity.CreatedAt(), entity.GetSequenceNo(),
	); err != nil {
		return nil, fmt.Errorf("failed to update resource type: %w", err)
	}
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	if err := s.projMgr.EnsureTable(ctx, cmd.Slug, cmd.Schema); err != nil {
		s.logger.Error(ctx, "failed to update projection table", "slug", cmd.Slug, "error", err)
	}
	s.logger.Info(ctx, "resource type updated", "id", entity.GetID())
	return entity, nil
}

func (s *resourceTypeService) Delete(
	ctx context.Context, cmd DeleteResourceTypeCommand,
) error {
	if err := s.repo.Delete(ctx, cmd.ID); err != nil {
		return err
	}
	s.logger.Info(ctx, "resource type deleted", "id", cmd.ID)
	return nil
}

func (s *resourceTypeService) ListPresets() []PresetDefinition {
	return ListPresetDefinitions()
}

func (s *resourceTypeService) InstallPreset(
	ctx context.Context, presetName string,
) (*InstallPresetResult, error) {
	preset, ok := GetPresetDefinition(presetName)
	if !ok {
		return nil, fmt.Errorf("unknown preset %q", presetName)
	}
	result := &InstallPresetResult{}
	for _, pt := range preset.Types {
		if _, err := s.GetBySlug(ctx, pt.Slug); err == nil {
			result.Skipped = append(result.Skipped, pt.Slug)
			continue
		}
		_, err := s.Create(ctx, CreateResourceTypeCommand{
			Name:        pt.Name,
			Slug:        pt.Slug,
			Description: pt.Description,
			Context:     pt.Context,
			Schema:      pt.Schema,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create resource type %q: %w", pt.Slug, err)
		}
		result.Created = append(result.Created, pt.Slug)
	}
	return result, nil
}
