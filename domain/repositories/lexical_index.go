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

import (
	"context"
	"fmt"
	"strings"
)

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

// MatchMode says which words of a lexical query a hit must hold.
type MatchMode int

const (
	// MatchAll — the zero value and the default — matches a resource only
	// when it holds every word of the query, as typed.
	MatchAll MatchMode = iota
	// MatchAny matches a resource that holds any content word of the query,
	// and ranks the hits by how many of them each holds.
	MatchAny
)

// String names the mode as a caller writes it: "all" or "any".
func (m MatchMode) String() string {
	if m == MatchAny {
		return "any"
	}
	return "all"
}

// ParseMatchMode reads a match mode as a caller writes it. "" and "all" are
// MatchAll and "any" is MatchAny, in any case and with surrounding spaces;
// anything else is an error, never quietly the default.
func ParseMatchMode(s string) (MatchMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all":
		return MatchAll, nil
	case "any":
		return MatchAny, nil
	}
	return MatchAll, fmt.Errorf("unknown match mode %q: use \"all\" or \"any\"", s)
}

// LexicalQuery is one lexical search.
type LexicalQuery struct {
	// Text is the user's query text. It is never read as query syntax.
	Text string
	// Limit is the most hits to return.
	Limit int
	// TypeSlugs, when non-empty, narrows the search to those resource types.
	TypeSlugs []string
	// Match is MatchAll (the zero value) or MatchAny.
	Match MatchMode
	// MinMatch is the fewest content words a hit must hold, in MatchAny mode
	// only. 0 and 1 both mean one word, and a value above the query's number
	// of content words is capped at it. MatchAll ignores it.
	MinMatch int
}

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
	// text is user text, never query syntax.
	//
	// In MatchAll mode (the default) its words are AND-ed, as typed: no word
	// is dropped. In MatchAny mode the text is reduced to its content words
	// (lexical.ContentWords: stop words dropped, at most eight), a resource
	// holding any of them is a hit, and hits are ranked by coverage — how
	// many distinct content words a hit holds — then by the engine's
	// relevance, then by id. A hit holding fewer than
	// lexical.Floor(MinMatch, words) content words is dropped. A text with no
	// content words matches nothing.
	//
	// When TypeSlugs is non-empty only resources of those types are matched,
	// so the limit is spent on those types alone; a type with nothing indexed
	// matches nothing.
	//
	// scope limits the matches to resources the caller may read, by the same
	// rule as ResourceRepository.FindAccessibleIDs: the caller created it, it
	// has no owner, or the caller holds a read grant on it. The scope is part
	// of the match, so the limit is spent on readable resources alone and a
	// resource the caller may not read is never counted, returned or quoted.
	// A nil or admin scope (a system caller) matches every resource. In
	// MatchAny mode the type filter, the scope and the coverage ordering are
	// all applied before the limit.
	Search(ctx context.Context, query LexicalQuery, scope *VisibilityScope) ([]LexicalHit, error)
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
