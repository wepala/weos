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

// PresetHTTPHandlers is a distinct nominal type for the Fx-provided list of
// resolved preset routes — keeps it from being confused with any other
// []MountedHandler that might enter the graph later.
type PresetHTTPHandlers []MountedHandler

// ProvidePresetHTTPHandlers resolves preset-contributed HTTP routes against
// the same BehaviorServices that behavior factories receive, so handlers and
// behaviors share a single application-services view.
func ProvidePresetHTTPHandlers(
	registry *PresetRegistry,
	resources repositories.ResourceRepository,
	triples repositories.TripleRepository,
	resourceTypes repositories.ResourceTypeRepository,
	logger entities.Logger,
	writer *lazyResourceWriter,
) (PresetHTTPHandlers, error) {
	if registry == nil {
		return nil, fmt.Errorf("ProvidePresetHTTPHandlers: nil PresetRegistry")
	}
	if isNilInterface(resources) {
		return nil, fmt.Errorf("ProvidePresetHTTPHandlers: nil Resources")
	}
	if isNilInterface(triples) {
		return nil, fmt.Errorf("ProvidePresetHTTPHandlers: nil Triples")
	}
	if isNilInterface(resourceTypes) {
		return nil, fmt.Errorf("ProvidePresetHTTPHandlers: nil ResourceTypes")
	}
	if isNilInterface(logger) {
		return nil, fmt.Errorf("ProvidePresetHTTPHandlers: nil Logger")
	}
	if writer == nil {
		return nil, fmt.Errorf("ProvidePresetHTTPHandlers: nil lazyResourceWriter")
	}
	services := BehaviorServices{
		Resources:     resources,
		Triples:       triples,
		ResourceTypes: resourceTypes,
		Logger:        logger,
		Writer:        writer,
	}
	mounted, err := registry.Handlers(services)
	if err != nil {
		return nil, err
	}
	if len(mounted) > 0 {
		logger.Info(context.Background(), "preset HTTP handlers registered",
			"count", len(mounted))
	}
	return mounted, nil
}
