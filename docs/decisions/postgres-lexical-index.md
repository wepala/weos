---
title: "ADR: A PostgreSQL Lexical Index, and a Type Filter on Lexical Search"
parent: Architecture Decision Records
layout: default
nav_order: 14
---

# ADR: A PostgreSQL Lexical Index, and a Type Filter on Lexical Search

**Status:** Accepted — Implemented
**Date:** 2026-11-11
**Story:** wm-irqzji — PostgreSQL lexical index for core, with a type filter on Search

## Problem

`memory_search` finds resources by keyword. It reads `repositories.LexicalIndex`, a derived view
over the text literals of every resource, kept in step with resource events by the checkpointed
`lexical-index` subscriber. Until this story only SQLite had one: an FTS5 virtual table named
`resource_search`. On PostgreSQL `ProvideLexicalIndex` returned an inactive index, and
`memory_search` fell back to the knowledge graph's label search. Label search sees a resource's
label and nothing else, so a word that appears only in a description could not be found on
PostgreSQL, and an instance with no graph store configured could not search at all.

A second problem applied to both engines. `Search` took no type. A client that wanted only tasks
had to filter the result itself, and in a crowded index the limit was spent on other types before
any task was reached, so the tasks it wanted were never in the result.

## Decision Drivers

- **The same contract on both engines.** The subscriber drives `Index`, `Remove` and `Clear` the
  same way on SQLite and PostgreSQL, with no second subscriber. A rebuild (`Clear`, then a replay)
  gives the same rows and the same indexed text on both. Rank order is each engine's own.
- **A query is user text, never query syntax.** `fts5Match` already gives this guarantee on SQLite.
  Operator characters, quotes and operator words are matched as words. Words are AND-ed.
- **No new dependency.** No extension (`pg_trgm` was cut at the plan gate), no embeddings, and
  lexical search never depends on the graph store.
- **Existing callers keep working without change.**

## Considered Options

### The PostgreSQL index

1. **Keep the label-search fallback.** Rejected: it is the problem.
2. **`to_tsvector(...) @@ to_tsquery(...)` computed per query, no stored column.** No schema beyond
   the table, but every search parses every row. Rejected.
3. **A `tsvector` column generated from `content`, under a GIN index** — chosen. The generated
   column means a write sets only the text, and the vector cannot drift from it.

### The text search configuration

1. **`english`** (stemming, stop words). Rejected: `memory_search` exists for proper nouns and
   identifiers, which stemming changes and stop-word removal can drop, and SQLite's default FTS5
   tokenizer (`unicode61`) does neither.
2. **`simple`** (lowercased words, no stemming, no stop words) — chosen. It is the closest built-in
   configuration to `unicode61`, but it is **not** word-for-word parity. The two engines split text
   differently:

   | Text | SQLite `unicode61` | PostgreSQL `simple` |
   |---|---|---|
   | Accents (`José`) | folded: `Jose` finds it | kept: only `José` finds it |
   | Email, host name, dotted token (`jose@example.com`, `example.com`, `v3.2`) | split at `@` and `.`: `example` finds it | one word: only the whole token finds it |
   | Hyphenated word (`foo-bar`) | split: `bar` finds it | indexed whole and as parts: `bar` finds it |

   So a search can find a resource on SQLite and not on PostgreSQL. Folding accents on PostgreSQL
   needs the `unaccent` extension, which this story does not add (no new dependency); it is a
   follow-up.

### The query parser

1. **`to_tsquery`.** Rejected: user input is query syntax, and malformed syntax is an error.
2. **`websearch_to_tsquery`.** Rejected: it obeys double quotes (an adjacency phrase), the word
   `or` and a leading `-`, so a typed question can quietly mean something else.
3. **`plainto_tsquery`** — chosen. Every operator character and quote is a word separator and the
   words are AND-ed, which is what `fts5Match` does. A query with no words left becomes an empty
   query, which matches nothing.

### The type filter

