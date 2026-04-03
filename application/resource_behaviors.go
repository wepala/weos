package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	registry["person"] = &PersonBehavior{}
	registry["organization"] = &OrganizationBehavior{}
	return registry
}

// PersonBehavior provides custom logic for the "person" resource type.
// It computes a full "name" field from givenName and familyName.
type PersonBehavior struct {
	entities.DefaultBehavior
}

func (b *PersonBehavior) BeforeCreate(
	ctx context.Context, data json.RawMessage, rt *entities.ResourceType,
) (json.RawMessage, error) {
	return injectPersonName(data)
}

func (b *PersonBehavior) BeforeUpdate(
	ctx context.Context, _ *entities.Resource, data json.RawMessage, rt *entities.ResourceType,
) (json.RawMessage, error) {
	return injectPersonName(data)
}

func injectPersonName(data json.RawMessage) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid person data: %w", err)
	}
	gn, _ := m["givenName"].(string)
	fn, _ := m["familyName"].(string)
	m["name"] = strings.TrimSpace(gn + " " + fn)
	return json.Marshal(m)
}

// OrganizationBehavior provides custom logic for the "organization" resource type.
type OrganizationBehavior struct {
	entities.DefaultBehavior
}
