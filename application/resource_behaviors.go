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
)

// BehaviorServices bundles application services that ResourceBehavior factories
// may depend on. All fields are required when constructed by
// ProvideResourceBehaviorRegistry; tests that build BehaviorServices directly
// must supply real or fake implementations for any field their behavior touches.
type BehaviorServices struct {
	Resources     repositories.ResourceRepository
	Triples       repositories.TripleRepository
	ResourceTypes repositories.ResourceTypeRepository
	Logger        entities.Logger
}

// BehaviorFactory constructs a ResourceBehavior given the available application
// services. Factories are invoked once at startup, after the Fx container is
// wired, allowing behaviors to close over real repositories and loggers.
// A factory must not return nil — that is treated as a programmer error and
// fails startup.
type BehaviorFactory func(services BehaviorServices) entities.ResourceBehavior

// StaticBehavior wraps a pre-constructed ResourceBehavior in a BehaviorFactory.
// Use this for behaviors that have no service dependencies, so presets can
// declare them inline without writing a factory function. Panics if b is nil.
func StaticBehavior(b entities.ResourceBehavior) BehaviorFactory {
	if b == nil {
		panic("application.StaticBehavior: behavior must not be nil")
	}
	return func(BehaviorServices) entities.ResourceBehavior { return b }
}

// ResourceBehaviorRegistry maps resource type slugs to their custom behaviors.
// Types without a registered behavior use DefaultBehavior (no-op).
type ResourceBehaviorRegistry map[string]entities.ResourceBehavior

// ProvideResourceBehaviorRegistry builds the behavior registry from all
// registered presets, invoking each factory with the supplied services. Fails
// startup if any injected dependency is nil or any factory returns nil.
func ProvideResourceBehaviorRegistry(
	registry *PresetRegistry,
	resources repositories.ResourceRepository,
	triples repositories.TripleRepository,
	resourceTypes repositories.ResourceTypeRepository,
	logger entities.Logger,
) (ResourceBehaviorRegistry, error) {
	if registry == nil {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil PresetRegistry")
	}
	if resources == nil {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil Resources")
	}
	if triples == nil {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil Triples")
	}
	if resourceTypes == nil {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil ResourceTypes")
	}
	if logger == nil {
		return nil, fmt.Errorf("ProvideResourceBehaviorRegistry: nil Logger")
	}
	services := BehaviorServices{
		Resources:     resources,
		Triples:       triples,
		ResourceTypes: resourceTypes,
		Logger:        logger,
	}
	behaviors, err := registry.Behaviors(services)
	if err != nil {
		return nil, err
	}
	for slug := range behaviors {
		logger.Info(context.Background(), "resource behavior registered", "slug", slug)
	}
	return behaviors, nil
}

// BehaviorMetaRegistry maps resource type slugs to their behavior metadata.
// Used by services to expose available behaviors and enforce manageability.
type BehaviorMetaRegistry map[string]entities.BehaviorMeta

// ProvideBehaviorMetaRegistry builds the metadata registry from all presets.
func ProvideBehaviorMetaRegistry(registry *PresetRegistry) BehaviorMetaRegistry {
	return registry.BehaviorsMeta()
}
