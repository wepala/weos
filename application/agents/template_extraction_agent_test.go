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

package agents

import (
	"testing"
)

func TestParseResponse_ValidJSON(t *testing.T) {
	input := `{
		"page_type": "homepage",
		"description": "A modern business homepage template",
		"sections": [
			{
				"name": "Hero",
				"slot": "hero",
				"html_selector": "section.hero",
				"semantic_type": "",
				"position": 0,
				"content_slots": [
					{
						"name": "Headline",
						"slot": "hero.headline",
						"html_selector": "h1",
						"content_type": "text",
						"required": true
					},
					{
						"name": "Subheadline",
						"slot": "hero.subheadline",
						"html_selector": "p.lead",
						"content_type": "text",
						"required": false
					}
				]
			},
			{
				"name": "Services",
				"slot": "services",
				"html_selector": "#services",
				"semantic_type": "Organization",
				"position": 1,
				"content_slots": [
					{
						"name": "Title",
						"slot": "services.title",
						"html_selector": "h2",
						"content_type": "text",
						"required": true
					}
				]
			}
		],
		"navigation": {
			"items": [
				{"label": "Home", "href": "#"},
				{"label": "About", "href": "#about"},
				{"label": "Contact", "href": "#contact"}
			]
		}
	}`

	result, err := parseResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.PageType != "homepage" {
		t.Errorf("page_type = %q, want %q", result.PageType, "homepage")
	}
	if result.Description != "A modern business homepage template" {
		t.Errorf("description = %q, want %q", result.Description, "A modern business homepage template")
	}
	if len(result.Sections) != 2 {
		t.Fatalf("sections count = %d, want 2", len(result.Sections))
	}

	hero := result.Sections[0]
	if hero.Name != "Hero" {
		t.Errorf("sections[0].name = %q, want %q", hero.Name, "Hero")
	}
	if hero.Slot != "hero" {
		t.Errorf("sections[0].slot = %q, want %q", hero.Slot, "hero")
	}
	if hero.Position != 0 {
		t.Errorf("sections[0].position = %d, want 0", hero.Position)
	}
	if len(hero.ContentSlots) != 2 {
		t.Fatalf("sections[0].content_slots count = %d, want 2", len(hero.ContentSlots))
	}
	if hero.ContentSlots[0].Slot != "hero.headline" {
		t.Errorf("content_slots[0].slot = %q, want %q", hero.ContentSlots[0].Slot, "hero.headline")
	}
	if !hero.ContentSlots[0].Required {
		t.Error("content_slots[0].required = false, want true")
	}

	services := result.Sections[1]
	if services.SemanticType != "Organization" {
		t.Errorf("sections[1].semantic_type = %q, want %q", services.SemanticType, "Organization")
	}

	if len(result.Navigation.Items) != 3 {
		t.Fatalf("navigation items count = %d, want 3", len(result.Navigation.Items))
	}
	if result.Navigation.Items[1].Label != "About" {
		t.Errorf("nav[1].label = %q, want %q", result.Navigation.Items[1].Label, "About")
	}
}

func TestParseResponse_MarkdownCodeBlock(t *testing.T) {
	input := "```json\n{\"page_type\": \"contact\", \"description\": \"Contact page\", \"sections\": []}\n```"

	result, err := parseResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PageType != "contact" {
		t.Errorf("page_type = %q, want %q", result.PageType, "contact")
	}
}

func TestParseResponse_ExtraTextAroundJSON(t *testing.T) {
	input := `Here is the analysis:
{"page_type": "about", "description": "About page", "sections": [], "navigation": {"items": []}}
That's the result.`

	result, err := parseResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PageType != "about" {
		t.Errorf("page_type = %q, want %q", result.PageType, "about")
	}
}

func TestParseResponse_NoJSON(t *testing.T) {
	_, err := parseResponse("no json here")
	if err == nil {
		t.Fatal("expected error for response with no JSON")
	}
}

func TestParseResponse_MalformedJSON(t *testing.T) {
	_, err := parseResponse("{invalid json}")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParseResponse_EmptyInput(t *testing.T) {
	_, err := parseResponse("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestParseResponse_EmptySections(t *testing.T) {
	input := `{"page_type": "landing", "description": "Simple landing page", "sections": []}`

	result, err := parseResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PageType != "landing" {
		t.Errorf("page_type = %q, want %q", result.PageType, "landing")
	}
	if len(result.Sections) != 0 {
		t.Errorf("sections count = %d, want 0", len(result.Sections))
	}
}

func TestToEntitySections(t *testing.T) {
	analysis := &TemplateAnalysis{
		Sections: []SectionAnalysis{
			{
				Name:         "Hero",
				Slot:         "hero",
				HTMLSelector: "section.hero",
				SemanticType: "WebPage",
				Position:     0,
				ContentSlots: []ContentSlot{
					{Name: "Headline", Slot: "hero.headline", ContentType: "text", Required: true},
				},
			},
		},
		Navigation: NavigationAnalysis{
			Items: []NavItem{{Label: "Home", Href: "/"}},
		},
	}

	sections := analysis.ToEntitySections()
	if len(sections) != 1 {
		t.Fatalf("entity sections count = %d, want 1", len(sections))
	}
	if sections[0].Name != "Hero" {
		t.Errorf("section name = %q, want %q", sections[0].Name, "Hero")
	}
	if len(sections[0].ContentSlots) != 1 {
		t.Fatalf("content slots count = %d, want 1", len(sections[0].ContentSlots))
	}
	if sections[0].ContentSlots[0].Slot != "hero.headline" {
		t.Errorf("slot = %q, want %q", sections[0].ContentSlots[0].Slot, "hero.headline")
	}

	navItems := analysis.ToEntityNavItems()
	if len(navItems) != 1 {
		t.Fatalf("nav items count = %d, want 1", len(navItems))
	}
	if navItems[0].Label != "Home" {
		t.Errorf("nav label = %q, want %q", navItems[0].Label, "Home")
	}
}
