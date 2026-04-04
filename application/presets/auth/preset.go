// Package auth provides resource types for authentication and authorization,
// wrapping the pericarp auth concepts (User, Role, Account) as WeOS resource types.
package auth

import "weos/application"

// Register adds the auth preset to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "auth",
		Description: "Authentication and authorization types: users, roles, and accounts",
		Types: []application.PresetResourceType{
			application.NewPresetType("User", "user",
				"A user account in the system",
				`{"@vocab": "https://schema.org/", "foaf": "http://xmlns.com/foaf/0.1/", "@type": "Person"}`,
				`{
					"type": "object",
					"properties": {
						"name":      {"type": "string"},
						"email":     {"type": "string", "format": "email"},
						"avatarURL": {"type": "string", "format": "uri"},
						"status":    {"type": "string", "enum": ["active", "inactive", "suspended"]}
					},
					"required": ["email"]
				}`,
			),
			application.NewPresetType("Role", "role",
				"A role that grants permissions to users",
				`{"@vocab": "https://schema.org/", "@type": "Role"}`,
				`{
					"type": "object",
					"properties": {
						"name":        {"type": "string"},
						"description": {"type": "string"},
						"roleName":    {"type": "string"}
					},
					"required": ["name", "roleName"]
				}`,
			),
			application.NewPresetType("Account", "account",
				"An organizational account or tenant",
				`{"@vocab": "https://schema.org/", "org": "http://www.w3.org/ns/org#", "@type": "Organization"}`,
				`{
					"type": "object",
					"properties": {
						"name":        {"type": "string"},
						"slug":        {"type": "string"},
						"description": {"type": "string"}
					},
					"required": ["name", "slug"]
				}`,
			),
		},
		AutoInstall: true,
	})
}
