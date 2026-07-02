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

// LexicalIndex is a full-text index over resource text literals, maintained
// from resource events by a checkpointed subscriber (so it is a rebuildable
// derived view, like every other projection). Implementations may be inactive
// — SQLite builds without FTS5, or PostgreSQL where FTS5 parity is out of
// scope — in which case lexical search falls back to graph label search.
type LexicalIndex interface {
	// Active reports whether the index is usable in this process.
	Active() bool
	// Index replaces the indexed content for a resource.
	Index(ctx context.Context, id, typeSlug, content string) error
	// Remove drops a resource from the index.
	Remove(ctx context.Context, id string) error
	// Search returns full-text matches for the query, best first.
	Search(ctx context.Context, query string, limit int) ([]LexicalHit, error)
	// Clear empties the index (the subscriber group's truncate/rebuild hook).
	Clear(ctx context.Context) error
}
