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

package entities

// ThemeManifest represents the theme.json manifest file inside a theme zip.
type ThemeManifest struct {
	Name        string             `json:"name"`
	Slug        string             `json:"slug"`
	Version     string             `json:"version"`
	Description string             `json:"description"`
	Author      string             `json:"author"`
	Templates   []TemplateManifest `json:"templates"`
	Source      string             `json:"-"`
}

// TemplateManifest describes a single template within the theme manifest.
type TemplateManifest struct {
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	File        string            `json:"file"`
	PageType    string            `json:"page_type,omitempty"`
	Description string            `json:"description,omitempty"`
	Sections    []TemplateSection `json:"sections,omitempty"`
	Navigation  []TemplateNavItem `json:"navigation,omitempty"`
}

// TemplateSection describes a structural section within a template.
type TemplateSection struct {
	Name         string                `json:"name"`
	Slot         string                `json:"slot"`
	HTMLSelector string                `json:"html_selector,omitempty"`
	SemanticType string                `json:"semantic_type,omitempty"`
	Position     int                   `json:"position"`
	ContentSlots []TemplateContentSlot `json:"content_slots,omitempty"`
}

// TemplateContentSlot describes an editable content area within a section.
type TemplateContentSlot struct {
	Name         string `json:"name"`
	Slot         string `json:"slot"`
	ContentType  string `json:"content_type"`
	HTMLSelector string `json:"html_selector,omitempty"`
	Required     bool   `json:"required"`
}

// TemplateNavItem represents a navigation link extracted from a template.
type TemplateNavItem struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}
