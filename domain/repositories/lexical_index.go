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

package repositories

import "context"

// LexicalHit is one full-text match against the lexical index.
type LexicalHit struct {
	ID       string
	TypeSlug string
	Snippet  string
}

// Lexical index engines, as reported by LexicalIndex.Engine. Lexical search
// reports the engine to its callers as the source of an answer.
const (
	// LexicalEngineFTS5 is the SQLite FTS5 virtual table.
	LexicalEngineFTS5 = "fts5"
	// LexicalEnginePostgres is a PostgreSQL tsvector column under a GIN index.
	LexicalEnginePostgres = "postgres-fts"
)

// LexicalIndex is a full-text index over resource text literals, maintained
// from resource events by a checkpointed subscriber (so it is a rebuildable
// derived view, like every other projection). SQLite indexes with FTS5 and
// PostgreSQL with a tsvector column; an implementation may still be inactive
// (the FTS5 table could not be created), in which case lexical search falls
// back to graph label search.
type LexicalIndex interface {
	// Active reports whether the index is usable in this process.
	Active() bool
	// Engine names the full-text engine behind the index (LexicalEngineFTS5,
	// LexicalEnginePostgres), or "" when the index is inactive.
	Engine() string
	// Index replaces the indexed content for a resource. Each engine indexes
	// the same bounded prefix of a very large content (about 256 KiB), so a
	// resource of any size can be indexed; words past the prefix are not
	// searchable.
	Index(ctx context.Context, id, typeSlug, content string) error
	// Remove drops a resource from the index.
	Remove(ctx context.Context, id string) error
	// Search returns full-text matches for the query, best first. The query
	// is user text, never query syntax: its words are AND-ed. When typeSlugs
	// is non-empty only resources of those types are matched, so the limit is
	// spent on those types alone; a type with nothing indexed matches nothing.
	Search(ctx context.Context, query string, limit int, typeSlugs ...string) ([]LexicalHit, error)
	// Clear empties the index (the subscriber group's truncate/rebuild hook).
	Clear(ctx context.Context) error
}
