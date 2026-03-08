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
	"encoding/json"
	"fmt"
	"strings"

	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.uber.org/fx"
)

type ResourceService interface {
	Create(ctx context.Context, cmd CreateResourceCommand) (*entities.Resource, error)
	GetByID(ctx context.Context, id string) (*entities.Resource, error)
	List(ctx context.Context, typeSlug, cursor string, limit int) (
		repositories.PaginatedResponse[*entities.Resource], error)
	Update(ctx context.Context, cmd UpdateResourceCommand) (*entities.Resource, error)
	Delete(ctx context.Context, cmd DeleteResourceCommand) error
}

type resourceService struct {
	repo     repositories.ResourceRepository
	typeRepo repositories.ResourceTypeRepository
	logger   entities.Logger
}

func ProvideResourceService(params struct {
	fx.In
	Repo     repositories.ResourceRepository
	TypeRepo repositories.ResourceTypeRepository
	Logger   entities.Logger
}) ResourceService {
	return &resourceService{
		repo:     params.Repo,
		typeRepo: params.TypeRepo,
		logger:   params.Logger,
	}
}

func (s *resourceService) Create(
	ctx context.Context, cmd CreateResourceCommand,
) (*entities.Resource, error) {
	rt, err := s.typeRepo.FindBySlug(ctx, cmd.TypeSlug)
	if err != nil {
		return nil, fmt.Errorf("resource type %q not found: %w", cmd.TypeSlug, err)
	}

	if err := validateAgainstSchema(rt.Schema(), cmd.Data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	entity, err := new(entities.Resource).With(cmd.TypeSlug, cmd.Data, rt.Context(), rt.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	if err := s.repo.Save(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "resource created", "id", entity.GetID(), "type", cmd.TypeSlug)
	return entity, nil
}

func (s *resourceService) GetByID(
	ctx context.Context, id string,
) (*entities.Resource, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *resourceService) List(
	ctx context.Context, typeSlug, cursor string, limit int,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return s.repo.FindAllByType(ctx, typeSlug, cursor, limit)
}

func (s *resourceService) Update(
	ctx context.Context, cmd UpdateResourceCommand,
) (*entities.Resource, error) {
	entity, err := s.repo.FindByID(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}

	rt, err := s.typeRepo.FindBySlug(ctx, entity.TypeSlug())
	if err != nil {
		return nil, fmt.Errorf("resource type %q not found: %w", entity.TypeSlug(), err)
	}

	if err := validateAgainstSchema(rt.Schema(), cmd.Data); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	enriched, err := entities.InjectJSONLDForUpdate(cmd.Data, entity.GetID(), rt.Name(), rt.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to inject JSON-LD fields: %w", err)
	}

	if err := entity.Restore(
		entity.GetID(), entity.TypeSlug(), "active",
		enriched, entity.CreatedAt(), entity.GetSequenceNo(),
	); err != nil {
		return nil, fmt.Errorf("failed to update resource: %w", err)
	}
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "resource updated", "id", entity.GetID())
	return entity, nil
}

func (s *resourceService) Delete(
	ctx context.Context, cmd DeleteResourceCommand,
) error {
	if err := s.repo.Delete(ctx, cmd.ID); err != nil {
		return err
	}
	s.logger.Info(ctx, "resource deleted", "id", cmd.ID)
	return nil
}

func validateAgainstSchema(schema, data json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", strings.NewReader(string(schema))); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("invalid JSON data: %w", err)
	}
	return sch.Validate(v)
}
