---
title: "ADR: An Any-Word Match Mode on Lexical Search"
parent: Architecture Decision Records
layout: default
nav_order: 15
---

# ADR: An Any-Word Match Mode on Lexical Search

**Status:** Accepted — Implemented
**Date:** 2026-11-12
**Story:** wm-u1xp4w — lexical search: an any-word match mode with stop words removed and
coverage-first ranking, on PostgreSQL and SQLite
**Amends:** [A PostgreSQL Lexical Index, and a Type Filter on Lexical Search]({% link decisions/postgres-lexical-index.md %})
— the query contract of `LexicalIndex.Search`, on both engines

## Problem

Both lexical index engines match a query only when a resource holds **every** word of it:
`plainto_tsquery` AND-s its lexemes on PostgreSQL, and FTS5 AND-s the phrases `fts5Match` builds on
SQLite. That is right for `memory_search` — an agent recalling a known thing by two or three
keywords — and wrong for a support search, where a person types a sentence ("my child can't log in
on the tablet"). No answer holds every word of a sentence, so the index returns nothing. The
support hub cannot make the index its primary path (wm-wi1bts) until core can match **any** content
word and rank by how many match.

## Decision Drivers

- **The all-words mode does not change.** It stays the default; every existing caller and scenario
  gets the same answer. No stop word is dropped from it.
- **No index change.** No schema change, no generated-column change, no checkpoint reset — the
  looser mode is a different query over the same rows. The `simple` configuration stays: switching
  the generated column to `english` would force a rebuild everywhere and is wrong for text that is
  not English.
- **A query is still user text, never query syntax**, on both engines, and a query with no content
  words matches **nothing**, never everything.
- **One definition of a content word, both engines, and the hub agrees with it.**
- **The type filter and the caller's scope still narrow the match, before the limit**, and so does
  the ranking: the limit is spent on the best-covering hits.

## Considered Options

### The shape of the call

1. **Another positional parameter on `Search`.** Rejected: `typeSlugs` was just added as a variadic,
   and a mode and a floor after it do not fit.
2. **An options struct, `repositories.LexicalQuery{Text, Limit, TypeSlugs, Match, MinMatch}`** —
   chosen. `LexicalIndex.Search(ctx, LexicalQuery, *VisibilityScope)` and
   `LexicalSearch.Search(ctx, LexicalQuery)`. `MatchMode`'s zero value is `MatchAll`, so a query
   that names no mode is the old search. This breaks every implementation and caller of the two
   interfaces, in core and in services pinned to core; each adapts when it bumps the pin
   (wm-wi1bts for the hub).

### What a content word is

1. **Split exactly as the hub does** (at every character that is not a letter or a digit).
   Rejected for the query: PostgreSQL's `simple` parser keeps an email address, a host name and a
   dotted token as **one** lexeme, so the parts of `dana@example.com` never match on PostgreSQL.
2. **One Go tokenizer per engine, imitating each parser.** Rejected: PostgreSQL's parser has some
   twenty token types; a Go copy of it would drift.
3. **One Go rule that keeps a word as a person wrote it, and lets each engine read the word as it
   reads stored text** — chosen (`domain/lexical.ContentWords`). Lower-case; a word is a run of
   letters and digits, and a single `-`, `.` or `@` between two of them joins the run, so
   `follow-up`, `dana@example.com` and `v3.2` are one word each. An apostrophe always splits, as
   both engines split it when they index (`can't` is `can` + `t`). Every other character — space,
   quote, and every operator of either engine — separates words and is never inside one. Then stop
   words and single letters are dropped, duplicates removed, and the first **8** words kept (the
   hub's `maxQueryTerms`).

   Each engine then matches a joined word as a phrase of its own tokens: FTS5 reads `"follow-up"`
   as the phrase `follow up`; PostgreSQL's `to_tsquery` reads `'follow-up'` as
   `'follow-up' <-> 'follow' <-> 'up'` and `'dana@example.com'` as the email lexeme. Both match the
   resource that holds the word as written. A hyphenated word is therefore **one** content word on
   both engines, for coverage and for `MinMatch`.

### The stop list

One list, `domain/lexical/stopwords.go`, applied to the query in the any mode only. Its first block
is the support hub's `stopWords` (services/weos-support `support/answers.go`) word for word. Its
second block adds the modals the hub lacks (`cannot`, `may`, `might`, `must`, `shall`) and every
fragment a contraction leaves once its apostrophe splits it (`t`, `s`, `d`, `m`, `doesn`, `didn`,
`isn`, `won`, …). So `can't` is dropped **whole**: a query word `can` would match every "you can …"
text as strongly as every "can't …" one, which reads as the opposite meaning. Lexical search does
not model negation; the content words carry the match. Its third block is the greetings and thanks a
person wraps a support question in — `please`, `thanks`, `thank`, `hello`, `hi`, `hey` — which say
nothing about what the question is about, and would each spend one of the eight words.

The list is **English**. A query in another language keeps its own function words as content
words; they still match, they only rank less sharply.

The hub's own list differs from core's by those two added blocks and by how it splits a joined word
(the hub splits `follow-up` into `follow` + `up`). The hub adopts core's rule when it moves to the
index (wm-wi1bts).

### PostgreSQL, any mode

1. **`replace(plainto_tsquery(...)::text, ' & ', ' | ')::tsquery`.** Rejected: it round-trips the
   query through text and re-parses it as query syntax.
2. **`to_tsquery` over the words, each `quote_literal()`-d, joined with ` | `** — chosen. The words
   are bound one placeholder each into a `text[]`; SQL quotes each one and builds the OR query, so
   user text never reaches `to_tsquery` unquoted. Coverage is
   `(SELECT count(*) FROM unnest(words) w WHERE tsv @@ to_tsquery('simple', quote_literal(w)))`,
   computed only over the rows the OR query matched. The type filter and the scope sit in the same
   `WHERE`, the `MinMatch` floor in the next, and the rows are ordered by coverage, `ts_rank`, `id`
   before `LIMIT`. `ts_headline` still runs over the limited page only.

### SQLite, any mode

FTS5 matches the words phrase-quoted and joined with ` OR `, with the type filter and the scope in
the same `WHERE`. FTS5 has no per-phrase match count, so coverage is:

1. **One `MATCH` per word, as a correlated subquery per row.** Rejected for now: up to eight extra
   full-text lookups per matched row.
2. **Counted in Go over each matched row's content** — chosen (the plan's choice). The rows are
   streamed with their `id`, type, content and `bm25` rank; each row's coverage is counted by
   `lexical.Coverage`, which reads the content the way `unicode61` does (lower-cased, diacritics
   removed, split at every non-letter, non-digit) and holds a joined word as a run of consecutive
   tokens, the rule an FTS5 phrase follows. Only id, type, coverage and rank are kept per row, so
   memory grows with the number of matches, not their text. The candidates are ordered by coverage,
   `bm25` (lower is better), `id`, cut to the limit, and the snippets are built by a second query
   over the returned page alone. A row FTS5 matched counts as holding at least one word even where
   Go's reading of a rare character differs from `unicode61`'s.

   The cost is that an any-mode search reads the content of **every** matched row. On a large index
   a query of one very common word reads many rows. If that shows, option 1 or a cap on the rows
   read is the follow-up.

### `MinMatch`

The fewest content words a hit must hold, any mode only: `lexical.Floor(MinMatch, words)` — 0 and 1
mean one word, and a floor above the query's content-word count is capped at it, so it asks for
every word rather than for the impossible.

### The degraded graph-label path

Label search matches one substring of a label. In the any mode it is asked once per content word
(each for the maximum, 100), a resource's coverage is the number of words whose search found it,
the floor drops the rest, and the order is coverage then label search's own order. So the mode is
honoured on every path, and a query of stop words alone matches nothing there too.

## Decision Outcome

| Part | Shape |
|---|---|
| Call | `LexicalIndex.Search(ctx, LexicalQuery, *VisibilityScope)`; `LexicalSearch.Search(ctx, LexicalQuery)`; `SearchWithRebuildNotice(ctx, search, LexicalQuery)` |
| Modes | `MatchAll` (zero value, default, unchanged); `MatchAny` |
| Content words | `domain/lexical.ContentWords`: one rule, both engines, 8 words at most |
| PostgreSQL any | OR `to_tsquery` from `quote_literal`-ed bound words; coverage per word in SQL; floor, then `ORDER BY coverage DESC, rank DESC, id LIMIT` |
| SQLite any | `"w1" OR "w2"` MATCH; coverage in Go over streamed rows; sort coverage, `bm25`, id; snippets for the page |
| `memory_search` | optional `match: "all" \| "any"` (default `all`; anything else is an error) and `minMatch` |

## Consequences

- The `LexicalIndex` and `LexicalSearch` interfaces changed shape. Every implementation and fake in
  core moved with them; services pinned to core adapt when they bump the pin.
- `memory_search` has two new optional inputs, `match` and `minMatch`. A call naming neither is
  answered exactly as before.
- Rank order in the any mode is coverage first on both engines; within one coverage it is each
  engine's own relevance, as before.
- The engines still read some text differently (see the amended ADR's table): accents fold on SQLite
  only, and a word inside an email or host is a word on SQLite only. A query that names the whole
  token matches on both.
- No rebuild, no migration, no checkpoint reset.
