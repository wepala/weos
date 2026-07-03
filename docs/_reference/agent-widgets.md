# Agent widget contract

The in-app agent answers every conversation turn with **widgets**: an ordered
list of typed blocks any client renders consistently — the embedded admin SPA
or a third-party renderer. The set is closed (agents never emit free-form
HTML), payloads are validated server-side, and anything malformed degrades to
a markdown widget, so a bad payload can never break a response.

The Go contract lives in `pkg/widgets`; `widgets.Parse` is the validating
entry point.

## Response envelope

```json
{
  "schemaVersion": 1,
  "widgets": [ { "type": "markdown", "markdown": "…" } ]
}
```

- `schemaVersion` — contract version. This document describes **v1**.
  Clients should render widgets with unknown `type` values (from future
  versions) as markdown of their raw JSON, which is also what the server does.
- `widgets` — render in order.

## Widget types (v1)

### `markdown` — prose

```json
{ "type": "markdown", "markdown": "The **fastest** route is via kg_search_entities." }
```

`markdown` is required and carries CommonMark text. Optional `title`.

### `table` — tabular data

```json
{
  "type": "table",
  "title": "People",
  "columns": ["Name", "Email"],
  "rows": [["Ada Lovelace", "ada@example.com"], ["Grace Hopper", "grace@example.com"]]
}
```

`columns` is required and non-empty; every row must have exactly one cell per
column (ragged tables are degraded). Cells are strings. Optional `title`.

### `list` — short enumerations

```json
{ "type": "list", "title": "Next steps", "items": ["Confirm the invoice", "Email Ada"] }
```

`items` is required and non-empty. Optional `title`.

### `card` — a single entity

```json
{
  "type": "card",
  "title": "Ada Lovelace",
  "body": "Analyst engine programmer.",
  "url": "https://example.com/people/ada",
  "fields": [
    { "label": "Email", "value": "ada@example.com" },
    { "label": "URN", "value": "urn:person:2ZkX…" }
  ]
}
```

At least one of `title` or `body` is required. `fields` are labeled values;
`url` links the card.

## Validation and degradation

`widgets.Parse` applies these rules to the agent's raw output:

1. Contract JSON (optionally wrapped in a ```json code fence) is parsed and
   each widget validated independently.
2. A widget that fails validation — unknown `type`, missing required fields,
   ragged table rows — is replaced by a markdown widget carrying its raw
   JSON. Valid neighbors are unaffected.
3. Output that is not contract JSON at all becomes one markdown widget with
   the raw text.

A response therefore always renders; the failure mode is "less structured",
never "broken".

## How agents emit the contract

Every agent (the coordinator and each skill) carries a standing instruction
to reply with contract JSON. Skills that use no tools additionally get the
contract enforced as a model response schema; agents with tools cannot
(Gemini does not combine forced response schemas with tool calling), which is
why server-side validation is the guarantee, not model compliance.

Skill authors can steer widget choice per skill with the `widgets` hint on
the `agent-skill` resource (see the agents preset).
