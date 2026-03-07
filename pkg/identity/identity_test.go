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
	"strings"
	"testing"
)

func TestNewWebsite(t *testing.T) {
	t.Parallel()
	id := NewWebsite("ak33m")
	if id != "urn:ak33m" {
		t.Fatalf("NewWebsite(\"ak33m\") = %q, want %q", id, "urn:ak33m")
	}
}

func TestNewPage(t *testing.T) {
	t.Parallel()
	id := NewPage("ak33m", "about-us")
	if !strings.HasPrefix(id, "urn:ak33m:page:") {
		t.Fatalf("NewPage produced %q, want prefix %q", id, "urn:ak33m:page:")
	}
	parts := strings.Split(id, ":")
	if len(parts) != 5 {
		t.Fatalf("NewPage produced %d parts, want 5", len(parts))
	}
	if parts[4] != "about-us" {
		t.Fatalf("page slug = %q, want %q", parts[4], "about-us")
	}
}

func TestNewPage_Uniqueness(t *testing.T) {
	t.Parallel()
	id1 := NewPage("ak33m", "home")
	id2 := NewPage("ak33m", "home")
	if id1 == id2 {
		t.Fatalf("expected unique IDs, got %q twice", id1)
	}
}

func TestNewSection(t *testing.T) {
	t.Parallel()
	id := NewSection("ak33m", "about-us")
	if !strings.HasPrefix(id, "urn:ak33m:about-us:section:") {
		t.Fatalf("NewSection produced %q, want prefix %q", id, "urn:ak33m:about-us:section:")
	}
	parts := strings.Split(id, ":")
	if len(parts) != 5 {
		t.Fatalf("NewSection produced %d parts, want 5", len(parts))
	}
}

func TestNewSection_Uniqueness(t *testing.T) {
	t.Parallel()
	id1 := NewSection("ak33m", "home")
	id2 := NewSection("ak33m", "home")
	if id1 == id2 {
		t.Fatalf("expected unique IDs, got %q twice", id1)
	}
}

func TestNewTheme(t *testing.T) {
	t.Parallel()
	id := NewTheme("my-theme")
	if id != "urn:theme:my-theme" {
		t.Fatalf("NewTheme(\"my-theme\") = %q, want %q", id, "urn:theme:my-theme")
	}
}

func TestNewTemplate(t *testing.T) {
	t.Parallel()
	id := NewTemplate("my-theme", "home")
	if !strings.HasPrefix(id, "urn:theme:my-theme:template:") {
		t.Fatalf("NewTemplate produced %q, want prefix %q",
			id, "urn:theme:my-theme:template:")
	}
	if !strings.HasSuffix(id, ":home") {
		t.Fatalf("NewTemplate produced %q, want suffix %q", id, ":home")
	}
	parts := strings.Split(id, ":")
	if len(parts) != 6 {
		t.Fatalf("NewTemplate produced %d parts, want 6", len(parts))
	}
}

func TestNewTemplate_Uniqueness(t *testing.T) {
	t.Parallel()
	id1 := NewTemplate("my-theme", "home")
	id2 := NewTemplate("my-theme", "home")
	if id1 == id2 {
		t.Fatalf("expected unique IDs, got %q twice", id1)
	}
}

func TestNewPerson(t *testing.T) {
	t.Parallel()
	id := NewPerson()
	if !strings.HasPrefix(id, "urn:person:") {
		t.Fatalf("NewPerson produced %q, want prefix %q", id, "urn:person:")
	}
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		t.Fatalf("NewPerson produced %d parts, want 3", len(parts))
	}
}

func TestNewPerson_Uniqueness(t *testing.T) {
	t.Parallel()
	id1 := NewPerson()
	id2 := NewPerson()
	if id1 == id2 {
		t.Fatalf("expected unique IDs, got %q twice", id1)
	}
}

func TestNewOrganization(t *testing.T) {
	t.Parallel()
	id := NewOrganization("acme-corp")
	if id != "urn:org:acme-corp" {
		t.Fatalf("NewOrganization(\"acme-corp\") = %q, want %q", id, "urn:org:acme-corp")
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		input string
		want string
	}{
		{"simple words", "My Website", "my-website"},
		{"words with numbers", "Hello World 123", "hello-world-123"},
		{"spaces and symbols", "  Spaces & Symbols!  ", "spaces-symbols"},
		{"empty string", "", ""},
		{"already a slug", "my-website", "my-website"},
		{"multiple hyphens", "foo---bar", "foo-bar"},
		{"special characters", "Héllo Wörld", "h-llo-w-rld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Slugify(tt.input)
			if got != tt.want {
				t.Fatalf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractThemeSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want string
	}{
		{"theme URN", "urn:theme:my-theme", "my-theme"},
		{"template URN", "urn:theme:my-theme:template:abc:home", "my-theme"},
		{"non-theme URN returns empty", "urn:ak33m", ""},
		{"non-URN returns empty", "plain-id", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractThemeSlug(tt.id)
			if got != tt.want {
				t.Fatalf("ExtractThemeSlug(%q) = %q, want %q",
					tt.id, got, tt.want)
			}
		})
	}
}

func TestExtractEntityType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want string
	}{
		{"website URN", "urn:ak33m", "website"},
		{"page URN", "urn:ak33m:page:abc123:about-us", "page"},
		{"section URN", "urn:ak33m:about-us:section:xyz789", "section"},
		{"theme URN", "urn:theme:my-theme", "theme"},
		{"template URN", "urn:theme:my-theme:template:abc:home", "template"},
		{"person URN", "urn:person:abc123", "person"},
		{"organization URN", "urn:org:acme-corp", "organization"},
		{"non-URN returns empty", "plain-id", ""},
		{"3-part unknown returns empty", "urn:ak33m:something", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractEntityType(tt.id)
			if got != tt.want {
				t.Fatalf("ExtractEntityType(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestExtractKSUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want string
	}{
		{"website has no KSUID", "urn:ak33m", ""},
		{"page KSUID", "urn:ak33m:page:abc123:about-us", "abc123"},
		{"section KSUID", "urn:ak33m:about-us:section:xyz789", "xyz789"},
		{"theme has no KSUID", "urn:theme:my-theme", ""},
		{"template KSUID", "urn:theme:my-theme:template:abc123:home", "abc123"},
		{"person KSUID", "urn:person:abc123", "abc123"},
		{"organization has no KSUID", "urn:org:acme-corp", ""},
		{"non-URN passthrough", "some-plain-id", "some-plain-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractKSUID(tt.id)
			if got != tt.want {
				t.Fatalf("ExtractKSUID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestExtractWebsiteSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want string
	}{
		{"website URN", "urn:ak33m", "ak33m"},
		{"page URN", "urn:ak33m:page:abc123:about-us", "ak33m"},
		{"section URN", "urn:ak33m:about-us:section:xyz789", "ak33m"},
		{"non-URN returns empty", "plain-id", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractWebsiteSlug(tt.id)
			if got != tt.want {
				t.Fatalf("ExtractWebsiteSlug(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestExtractPageSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want string
	}{
		{"page URN returns page slug", "urn:ak33m:page:abc123:about-us", "about-us"},
		{"section URN returns page slug", "urn:ak33m:about-us:section:xyz789", "about-us"},
		{"website URN returns empty", "urn:ak33m", ""},
		{"non-URN returns empty", "plain-id", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractPageSlug(tt.id)
			if got != tt.want {
				t.Fatalf("ExtractPageSlug(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}
