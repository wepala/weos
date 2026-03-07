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
	Name string `json:"name"`
	Slug string `json:"slug"`
	File string `json:"file"`
}
