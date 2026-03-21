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

package application

import (
	"encoding/json"
	"sort"
)

// PresetResourceType defines a single resource type within a preset group.
type PresetResourceType struct {
	Name        string
	Slug        string
	Description string
	Context     json.RawMessage
	Schema      json.RawMessage
}

// PresetDefinition defines a named group of resource types that can be installed together.
type PresetDefinition struct {
	Name        string
	Description string
	Types       []PresetResourceType
}

// InstallPresetResult reports which types were created, updated, or skipped during installation.
type InstallPresetResult struct {
	Created []string `json:"created"`
	Updated []string `json:"updated,omitempty"`
	Skipped []string `json:"skipped"`
}

func newPresetType(name, slug, desc, ctx, schema string) PresetResourceType {
	pt := PresetResourceType{
		Name:        name,
		Slug:        slug,
		Description: desc,
	}
	if ctx != "" {
		pt.Context = json.RawMessage(ctx)
	}
	if schema != "" {
		pt.Schema = json.RawMessage(schema)
	}
	return pt
}

var presets = map[string]PresetDefinition{
	"website":   websitePreset(),
	"ecommerce": ecommercePreset(),
	"events":    eventsPreset(),
	"knowledge": knowledgePreset(),
}

// ListPresetDefinitions returns all available preset definitions sorted by name.
func ListPresetDefinitions() []PresetDefinition {
	defs := make([]PresetDefinition, 0, len(presets))
	for _, d := range presets {
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

// GetPresetDefinition returns the preset with the given name, or false if not found.
func GetPresetDefinition(name string) (PresetDefinition, bool) {
	d, ok := presets[name]
	return d, ok
}

func websitePreset() PresetDefinition {
	return PresetDefinition{
		Name:        "website",
		Description: "Standard website types: site structure, pages, templates, and content",
		Types: []PresetResourceType{
			newPresetType("Web Site", "web-site",
				"Root website entity with name, URL, and language",
				`{"@vocab":"https://schema.org/","@type":"WebSite"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"url":{"type":"string","format":"uri"},`+
					`"description":{"type":"string"},"inLanguage":{"type":"string"}},"required":["name"]}`,
			),
			newPresetType("Web Page", "web-page",
				"Individual page with name, slug, description, and template reference",
				`{"@vocab":"https://schema.org/","@type":"WebPage"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},`+
					`"description":{"type":"string"},"template":{"type":"string"}},"required":["name"]}`,
			),
			newPresetType("Web Page Element", "web-page-element",
				"Content section or block within a page",
				`{"@vocab":"https://schema.org/","@type":"WebPageElement"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"cssSelector":{"type":"string"},`+
					`"content":{"type":"string"}},"required":["name"]}`,
			),
			webPageTemplatePreset(),
			themePresetType(),
			articlePresetType(),
			blogPostPresetType(),
			faqPresetType(),
			breadcrumbListPresetType(),
		},
	}
}

func webPageTemplatePreset() PresetResourceType {
	return newPresetType("Web Page Template", "web-page-template",
		"HTML page template defining layout and slots",
		`{"@vocab":"https://schema.org/","@type":"WebPage","variant":"template"}`,
		`{"type":"object","properties":{"name":{"type":"string"},"templateBody":{"type":"string"},`+
			`"slots":{"type":"array","items":{"type":"string"}}},"required":["name"]}`,
	)
}

func themePresetType() PresetResourceType {
	return newPresetType("Theme", "theme",
		"Visual theme or skin with version and thumbnail",
		`{"@vocab":"https://schema.org/","@type":"CreativeWork"}`,
		`{"type":"object","properties":{"name":{"type":"string"},"version":{"type":"string"},`+
			`"thumbnailUrl":{"type":"string","format":"uri"}},"required":["name"]}`,
	)
}

func articlePresetType() PresetResourceType {
	return newPresetType("Article", "article",
		"Written composition such as a news or feature article",
		`{"@vocab":"https://schema.org/","@type":"Article"}`,
		`{"type":"object","properties":{"headline":{"type":"string"},"articleBody":{"type":"string"},`+
			`"author":{"type":"string"},"datePublished":{"type":"string","format":"date-time"}},`+
			`"required":["headline"]}`,
	)
}

func blogPostPresetType() PresetResourceType {
	return newPresetType("Blog Post", "blog-post",
		"Blog entry, informal tone, reverse-chronological listing",
		`{"@vocab":"https://schema.org/","@type":"BlogPosting"}`,
		`{"type":"object","properties":{"headline":{"type":"string"},"articleBody":{"type":"string"},`+
			`"author":{"type":"string"},"datePublished":{"type":"string","format":"date-time"}},`+
			`"required":["headline"]}`,
	)
}

func faqPresetType() PresetResourceType {
	return newPresetType("FAQ", "faq",
		"Frequently asked questions page with question-answer pairs",
		`{"@vocab":"https://schema.org/","@type":"FAQPage"}`,
		`{"type":"object","properties":{"name":{"type":"string"},`+
			`"mainEntity":{"type":"array","items":{"type":"object","properties":{`+
			`"name":{"type":"string"},"acceptedAnswer":{"type":"string"}}}}},"required":["name"]}`,
	)
}

func breadcrumbListPresetType() PresetResourceType {
	return newPresetType("Breadcrumb List", "breadcrumb-list",
		"Navigation trail from homepage to current page",
		`{"@vocab":"https://schema.org/","@type":"BreadcrumbList"}`,
		`{"type":"object","properties":{"name":{"type":"string"},`+
			`"itemListElement":{"type":"array","items":{"type":"object","properties":{`+
			`"name":{"type":"string"},"item":{"type":"string","format":"uri"},`+
			`"position":{"type":"integer"}}}}},"required":["name"]}`,
	)
}

func ecommercePreset() PresetDefinition {
	return PresetDefinition{
		Name:        "ecommerce",
		Description: "E-commerce types: products, offers, reviews, and services",
		Types: []PresetResourceType{
			newPresetType("Product", "product",
				"A product offered for sale",
				`{"@vocab":"https://schema.org/","@type":"Product"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},`+
					`"sku":{"type":"string"},"brand":{"type":"string"},`+
					`"image":{"type":"string","format":"uri"}},"required":["name"]}`,
			),
			newPresetType("Offer", "offer",
				"A price or availability offer for a product or service",
				`{"@vocab":"https://schema.org/","gr":"http://purl.org/goodrelations/v1#","@type":"Offer"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"price":{"type":"number"},`+
					`"priceCurrency":{"type":"string"},"availability":{"type":"string"}},`+
					`"required":["name","price"]}`,
			),
			newPresetType("Review", "review",
				"A user review or rating for a product or service",
				`{"@vocab":"https://schema.org/","@type":"Review"}`,
				`{"type":"object","properties":{"name":{"type":"string"},`+
					`"reviewBody":{"type":"string"},"reviewRating":{"type":"object","properties":{`+
					`"ratingValue":{"type":"number"},"bestRating":{"type":"number"}}},`+
					`"author":{"type":"string"}},"required":["name"]}`,
			),
			newPresetType("Service", "service",
				"A service offered by an organization or individual",
				`{"@vocab":"https://schema.org/","@type":"Service"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},`+
					`"provider":{"type":"string"},"serviceType":{"type":"string"}},"required":["name"]}`,
			),
		},
	}
}

func eventsPreset() PresetDefinition {
	return PresetDefinition{
		Name:        "events",
		Description: "Event types: events, places, and venues",
		Types: []PresetResourceType{
			newPresetType("Event", "event",
				"An event happening at a certain time and place",
				`{"@vocab":"https://schema.org/","@type":"Event"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},`+
					`"startDate":{"type":"string","format":"date-time"},`+
					`"endDate":{"type":"string","format":"date-time"},`+
					`"location":{"type":"string"}},"required":["name"]}`,
			),
			newPresetType("Place", "place",
				"A physical location or area",
				`{"@vocab":"https://schema.org/","@type":"Place"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"address":{"type":"string"},`+
					`"geo":{"type":"object","properties":{"latitude":{"type":"number"},`+
					`"longitude":{"type":"number"}}}},"required":["name"]}`,
			),
			newPresetType("Venue", "venue",
				"An event venue with capacity and amenity details",
				`{"@vocab":"https://schema.org/","@type":"EventVenue"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"address":{"type":"string"},`+
					`"maximumAttendeeCapacity":{"type":"integer"}},"required":["name"]}`,
			),
		},
	}
}

func knowledgePreset() PresetDefinition {
	return PresetDefinition{
		Name:        "knowledge",
		Description: "Knowledge organization types: SKOS concepts, schemes, and collections",
		Types: []PresetResourceType{
			newPresetType("Concept", "concept",
				"A SKOS concept — an idea or notion in a knowledge domain",
				`{"@vocab":"http://www.w3.org/2004/02/skos/core#","@type":"Concept"}`,
				`{"type":"object","properties":{"prefLabel":{"type":"string"},`+
					`"altLabel":{"type":"array","items":{"type":"string"}},`+
					`"definition":{"type":"string"}},"required":["prefLabel"]}`,
			),
			newPresetType("Concept Scheme", "concept-scheme",
				"A SKOS concept scheme — a set of concepts and their relationships",
				`{"@vocab":"http://www.w3.org/2004/02/skos/core#","@type":"ConceptScheme"}`,
				`{"type":"object","properties":{"title":{"type":"string"},`+
					`"description":{"type":"string"}},"required":["title"]}`,
			),
			newPresetType("Collection", "collection",
				"A SKOS collection — a labeled group of concepts",
				`{"@vocab":"http://www.w3.org/2004/02/skos/core#","@type":"Collection"}`,
				`{"type":"object","properties":{"prefLabel":{"type":"string"},`+
					`"member":{"type":"array","items":{"type":"string"}}},"required":["prefLabel"]}`,
			),
		},
	}
}
