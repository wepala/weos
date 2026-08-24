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

package jsonld

import (
	"encoding/json"
	"strings"
)

// EdgeIDs unwraps a JSON-LD edge value into the resource IDs it carries and
// reports whether it was written as a list.
//
// It lives here rather than beside any one caller because every reader of an
// edges node needs exactly this, and each one used to open the value itself.
// Three of them only ever unwrapped a single `{"@id": …}` map and skipped
// anything else, so a property the schema declares as an ARRAY of references —
// which BuildResourceGraph correctly writes as a JSON array of refs — was
// silently dropped on read while the write, the projection column and the boot
// reconcile all reported success (issue #513).
//
// isList reports the shape the edge was written in, not how many IDs came back:
// a one-element array is still a list. Callers preserve that shape so a
// round-trip does not quietly turn a declared list into a scalar.
func EdgeIDs(val any) (ids []string, isList bool) {
	switch v := val.(type) {
	case map[string]any:
		if id, ok := v["@id"].(string); ok && id != "" {
			return []string{id}, false
		}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch entry := item.(type) {
			case map[string]any:
				if id, ok := entry["@id"].(string); ok && id != "" {
					out = append(out, id)
				}
			case string:
				if entry != "" {
					out = append(out, entry)
				}
			}
		}
		// An array with no usable @id still WAS a list; reporting that keeps a
		// caller from re-reading it as a scalar and writing a malformed value.
		return out, true
	case string:
		if v != "" {
			return []string{v}, false
		}
	}
	return nil, false
}

// EdgeProperty resolves one key from a stored edges node to the property name
// it belongs to, and reports whether it could.
//
// A resource stores its edges keyed by PROPERTY NAME (issue #515). Records
// written before that change are keyed by PREDICATE IRI, and both forms have
// to keep reading: there is no migration, so the two coexist indefinitely and
// every reader meets both.
//
// The two are told apart by shape rather than by a flag. An absolute IRI is
// never a valid JSON-LD term name, and a term name is never absolute, so the
// key itself says which form it is. A compact key needs no resolution at all —
// it IS the property name — which is the whole point of the change: the
// inversion that kept losing data is simply not performed for new records.
func EdgeProperty(key string, ldContext json.RawMessage) (string, bool) {
	if key == "" || key == "@id" {
		return "", false
	}
	if !IsIRIKey(key) {
		// Compact: the key is the property name.
		return key, true
	}
	// Expanded: invert the context, exactly as before the change.
	if name, ok := BuildReverseMap(ldContext)[key]; ok {
		return name, true
	}
	// No term names it, so the write path resolved it through `@vocab` and the
	// property name is what @vocab was prepended to. This is the shape issue
	// #510 reported — a reference with no term — and without this the records
	// that ticket is ABOUT stay unreadable, which is the one population the
	// no-migration guarantee most needs to cover.
	if vocab, _ := ParseContext(ldContext); vocab != "" && strings.HasPrefix(key, vocab) {
		if name := strings.TrimPrefix(key, vocab); name != "" && !IsIRIKey(name) {
			return name, true
		}
	}
	return "", false
}

// IsIRIKey reports whether an edges-node key is an IRI rather than a property
// name.
//
// JSON-LD states the rule exactly: a term MUST NOT contain a colon. So a colon
// is the discriminator, and it is exact for every scheme — a whitelist of
// http/https/urn missed a COMPACT IRI like `foaf:knows`, which is what a
// predicate resolves to when its prefix is undefined and no `@vocab` absorbs
// it. Such a key was read as a property name, so the API served a field called
// `foaf:knows` and the real one came back empty.
//
// Property names in this system are schema property names, which never contain
// a colon.
func IsIRIKey(key string) bool {
	return strings.Contains(key, ":")
}
