// Package website provides resource types for website structure and content.
package website

import "github.com/wepala/weos/v3/application"

// Register adds the website preset to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "website",
		Description: "Standard website types: site structure, pages, templates, and content",
		Types: []application.PresetResourceType{
			application.NewPresetType("Web Site", "web-site",
				"Root website entity with name, URL, and language",
				`{"@vocab":"https://schema.org/","@type":"WebSite"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"url":{"type":"string","format":"uri"},`+
					`"description":{"type":"string"},"inLanguage":{"type":"string"}},"required":["name"]}`,
			),
			application.NewPresetType("Web Page", "web-page",
				"Individual page with name, slug, description, and template reference",
				`{"@vocab":"https://schema.org/","@type":"WebPage"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},`+
					`"description":{"type":"string"},"template":{"type":"string"}},"required":["name"]}`,
			),
			application.NewPresetType("Web Page Element", "web-page-element",
				"Content section or block within a page",
				`{"@vocab":"https://schema.org/","@type":"WebPageElement"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"cssSelector":{"type":"string"},`+
					`"content":{"type":"string"}},"required":["name"]}`,
			),
			application.NewPresetType("Web Page Template", "web-page-template",
				"HTML page template defining layout and slots",
				`{"@vocab":"https://schema.org/","@type":"WebPage","variant":"template"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"templateBody":{"type":"string"},`+
					`"slots":{"type":"array","items":{"type":"string"}}},"required":["name"]}`,
			),
			application.NewPresetType("Theme", "theme",
				"Visual theme or skin with version and thumbnail",
				`{"@vocab":"https://schema.org/","@type":"CreativeWork"}`,
				`{"type":"object","properties":{"name":{"type":"string"},"version":{"type":"string"},`+
					`"thumbnailUrl":{"type":"string","format":"uri"}},"required":["name"]}`,
			),
			application.NewPresetType("Article", "article",
				"Written composition such as a news or feature article",
				`{"@vocab":"https://schema.org/","@type":"Article"}`,
				`{"type":"object","properties":{"headline":{"type":"string"},"articleBody":{"type":"string"},`+
					`"author":{"type":"string"},"datePublished":{"type":"string","format":"date-time"}},`+
					`"required":["headline"]}`,
			),
			application.NewPresetType("Blog Post", "blog-post",
				"Blog entry, informal tone, reverse-chronological listing",
				`{"@vocab":"https://schema.org/","@type":"BlogPosting"}`,
				`{"type":"object","properties":{"headline":{"type":"string"},"articleBody":{"type":"string"},`+
					`"author":{"type":"string"},"datePublished":{"type":"string","format":"date-time"}},`+
					`"required":["headline"]}`,
			),
			application.NewPresetType("FAQ", "faq",
				"Frequently asked questions page with question-answer pairs",
				`{"@vocab":"https://schema.org/","@type":"FAQPage"}`,
				`{"type":"object","properties":{"name":{"type":"string"},`+
					`"mainEntity":{"type":"array","items":{"type":"object","properties":{`+
					`"name":{"type":"string"},"acceptedAnswer":{"type":"string"}}}}},"required":["name"]}`,
			),
			application.NewPresetType("Breadcrumb List", "breadcrumb-list",
				"Navigation trail from homepage to current page",
				`{"@vocab":"https://schema.org/","@type":"BreadcrumbList"}`,
				`{"type":"object","properties":{"name":{"type":"string"},`+
					`"itemListElement":{"type":"array","items":{"type":"object","properties":{`+
					`"name":{"type":"string"},"item":{"type":"string","format":"uri"},`+
					`"position":{"type":"integer"}}}}},"required":["name"]}`,
			),
		},
	})
}
