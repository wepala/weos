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

// LexicalOwner is who a resource in the lexical index belongs to: the agent
// that created it and the account it was created in. The handler reads both
// from the resource's own events, never from the caller's context, so a
// rebuild from history (which has no caller) restores them exactly. An empty
// CreatedBy marks an ownerless resource (created by the CLI, a preset or a
// stdio session), which every caller may search, as every caller may read it.
type LexicalOwner struct {
	CreatedBy string
	AccountID string
}

// LexicalIndexSubscriber is the checkpoint name of the subscriber group that
// fills the lexical index. An index that changes its table shape resets this
// checkpoint so the group refills the new table from event 0.
const LexicalIndexSubscriber = "lexical-index"

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
	// Index replaces the indexed content and owner for a resource. Each
	// engine indexes the same bounded prefix of a very large content (about
	// 256 KiB), so a resource of any size can be indexed; words past the
	// prefix are not searchable.
	Index(ctx context.Context, id, typeSlug, content string, owner LexicalOwner) error
	// Remove drops a resource from the index.
	Remove(ctx context.Context, id string) error
	// Owner returns the owner a resource is indexed with, and false when the
	// index holds no row for it (or only a row with no owner recorded). An
	// owner never changes, so the subscriber reads it here on an update rather
	// than loading the resource's whole history. It reads through the
	// subscriber's batch transaction when one is on ctx, so a row written
	// earlier in the same batch is seen.
	Owner(ctx context.Context, id string) (LexicalOwner, bool, error)
	// Search returns full-text matches for the query, best first. The query
	// is user text, never query syntax: its words are AND-ed. When typeSlugs
	// is non-empty only resources of those types are matched, so the limit is
	// spent on those types alone; a type with nothing indexed matches nothing.
	//
	// scope limits the matches to resources the caller may read, by the same
	// rule as ResourceRepository.FindAccessibleIDs: the caller created it, it
	// has no owner, or the caller holds a read grant on it. The scope is part
	// of the match, so the limit is spent on readable resources alone and a
	// resource the caller may not read is never counted, returned or quoted.
	// A nil or admin scope (a system caller) matches every resource.
	Search(ctx context.Context, query string, limit int, scope *VisibilityScope,
		typeSlugs ...string) ([]LexicalHit, error)
	// Clear empties the index (the subscriber group's truncate/rebuild hook).
	Clear(ctx context.Context) error
	// Lag reports how far the index is behind the event history: the newest
	// stored event's position less the LexicalIndexSubscriber checkpoint, or
	// the newest position when the subscriber has never run. 0 means caught
	// up. While it is above 0 — a refill after an upgrade or a rebuild, or a
	// subscriber that is behind — a search answers from part of the history.
	// Positions can have gaps, so it counts positions, not exactly events.
	Lag(ctx context.Context) (int64, error)
}
