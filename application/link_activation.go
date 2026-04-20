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

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// LinkActivator walks the LinkRegistry and activates any link whose source
// and target resource types are both installed. Activation means asking the
// ProjectionManager to add the FK + display columns and record forward/reverse
// reference entries — the same effects schema-declared x-resource-type
// properties produce.
//
// Reconcile is idempotent and safe to call repeatedly. It's the single entry
// point used from both startup (builtin_resource_types.go after AutoInstall)
// and InstallPreset, so any path that adds a resource type re-evaluates every
// previously-dormant link.
type LinkActivator struct {
	registry *LinkRegistry
	projMgr  repositories.ProjectionManager
	typeRepo repositories.ResourceTypeRepository
	logger   entities.Logger
}

// NewLinkActivator constructs a LinkActivator, validating that every
// dependency is present. Moving the nil-check to construction time keeps
// Reconcile's hot path honest: a running activator is guaranteed to have
// a working registry, projection manager, repo, and logger. Returns an
// error (rather than panicking) so Fx surfaces wiring mistakes cleanly.
func NewLinkActivator(
	registry *LinkRegistry,
	projMgr repositories.ProjectionManager,
	typeRepo repositories.ResourceTypeRepository,
	logger entities.Logger,
) (*LinkActivator, error) {
	if registry == nil {
		return nil, fmt.Errorf("NewLinkActivator: registry is required")
	}
	if projMgr == nil {
		return nil, fmt.Errorf("NewLinkActivator: projMgr is required")
	}
	if typeRepo == nil {
		return nil, fmt.Errorf("NewLinkActivator: typeRepo is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("NewLinkActivator: logger is required")
	}
	return &LinkActivator{
		registry: registry,
		projMgr:  projMgr,
		typeRepo: typeRepo,
		logger:   logger,
	}, nil
}

// Reconcile loads the set of installed resource-type slugs and activates
// every link in the registry whose endpoints are both present. Links whose
// source type lacks a projection table are skipped silently by
// ProjectionManager.RegisterLink; they'll activate on a subsequent call
// after the source is installed.
//
// Individual RegisterLink failures are logged and counted but don't stop
// the pass — one bad link shouldn't block siblings. A final aggregate error
// surfaces to the caller so InstallPreset / startup can escalate; per-link
// errors are also available in logs for operator diagnosis.
func (a *LinkActivator) Reconcile(ctx context.Context) error {
	installed, err := a.loadInstalledSlugs(ctx)
	if err != nil {
		return fmt.Errorf("LinkActivator: load installed types: %w", err)
	}

	active := a.registry.ActiveFor(installed)
	var failed int
	for _, link := range active {
		err := a.projMgr.RegisterLink(ctx, repositories.LinkReference{
			SourceSlug:      link.SourceType,
			PropertyName:    link.PropertyName,
			TargetSlug:      link.TargetType,
			DisplayProperty: link.DisplayProperty,
		})
		if err != nil {
			failed++
			a.logger.Error(ctx, "link activation failed",
				"source", link.SourceType, "target", link.TargetType,
				"property", link.PropertyName, "error", err)
			continue
		}
		a.logger.Info(ctx, "link activated",
			"source", link.SourceType, "target", link.TargetType,
			"property", link.PropertyName)
	}
	if failed > 0 {
		return fmt.Errorf("link activation: %d of %d links failed", failed, len(active))
	}
	return nil
}

// loadInstalledSlugs paginates through every installed resource type and
// returns a set of slugs. A chunked cursor pagination (500 per page) avoids
// pulling the full resource-type table into memory in one query, even though
// in practice the count is small.
func (a *LinkActivator) loadInstalledSlugs(ctx context.Context) (map[string]bool, error) {
	installed := make(map[string]bool)
	cursor := ""
	const pageSize = 500
	for {
		page, err := a.typeRepo.FindAll(ctx, cursor, pageSize)
		if err != nil {
			return nil, err
		}
		for _, rt := range page.Data {
			installed[rt.Slug()] = true
		}
		if !page.HasMore || page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	return installed, nil
}