1. **Filter the result after the search.** Rejected: the limit is already spent.
2. **A variadic `typeSlugs ...string` on `LexicalIndex.Search` and `LexicalSearch.Search`** —
   chosen. The filter is in the SQL on both engines (`type_slug IN (...)`), so the limit is spent on
   the named types alone. A variadic parameter keeps every existing call site compiling unchanged.
   Slugs are trimmed and lowercased first (type slugs are always stored lowercase), so `Task `
   filters to tasks; a plural such as `tasks` is a different slug and is not rewritten. Blank
   entries are dropped, so a filter of blanks is no filter. A type with nothing indexed
   matches nothing; it is not an error, because a derived view cannot tell "no such type" from
   "nothing indexed under it".

   The degraded label-search path is narrowed too. Label search knows classes, not resource types,
   so the filter is applied to the resource URN (`urn:<typeSlug>:<id>`), and a narrowed search asks
   the graph for the maximum (100) so other types' labels do not spend the limit first.

### The `mode` that `memory_search` reports

1. **Report `fts5` on PostgreSQL too.** Rejected: the output would name an engine that did not
   answer.
2. **One engine-neutral value (`full-text`) on both engines.** Rejected: it changes the value every
   existing SQLite client already reads.
3. **A value per engine: `fts5` on SQLite (unchanged), `postgres-fts` on PostgreSQL** — chosen.
   No existing client sees a different value: PostgreSQL reported `graph-labels` until now.
   `LexicalIndex` gains `Engine()`, which the search reports as its mode, and
   `application.IsFullTextMode` lets Go callers ask "full text or degraded?" without naming an
   engine.

## Decision Outcome

| Part | Shape |
|---|---|
| Table | `resource_search (id TEXT PRIMARY KEY, type_slug TEXT NOT NULL, content TEXT NOT NULL, tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED)` |
| Indexes | GIN on `tsv`; b-tree on `type_slug` |
| Creation | `ProvideLexicalIndex`, under a transaction-scoped advisory lock so a serve and a worker process that boot together do not race the `IF NOT EXISTS` DDL. A failure is logged and degrades to the inactive index, as on SQLite. |
| `Index` | `INSERT ... ON CONFLICT (id) DO UPDATE` |
| Content cap | Both engines index the first 256 KiB of a resource's text, cut on a UTF-8 rune boundary. PostgreSQL refuses a `tsvector` over 1 MB, and the vector of all-distinct words is up to three times the text, so without the cap a large resource fails every insert, parks its event and is never searchable. SQLite takes the same cut so both engines index the same text. Words past the cap are not searchable on either engine. |
| `Remove` | `DELETE` |
| `Clear` | `TRUNCATE` |
| `Search` | `plainto_tsquery('simple', ?)`, ordered by `ts_rank` then `id`; `ts_headline` builds the snippet for the limited page only |
| Writes | join the subscriber's batch transaction when one is on the context, as on SQLite, so the index write and the checkpoint advance commit together |

### How an existing PostgreSQL deployment fills the new table

No step is needed. The `lexical-index` group was never registered on PostgreSQL, so it has no
checkpoint row. The group does not set `StartAtHead`, so on first start pericarp creates its
checkpoint at position 0 and the subscriber replays the whole event history into the empty table.
`TestProvideLexicalGroup_ActiveIndexReplaysFromTheStart` pins that. A later rebuild is
`weos worker checkpoint reset lexical-index --truncate`, as on SQLite.

## Consequences

- The `LexicalIndex` interface changed (`Engine()`, the type filter). It has one implementation per
  engine plus the inactive one, all in `infrastructure/database/gorm`.
- `memory_search` has a new optional input, `typeSlugs`, and a new `mode` value, `postgres-fts`.
- **The index is not scoped by account or visibility, on either engine.** Before this story a
  multi-account PostgreSQL deployment answered `memory_search` from label search, which per-account
  graph stores scope to the caller. It now answers from the shared index. Tracked as wm-yna4s3.
