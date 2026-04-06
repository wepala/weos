// Package presets provides functions to register all built-in presets and create a
// fully populated registry.
package presets

import (
	"weos/application"
	"weos/application/presets/auth"
	"weos/application/presets/core"
	"weos/application/presets/ecommerce"
	"weos/application/presets/events"
	"weos/application/presets/knowledge"
	"weos/application/presets/tasks"
	"weos/application/presets/website"
)

var customRegistrars []func(*application.PresetRegistry)

// RegisterAll registers all built-in presets with the given registry.
func RegisterAll(registry *application.PresetRegistry) {
	core.Register(registry)
	auth.Register(registry)
	ecommerce.Register(registry)
	tasks.Register(registry)
	website.Register(registry)
	events.Register(registry)
	knowledge.Register(registry)
	for _, r := range customRegistrars {
		r(registry)
	}
}

// NewDefaultRegistry creates a registry with all built-in presets registered.
func NewDefaultRegistry() *application.PresetRegistry {
	registry := application.NewPresetRegistry()
	RegisterAll(registry)
	return registry
}
