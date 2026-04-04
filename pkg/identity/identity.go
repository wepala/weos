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

package identity

import (
	"regexp"
	"strings"

	"github.com/segmentio/ksuid"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts a name into a URL-friendly slug.
// It lowercases the input, replaces non-alphanumeric characters with hyphens,
// collapses consecutive hyphens, and trims leading/trailing hyphens.
func Slugify(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = nonAlphanumeric.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	return slug
}

const (
	TypeWebsite      = "website"
	TypePage         = "page"
	TypeSection      = "section"
	TypeTheme        = "theme"
	TypeTemplate     = "template"
	TypePerson       = "person"
	TypeOrganization = "organization"
	TypeResourceType = "resource-type"
	TypeResource     = "resource"
)

// NewWebsite generates a website URN from a slug.
// Format: "urn:<slug>"
func NewWebsite(slug string) string {
	return "urn:" + slug
}

// NewPage generates a page URN scoped to a website.
// Format: "urn:<websiteSlug>:page:<ksuid>:<pageSlug>"
func NewPage(websiteSlug, pageSlug string) string {
	return "urn:" + websiteSlug + ":page:" + ksuid.New().String() + ":" + pageSlug
}

// NewSection generates a section URN scoped to a page within a website.
// Format: "urn:<websiteSlug>:<pageSlug>:section:<ksuid>"
func NewSection(websiteSlug, pageSlug string) string {
	return "urn:" + websiteSlug + ":" + pageSlug + ":section:" + ksuid.New().String()
}

// NewTheme generates a theme URN from a slug.
// Format: "urn:theme:<slug>"
func NewTheme(slug string) string {
	return "urn:theme:" + slug
}

// NewTemplate generates a template URN scoped to a theme.
// Format: "urn:theme:<themeSlug>:template:<ksuid>:<templateSlug>"
func NewTemplate(themeSlug, templateSlug string) string {
	return "urn:theme:" + themeSlug + ":template:" + ksuid.New().String() + ":" + templateSlug
}

// NewPerson generates a person URN.
// Format: "urn:person:<ksuid>"
func NewPerson() string {
	return "urn:person:" + ksuid.New().String()
}

// NewOrganization generates an organization URN from a slug.
// Format: "urn:org:<slug>"
func NewOrganization(slug string) string {
	return "urn:org:" + slug
}

// NewResourceType generates a resource type URN from a slug.
// Format: "urn:type:<slug>"
func NewResourceType(slug string) string {
	return "urn:type:" + slug
}

// NewResource generates a resource URN scoped to a type slug.
// Format: "urn:<typeSlug>:<ksuid>"
func NewResource(typeSlug string) string {
	return "urn:" + typeSlug + ":" + ksuid.New().String()
}

// ExtractThemeSlug returns the theme slug from a theme or template URN.
// Theme URN (urn:theme:<slug>) → parts[2]
// Template URN (urn:theme:<ts>:template:<ksuid>:<tps>) → parts[2]
// Otherwise → ""
func ExtractThemeSlug(id string) string {
	if !strings.HasPrefix(id, "urn:theme:") {
		return ""
	}
	parts := strings.Split(id, ":")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// ExtractEntityType returns the entity type from a URN.
// 2-part (urn:<slug>) → "website"
// 5-part with parts[2]=="page" → "page"
// 5-part with parts[3]=="section" → "section"
// Non-URN strings return "".
func ExtractEntityType(id string) string {
	if !strings.HasPrefix(id, "urn:") {
		return ""
	}
	parts := strings.Split(id, ":")
	switch len(parts) {
	case 2:
		return TypeWebsite
	case 3:
		switch parts[1] {
		case "theme":
			return TypeTheme
		case "person":
			return TypePerson
		case "org":
			return TypeOrganization
		case "type":
			return TypeResourceType
		}
	case 5:
		if parts[2] == "page" {
			return TypePage
		}
		if parts[3] == "section" {
			return TypeSection
		}
	case 6:
		if parts[3] == "template" {
			return TypeTemplate
		}
	}
	return ""
}

// ExtractKSUID returns the KSUID portion of an entity ID.
// 2-part (website) → ""
// 5-part page (urn:<ws>:page:<ksuid>:<ps>) → parts[3]
// 5-part section (urn:<ws>:<ps>:section:<ksuid>) → parts[4]
// Non-URN strings are returned as-is.
func ExtractKSUID(id string) string {
	if !strings.HasPrefix(id, "urn:") {
		return id
	}
	parts := strings.Split(id, ":")
	switch len(parts) {
	case 2:
		return ""
	case 3:
		if parts[1] == "person" {
			return parts[2]
		}
		return ""
	case 5:
		if parts[2] == "page" {
			return parts[3]
		}
		if parts[3] == "section" {
			return parts[4]
		}
	case 6:
		if parts[3] == "template" {
			return parts[4]
		}
	}
	return id
}

// ExtractWebsiteSlug returns the website slug from any URN.
// Returns parts[1] for any valid URN, "" for non-URN.
func ExtractWebsiteSlug(id string) string {
	if !strings.HasPrefix(id, "urn:") {
		return ""
	}
	parts := strings.Split(id, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// ExtractResourceTypeSlug returns the type slug from a resource URN.
// Resource URN format: "urn:<typeSlug>:<ksuid>" → returns typeSlug.
// Only matches 3-part URNs that don't use reserved prefixes.
func ExtractResourceTypeSlug(id string) string {
	if !strings.HasPrefix(id, "urn:") {
		return ""
	}
	parts := strings.Split(id, ":")
	if len(parts) == 3 && parts[0] == "urn" {
		// Exclude known 3-part prefixes (person, org, theme, type)
		switch parts[1] {
		case "person", "org", "theme", "type":
			return ""
		}
		return parts[1]
	}
	return ""
}

// ExtractPageSlug returns the page slug from a URN.
// 5-part page (urn:<ws>:page:<ksuid>:<ps>) → parts[4]
// 5-part section (urn:<ws>:<ps>:section:<ksuid>) → parts[2]
// Otherwise → ""
func ExtractPageSlug(id string) string {
	if !strings.HasPrefix(id, "urn:") {
		return ""
	}
	parts := strings.Split(id, ":")
	if len(parts) == 5 {
		if parts[2] == "page" {
			return parts[4]
		}
		if parts[3] == "section" {
			return parts[2]
		}
	}
	return ""
}
