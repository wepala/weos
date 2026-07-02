---
name: agent-skill-seed
description: Turn a natural-language description of an interaction into an installed, working agent-skill — the declarative sub-agents the WeOS in-app orchestrator routes to. Use when someone says "add a skill that…", "teach the app to…", "the agent should be able to…", or describes a conversation they want their WeOS app to handle. Grounds tool selection in the instance's live tool registry, previews the definition before anything is installed, and verifies the installed skill with a smoke conversation.
---

# agent-skill-seed

Grow a new in-app agent skill from a described interaction. A skill is **data**
(an `agent-skill` resource), not code: this skill compresses the description
into the best v1 representation, installs it, and proves it works. It follows
the same intake → preview → install → verify shape as the `seed` capability
skill.

## What a v1 skill is

One ADK sub-agent, declared as an `agent-skill` resource (schema in
`application/presets/agents/preset.go`):

| field | role |
|---|---|
| `name` | unique, `^[a-zA-Z][a-zA-Z0-9_]{0,63}$` — doubles as the ADK agent name |
| `description` | **the routing signal** — the orchestrator's LLM routes on this; write it for a router, not a human |
| `instructions` | the skill's standing brief; judgment lives here |
| `tools` | allowlist of tool names — must exist on the instance's tool surface |
| `mode` | `task` (converses until done) or `single_turn` (answers once) |
| `widgets` | output hints from `markdown`, `table`, `list`, `card` (see `docs/_reference/agent-widgets.md`) |
| `schemaVersion` | `1` |

Free by default — do NOT add them to `tools`: `memory_recall`,
`memory_search`, `playbook_record_outcome` (every skill gets them), and
widget-JSON output formatting (every agent carries the contract brief).
Mutating tools automatically pause for the user's confirmation in the chat
(HITL) — never write instructions that tell the skill to skip or work around
confirmation.

## Workflow

### 1 — Intake (don't interrogate)

From the description, infer: the job (what conversation should succeed),
which data it touches, whether it converses (`task`) or answers one-shot
(`single_turn`). Ask at most one question, and only if the job itself is
ambiguous.

### 2 — Ground the tool selection (never guess)

Tool names come from the live registry, in order of preference:

1. A running instance connected over MCP: call its `tools/list` (any MCP
   client surface) and use the returned names + annotations.
2. This repo's source: `internal/mcp/*_tools.go` — every `mcp.AddTool`
   declaration; the annotation inventory test
   (`internal/mcp/agent_toolset_test.go`) is the authoritative
   read-only/mutating classification.

Select the smallest allowlist that covers the job. A tool name not present
in the registry will make the skill **fail to load** (the registry rejects
unknown tools), so an invented name is impossible to ship silently — but
don't rely on that: verify every name.

### 3 — Decide the representation, and say so

- Fits one sub-agent (instructions + allowlist + mode)? → v1 skill. Record
  *why* in a `<!-- rationale -->` comment shown with the preview.
- Genuinely needs a composed multi-step flow (fixed pipelines, fan-out,
  cross-skill handoffs)? **Say so explicitly**: scope the v1 skill to the
  routable core and note the rest is deferred — composed/graph skills are a
  future `schemaVersion`, not something to fake with prompt gymnastics.

Mode choice: `task` when the skill may need follow-up questions or several
tool rounds; `single_turn` for lookup-and-answer jobs.

### 4 — Preview (nothing installs without approval)

Show the complete definition JSON plus one-line rationales for: routing
description, tool allowlist, mode, widgets. Wait for approval; apply edits
and re-preview if asked.

### 5 — Install

Two paths — pick by target:

- **Running instance** (default): create the resource over MCP —
  `resource_create` with `type_slug: "agent-skill"` and the definition as
  `data`. The registry reloads on the publish event; no restart.
- **Project preset** (ship with the app): add the definition as a fixture on
  the `agent-skill` type in the project's preset (see `researcherFixture` in
  `application/presets/agents/preset.go` for the pattern).

### 6 — Verify with a smoke conversation

Against a running instance, send a turn that should route to the new skill:
`POST /api/agent/conversations/<fresh-id>/messages` with a representative
message (or drive it through the chat UI). Green means: the reply grounds in
the skill's tools and arrives as valid widget JSON. If the orchestrator
routed elsewhere, sharpen `description` (it is the routing signal) and
re-install — that's the most common fix. Report the result either way;
never declare success without the smoke turn.

## Anti-patterns

- **Guessed tool names** — ground in the registry (step 2); a typo means the
  skill silently fails to load with only a server log line.
- **Routing prose written for humans** — the orchestrator routes on
  `description`; "Handles invoice questions: totals, due dates, overdue
  lookups" routes; "A helpful assistant for invoices" doesn't.
- **Re-stating the defaults** — memory tools, widget formatting, and HITL
  confirmation are platform behavior; duplicating them in `instructions`
  drifts as the platform evolves.
- **Faking composed flows in one prompt** — flag them as deferred instead
  (step 3).
- **Installing without the preview or skipping the smoke turn.**
