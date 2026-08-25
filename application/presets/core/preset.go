// Package core provides the built-in Person and Organization resource types
// with their associated behaviors. These are auto-created at startup.
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

// Register adds the core preset (Person, Organization) to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "core",
		Description: "Core types: Person and Organization with computed name behaviors",
		Types: []application.PresetResourceType{
			// The class is foaf:Person (issue #521, decided 2026-08-25). The
			// foaf prefix is declared on this context for exactly this: FOAF is
			// the standard vocabulary for the concept of a person, and the
			// prefix was decorative until the class used it. schema.org stays
			// the @vocab for the predicates (givenName, familyName, email…).
			// Before the declaration a person's entity @type fell back to the
			// type NAME and expanded to https://schema.org/Person, so an
			// existing install must adopt the held @type and re-stamp its
			// records to move — see docs/_howto/normalize-edge-keys.md.
			application.NewPresetType("Person", "person",
				"A person (foaf:Person)",
				`{"@vocab": "https://schema.org/", "foaf": "http://xmlns.com/foaf/0.1/", "@type": "foaf:Person"}`,
				`{
					"type": "object",
					"properties": {
						"givenName":  {"type": "string"},
						"familyName": {"type": "string"},
						"name":       {"type": "string"},
						"email":      {"type": "string"},
						"avatarURL":  {"type": "string"}
					},
					"required": ["givenName", "familyName"]
				}`,
			),
			// The class is org:Organization (issue #521): the W3C Organization
			// Ontology's class, through the org prefix this context already
			// declares. Same reasoning and same migration as Person above.
			application.NewPresetType("Organization", "organization",
				"An organization (org:Organization)",
				`{"@vocab": "https://schema.org/", "org": "http://www.w3.org/ns/org#", "@type": "org:Organization"}`,
				`{
					"type": "object",
					"properties": {
						"name":        {"type": "string"},
						"slug":        {"type": "string"},
						"description": {"type": "string"},
						"url":         {"type": "string"},
						"logoURL":     {"type": "string"}
					},
					"required": ["name", "slug"]
				}`,
			),
		},
		Behaviors: map[string]application.BehaviorFactory{
			"person":       application.StaticBehavior(&personBehavior{}),
			"organization": application.StaticBehavior(&organizationBehavior{}),
		},
		BehaviorMeta: map[string]entities.BehaviorMeta{
			"person": {
				Slug:        "person",
				DisplayName: "Computed Name",
				Description: "Auto-computes full name from givenName + familyName",
				Default:     true,
				Manageable:  false,
			},
			"organization": {
				Slug:        "organization",
				DisplayName: "Organization Defaults",
				Description: "Placeholder for organization-specific logic",
				Default:     true,
				Manageable:  false,
			},
		},
		AutoInstall: true,
	})
}

// personBehavior computes a full "name" field from givenName and familyName.
type personBehavior struct {
	entities.DefaultBehavior
}

func (b *personBehavior) BeforeCreate(
	ctx context.Context, data json.RawMessage, rt *entities.ResourceType,
) (json.RawMessage, error) {
	return injectPersonName(data)
}

func (b *personBehavior) BeforeUpdate(
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

// organizationBehavior is a no-op placeholder ensuring the organization type is
// represented in the behavior registry. Custom logic can be added here later.
type organizationBehavior struct {
	entities.DefaultBehavior
}
