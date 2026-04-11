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

package storage

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/segmentio/ksuid"
)

const maxFilenameLength = 200

var (
	unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	safeIDChars = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// ValidateID checks that a caller-supplied ID contains only safe characters
// (alphanumeric, underscore, hyphen) and is non-empty. This prevents path
// traversal when the ID is used as part of a filesystem path or object key.
func ValidateID(id string) error {
	if id == "" {
		return fmt.Errorf("upload ID must not be empty")
	}
	if !safeIDChars.MatchString(id) {
		return fmt.Errorf("upload ID contains unsafe characters: %q", id)
	}
	return nil
}

// SanitizeFilename strips path separators, collapses unsafe characters,
// and truncates the result to a safe length.
func SanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = unsafeChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if len(name) > maxFilenameLength {
		name = name[:maxFilenameLength]
	}
	if name == "" || name == "." {
		name = "unnamed"
	}
	return name
}

// GenerateObjectKey produces a unique object key by prepending a KSUID
// to the sanitized original filename under the given prefix.
func GenerateObjectKey(prefix, originalName string) string {
	_, key := GenerateObjectKeyWithID(prefix, originalName)
	return key
}

// GenerateObjectKeyWithID is like GenerateObjectKey but also returns the bare
// KSUID so callers can use a consistent ID across all backends.
func GenerateObjectKeyWithID(prefix, originalName string) (id, key string) {
	id = ksuid.New().String()
	safe := SanitizeFilename(originalName)
	return id, prefix + "/" + id + "-" + safe
}
