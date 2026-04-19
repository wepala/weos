// Package events provides resource types for event management.
package events

import "github.com/wepala/weos/v3/application"

// Register adds the events preset to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "events",
		Description: "Event types: events, places, and venues",
		Types: []application.PresetResourceType{
			application.NewPresetType("Event", "event",
				"An event happening at a certain time and place",
				`{"@vocab":"https://schema.org/","@type":"Event"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},`+
					`"startDate":{"type":"string","format":"date-time"},`+
					`"endDate":{"type":"string","format":"date-time"},`+
					`"location":{"type":"string"}},"required":["name"]}`,
			),
			application.NewPresetType("Place", "place",
				"A physical location or area",
				`{"@vocab":"https://schema.org/","@type":"Place"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"address":{"type":"string"},`+
					`"geo":{"type":"object","properties":{"latitude":{"type":"number"},`+
					`"longitude":{"type":"number"}}}},"required":["name"]}`,
			),
			application.NewPresetType("Venue", "venue",
				"An event venue with capacity and amenity details",
				`{"@vocab":"https://schema.org/","@type":"EventVenue"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"address":{"type":"string"},`+
					`"maximumAttendeeCapacity":{"type":"integer"}},"required":["name"]}`,
			),
		},
	})
}
