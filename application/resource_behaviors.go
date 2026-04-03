package application

import (
	"weos/domain/entities"

	"go.uber.org/fx"
)

// ResourceBehaviorRegistry maps resource type slugs to their custom behaviors.
// Types without a registered behavior use DefaultBehavior (no-op).
type ResourceBehaviorRegistry map[string]entities.ResourceBehavior

// ProvideResourceBehaviorRegistry builds the behavior registry at startup.
// Add dependencies to the params struct as concrete behaviors need them.
func ProvideResourceBehaviorRegistry(params struct {
	fx.In
}) ResourceBehaviorRegistry {
	registry := make(ResourceBehaviorRegistry)
	// Register concrete behaviors here, e.g.:
	// registry["web-page"] = &WebPageBehavior{}
	return registry
}
