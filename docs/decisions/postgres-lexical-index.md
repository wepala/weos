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
**Finding:** wm-yna4s3 — account scope on the lexical index (closed by this story)
**Amended by:** [An Any-Word Match Mode on Lexical Search]({% link decisions/lexical-any-word-match.md %})
(wm-u1xp4w) — `Search` takes a `LexicalQuery` with a match mode; the all-words query below is unchanged

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

A third problem, found in review (wm-yna4s3), also applied to both engines. `resource_search` held
only `id`, `type_slug` and `content`, and `memory_search` answered from it with no visibility check.
The table is one for the whole instance, so on a multi-account deployment one account's search
returned another account's resources, and quoted their text in the snippet.

## Decision Drivers

- **The same contract on both engines.** The subscriber drives `Index`, `Remove` and `Clear` the
  same way on SQLite and PostgreSQL, with no second subscriber. A rebuild (`Clear`, then a replay)
  gives the same rows and the same indexed text on both. Rank order is each engine's own.
- **A query is user text, never query syntax.** `fts5Match` already gives this guarantee on SQLite.
  Operator characters, quotes and operator words are matched as words. Words are AND-ed.
- **No new dependency.** No extension (`pg_trgm` was cut at the plan gate), no embeddings, and
  lexical search never depends on the graph store.
- **Existing callers keep working without change.**
- **A search never shows a caller a resource the caller may not read**, by identifier or by
  snippet, and the index must stay correct when it is rebuilt from history, where no caller exists.

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

### The account scope

1. **Filter the answer after the search.** Rejected: the limit is already spent, so another
   account's better-ranked matches push the caller's own out of a crowded result, and the snippets
   are built in the same query.
2. **Scope by `account_id` alone.** Rejected by Akeem on wm-yna4s3: it is a second visibility rule
   beside the one resource reads already use, and the two would disagree.
3. **The rule resource reads use, applied in the query** — chosen. The scope is the caller's
   `VisibilityScope`, built from the identity on the context exactly as `ResourceService` builds it
   (`visibilityScopeFromContext`), and the predicate is the one `FindAccessibleIDs` applies:
   the caller created the resource, **or** the resource has no owner, **or** the caller holds a
   `read` grant on it in `resource_permissions`. One function, `accessiblePredicate`, serves both,
   so they cannot drift. It goes in the `WHERE` clause beside the `MATCH` / `@@` and the type filter,
   before `ORDER BY ... LIMIT`. A caller with no identity (CLI, stdio, a background job) has a nil
   scope and searches everything, as it reads everything; so does an admin scope.

**Ownerless resources stay visible to every caller** (decided by Akeem on wm-yna4s3). A resource
created by the CLI, a preset or a stdio session has an empty `created_by`, and `FindAccessibleIDs`
already treats it as readable by everyone. The index follows that rule rather than hiding preset
data from every account.

**The owner comes from the events, never from the context.** `lexicalHandler` reads `CreatedBy` and
`AccountID` from the resource's `Resource.Created` in the transaction on a create. An update's
transaction does not carry it, and an owner never changes, so on an update the handler reads the
owner back from the resource's existing index row (`LexicalIndex.Owner`, through the batch
transaction) and loads the aggregate's history for its `Resource.Created` only when there is no row
yet (wm-ehvlu3). A lookup or history load that fails is returned so the subscriber retries the
event; it is never indexed as ownerless. A rebuild replays the same events, the create before any
update, so it restores the same owners. On SQLite the row lookup is a scan of the FTS5 table, as the
`DELETE` in `Index` already is; an indexed id lookup is wm-xs10t3. `account_id` is stored beside `created_by`, as on the
`resources` table, though the predicate does not read it today.

### Migrating a table written before the owner columns

An existing `resource_search` has no owner columns, and its rows have no owner to read.

1. **Leave it, and tell operators to run `weos worker checkpoint reset lexical-index --truncate`.**
   Rejected as the only step: until someone runs it the old table cannot be scoped at all.
2. **`ALTER TABLE ... ADD COLUMN` with a default of `''`, then rebuild.** Rejected: every existing
   row reads as ownerless, which the scope shows to every caller — the leak, kept open until the
   rebuild finishes. FTS5 has no `ADD COLUMN` either, so SQLite would need a different path.
3. **Drop and recreate the table, and reset the checkpoint, in one transaction at startup** —
   chosen. `ProvideLexicalIndex` sees a `resource_search` without `created_by` (SQLite:
   `pragma_table_info`; PostgreSQL: `pg_attribute` of the one table `to_regclass` finds on the search
   path — the table the `DROP` and every query reach — under the same advisory lock as the DDL),
   drops it, creates the new shape, and sets the `lexical-index` row in
   `subscriber_checkpoints` to position 0. The rows were a derived view, so nothing is lost, and the
   subscriber refills the table from history with every owner. It logs that it did so. A second
   start finds the new shape and changes nothing. Because the reset is in the database, not in the
   process, it holds whichever process starts first — a CLI command, `serve` or `worker`.

The table is empty, never unscoped, while it refills: the partial-answer window below applies.

**A process still on the old binary during the upgrade cannot write an unowned row** (wm-f3t6tp).
The old binary writes `(id, type_slug, content)` and names no owner.

- On PostgreSQL the owner columns are `NOT NULL` with **no default**, so that insert fails. The
  batch it ran in rolls back, the checkpoint advance with it, and the event is left for a current
  binary to index with its owner. With a default of `''` the row would read as ownerless, be shown
  to every caller, and its checkpoint advance would commit. A table built with that default (a
  build of this story before wm-f3t6tp) is treated as an old shape and rebuilt the same way, since
  it may already hold such rows.
