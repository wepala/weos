// Package knowledge provides SKOS-based resource types for knowledge organization.
package knowledge

import "github.com/wepala/weos/v3/application"

// Register adds the knowledge preset to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "knowledge",
		Description: "Knowledge organization types: SKOS concepts, schemes, and collections",
		Types: []application.PresetResourceType{
			application.NewPresetType("Concept", "concept",
				"A SKOS concept — an idea or notion in a knowledge domain",
				`{"@vocab":"http://www.w3.org/2004/02/skos/core#","@type":"Concept"}`,
				`{"type":"object","properties":{"prefLabel":{"type":"string"},`+
					`"altLabel":{"type":"array","items":{"type":"string"}},`+
					`"definition":{"type":"string"}},"required":["prefLabel"]}`,
			),
			application.NewPresetType("Concept Scheme", "concept-scheme",
				"A SKOS concept scheme — a set of concepts and their relationships",
				`{"@vocab":"http://www.w3.org/2004/02/skos/core#","@type":"ConceptScheme"}`,
				`{"type":"object","properties":{"title":{"type":"string"},`+
					`"description":{"type":"string"}},"required":["title"]}`,
			),
			application.NewPresetType("Collection", "collection",
				"A SKOS collection — a labeled group of concepts",
				`{"@vocab":"http://www.w3.org/2004/02/skos/core#","@type":"Collection"}`,
				`{"type":"object","properties":{"prefLabel":{"type":"string"},`+
					`"member":{"type":"array","items":{"type":"string"}}},"required":["prefLabel"]}`,
			),
		},
	})
}
