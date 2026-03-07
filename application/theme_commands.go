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

import "io"

type CreateThemeCommand struct {
	Name string
	Slug string
}

type UploadThemeCommand struct {
	ZipReader   io.Reader
	ZipSize     int64
	StoragePath string
	Name        string
	FileName    string
}

type UpdateThemeCommand struct {
	ID           string
	Name         string
	Slug         string
	Description  string
	Version      string
	ThumbnailURL string
	Status       string
}

type DeleteThemeCommand struct {
	ID string
}
