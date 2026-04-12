// Package presets provides functions to register all built-in presets and create a
// fully populated registry.
package presets

import (
	"weos/application"
	"weos/application/presets/core"
	"weos/application/presets/ecommerce"
	"weos/application/presets/events"
	"weos/application/presets/knowledge"
	"weos/application/presets/mealplanning"
	"weos/application/presets/tasks"
	"weos/application/presets/website"
)

var customRegistrars []func(*application.PresetRegistry)

// Register adds a custom preset registrar that will be invoked by RegisterAll
// after the built-in presets have been registered. Downstream services that
// embed the weos binary can call this from a package init() to plug custom
// presets into the default registry before cli.Execute() runs.
func Register(fn func(*application.PresetRegistry)) {
	if fn == nil {
		return
	}
	customRegistrars = append(customRegistrars, fn)
}

// RegisterAll registers all built-in presets with the given registry.
func RegisterAll(registry *application.PresetRegistry) {
	core.Register(registry)
	ecommerce.Register(registry)
	tasks.Register(registry)
	website.Register(registry)
	events.Register(registry)
	knowledge.Register(registry)
	mealplanning.Register(registry)
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
