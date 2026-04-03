package application

import (
	"context"
	"encoding/json"

	"weos/domain/entities"

	"go.uber.org/fx"
)

// builtInResourceTypes defines core resource types that are always available.
// These are created at startup if they don't already exist.
var builtInResourceTypes = []PresetResourceType{
	newPresetType("Person", "person",
		"A person (foaf:Person / schema:Person)",
		`{"@vocab": "https://schema.org/", "foaf": "http://xmlns.com/foaf/0.1/"}`,
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
	newPresetType("Organization", "organization",
		"An organization (org:Organization / schema:Organization)",
		`{"@vocab": "https://schema.org/", "org": "http://www.w3.org/ns/org#"}`,
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
}

// ensureBuiltInResourceTypes creates the core resource types at startup
// if they don't already exist.
func ensureBuiltInResourceTypes(params struct {
	fx.In
	TypeSvc ResourceTypeService
	Logger  entities.Logger
}) error {
	ctx := context.Background()
	for _, bt := range builtInResourceTypes {
		if _, err := params.TypeSvc.GetBySlug(ctx, bt.Slug); err == nil {
			continue // already exists
		}
		cmd := CreateResourceTypeCommand{
			Name:        bt.Name,
			Slug:        bt.Slug,
			Description: bt.Description,
			Context:     bt.Context,
			Schema:      bt.Schema,
		}
		if _, err := params.TypeSvc.Create(ctx, cmd); err != nil {
			params.Logger.Error(ctx, "failed to create built-in resource type",
				"slug", bt.Slug, "error", err)
		} else {
			params.Logger.Info(ctx, "created built-in resource type", "slug", bt.Slug)
		}
	}
	return nil
}

// PersonSchema returns the JSON schema for the built-in person resource type.
func PersonSchema() json.RawMessage {
	return builtInResourceTypes[0].Schema
}

// OrganizationSchema returns the JSON schema for the built-in organization resource type.
func OrganizationSchema() json.RawMessage {
	return builtInResourceTypes[1].Schema
}
