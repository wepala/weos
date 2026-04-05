// Package ecommerce provides resource types for e-commerce: products, offers, reviews, and services.
package ecommerce

import "weos/application"

// Register adds the ecommerce preset to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "ecommerce",
		Description: "E-commerce types: products, offers, reviews, and services",
		Types: []application.PresetResourceType{
			application.NewPresetType("Product", "product",
				"A product offered for sale",
				`{"@vocab":"https://schema.org/","@type":"Product"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},`+
					`"sku":{"type":"string"},"brand":{"type":"string"},`+
					`"image":{"type":"string","format":"uri"}},"required":["name"]}`,
			),
			application.NewPresetType("Offer", "offer",
				"A price or availability offer for a product or service",
				`{"@vocab":"https://schema.org/","gr":"http://purl.org/goodrelations/v1#","@type":"Offer"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"price":{"type":"number"},`+
					`"priceCurrency":{"type":"string"},"availability":{"type":"string"}},`+
					`"required":["name","price"]}`,
			),
			application.NewPresetType("Review", "review",
				"A user review or rating for a product or service",
				`{"@vocab":"https://schema.org/","@type":"Review"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"reviewBody":{"type":"string"},`+
					`"reviewRating":{"type":"integer"},"author":{"type":"string"}},`+
					`"required":["name"]}`,
			),
			application.NewPresetType("Service", "service",
				"A service offered by an organization or individual",
				`{"@vocab":"https://schema.org/","@type":"Service"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},`+
					`"provider":{"type":"string"},"serviceType":{"type":"string"}},`+
					`"required":["name"]}`,
			),
		},
	})
}
