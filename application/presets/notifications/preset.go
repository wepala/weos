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

// Package notifications provides the generic notification resource type that
// backs the inbox capability. Any service can produce notifications addressed
// to a user through application.NotificationService; this preset only defines
// the store. It is AutoInstall (like core): the inbox is a baseline capability
// every weos service gets, so the type is created at startup with no opt-in.
package notifications

import (
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// Slug is the resource type slug for a notification, aliasing the canonical
// value the application package owns (avoids drift between the type this preset
// installs and the type the notification service reads/writes).
const Slug = application.NotificationTypeSlug

// Register adds the notification preset to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "notifications",
		Description: "Generic notification store backing the per-user inbox",
		// Always-on: the inbox is a baseline capability, so the notification
		// type is created at startup for every weos service (no opt-in).
		AutoInstall: true,
		Types: []application.PresetResourceType{
			application.NewPresetType("Notification", Slug,
				"A notification addressed to a recipient, shown in their inbox",
				// schema:Message is the closest ontology fit for an inbox item.
				// No x-resource-type properties: taskRef stays a plain string so
				// the store never couples to any particular consumer's types.
				`{"@vocab":"https://schema.org/","@type":"Message",`+
					`"notif":"`+jsonld.NotificationsVocab+`",`+
					// title and body are the notification's label and textual
					// content. Message IS a CreativeWork, so schema:name and
					// schema:text are the published terms for exactly this;
					// schema:title is published for JobPosting.
					`"title":"https://schema.org/name","body":"https://schema.org/text",`+
					// The rest are house concepts schema.org never named.
					`"kind":"notif:kind","actionUrl":"notif:actionUrl",`+
					`"actionLabel":"notif:actionLabel","taskRef":"notif:taskRef",`+
					`"occurredAt":"notif:occurredAt","read":"notif:read",`+
					`"dedupeKey":"notif:dedupeKey"}`,
				`{
					"type": "object",
					"properties": {
						"recipient":   {"type": "string"},
						"kind":        {"type": "string"},
						"title":       {"type": "string"},
						"body":        {"type": "string"},
						"actionUrl":   {"type": "string"},
						"actionLabel": {"type": "string"},
						"taskRef":     {"type": "string"},
						"occurredAt":  {"type": "string", "format": "date-time"},
						"read":        {"type": "boolean"},
						"dedupeKey":   {"type": "string"}
					},
					"required": ["recipient", "title", "occurredAt", "read"]
				}`,
			),
		},
	})
}