- On SQLite an FTS5 table takes no constraints and no triggers, so the insert succeeds — but its
  owner columns are `NULL`, which the scope never matches. A scoped caller is not shown the row; it
  is missing from search, never leaked, until the resource is next written or the index is rebuilt.
  A SQLite instance runs its subscribers in the one `serve` process, so an overlap needs two
  binaries on one database file.

Stopping every old-binary process before the upgrade is still the clean path.

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
| Table | `resource_search (id TEXT PRIMARY KEY, type_slug TEXT NOT NULL, created_by TEXT NOT NULL, account_id TEXT NOT NULL, content TEXT NOT NULL, tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED)`. SQLite: `fts5(content, id UNINDEXED, type_slug UNINDEXED, created_by UNINDEXED, account_id UNINDEXED)` |
| Indexes | GIN on `tsv`; b-tree on `type_slug` and on `created_by` |
| Creation | `ProvideLexicalIndex`, under a transaction-scoped advisory lock so a serve and a worker process that boot together do not race the `IF NOT EXISTS` DDL. A failure is logged and degrades to the inactive index, as on SQLite: `memory_search` then answers from graph label search and reports `graph-labels`. The DDL runs with the app's own database user, which therefore needs the **CREATE privilege on the schema**. PostgreSQL 15+ no longer grants it on `public` to every user, so a hardened deployment must grant it (`GRANT CREATE ON SCHEMA public TO <app user>`); the error log names the privilege. After the grant, a restart creates the index. |
| `Index` | `INSERT ... ON CONFLICT (id) DO UPDATE`, writing the owner on the update path as well as the text |
| Content cap | Both engines index the first 256 KiB of a resource's text, cut on a UTF-8 rune boundary. PostgreSQL refuses a `tsvector` over 1 MB, and the vector of all-distinct words is up to three times the text, so without the cap a large resource fails every insert, parks its event and is never searchable. SQLite takes the same cut so both engines index the same text. Words past the cap are not searchable on either engine. |
| `Remove` | `DELETE` |
| `Clear` | `TRUNCATE` |
| `Search` | `plainto_tsquery('simple', ?)`, plus the type filter and the caller's scope in the same `WHERE`, ordered by `ts_rank` then `id`; `ts_headline` builds the snippet for the limited page only |
| Writes | join the subscriber's batch transaction when one is on the context, as on SQLite, so the index write and the checkpoint advance commit together |

### How an existing PostgreSQL deployment fills the new table

No step is needed. The `lexical-index` group was never registered on PostgreSQL, so it has no
checkpoint row. The group does not set `StartAtHead`, so on first start pericarp creates its
checkpoint at position 0 and the subscriber replays the whole event history into the empty table.
`TestProvideLexicalGroup_ActiveIndexReplaysFromTheStart` pins that. A later rebuild is
`weos worker checkpoint reset lexical-index --truncate`, as on SQLite.

A deployment that already has a `resource_search` without owner columns (any SQLite deployment, and
a PostgreSQL one that ran this story before wm-yna4s3) needs no step either: startup recreates the
table and resets the checkpoint, as above. `weos worker checkpoint reset lexical-index --truncate`
does the same rebuild by hand, and is the command to run if the automatic one is ever in doubt.

## Consequences

- The `LexicalIndex` interface changed (`Engine()`, the type filter, `Owner()`, `Lag()`). It has one
  implementation per engine plus the inactive one, all in `infrastructure/database/gorm`.
- `memory_search` has a new optional input, `typeSlugs`, a new `mode` value, `postgres-fts`, and a
  new optional output, `notice`.
- **Search answers partially while the index fills, and says so (wm-4db1v2).** After an upgrade the
  index is `Active()` the moment `resource_search` exists, but it fills from a from-zero replay of
  the whole event history. Until the `lexical-index` subscriber catches up, `memory_search` returns
  only what has been replayed so far, and still reports its full-text `mode`. The same holds after
  every `weos worker checkpoint reset lexical-index --truncate` rebuild, on either engine, and the
  window is as long as the replay of the instance's history takes. So the answer carries a `notice`
  beside the `mode` whenever the index is behind: it says the index is rebuilding, about how many
  events behind it is, and that the answer may be incomplete. `LexicalIndex.Lag` reads it in one
  statement — the newest `events.position` (the end of its unique index) less the `lexical-index`
  row in `subscriber_checkpoints` (a primary-key lookup) — so the check costs the same on any size
  of history. A lag that cannot be read gives a notice that the answer may be incomplete, never
  silence. The notice also appears for the moment between a write and the subscriber indexing it;
  it is true then too. The key is omitted when the index has caught up, and on `graph-labels`.
- **The cross-account gap is closed (wm-yna4s3).** The index is scoped by the caller's visibility
  on both engines, in the query, by the `FindAccessibleIDs` rule; ownerless resources stay visible to
  every caller. The degraded graph-label path is unchanged and stays scoped as before.
- `LexicalIndex.Index` takes the resource's owner and `LexicalIndex.Search` takes a
  `*VisibilityScope`. `LexicalSearch.Search` is unchanged: it reads the scope from the context.
- **The first start after the upgrade rebuilds the index**, on every existing deployment, from a
  from-zero replay. The partial-answer window above applies to it.
- **The scope inherits `FindAccessibleIDs`' limits.** It does not apply the account admin/owner
  bypass that `GetByID` applies, so an account admin searches only what they created, what is
  ownerless, and what was shared with them. That is the strict side, and it matches the graph filter.
