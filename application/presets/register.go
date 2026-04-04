// Package presets provides a convenience function to register all built-in presets.
package presets

import (
	"weos/application"
	"weos/application/presets/auth"
	"weos/application/presets/core"
	"weos/application/presets/events"
	"weos/application/presets/knowledge"
	"weos/application/presets/tasks"
	"weos/application/presets/website"
)

// RegisterAll registers all built-in presets with the given registry.
func RegisterAll(registry *application.PresetRegistry) {
	core.Register(registry)
	auth.Register(registry)
	tasks.Register(registry)
	website.Register(registry)
	events.Register(registry)
	knowledge.Register(registry)
}

// NewDefaultRegistry creates a registry with all built-in presets registered.
func NewDefaultRegistry() *application.PresetRegistry {
	registry := application.NewPresetRegistry()
	RegisterAll(registry)
	return registry
}
