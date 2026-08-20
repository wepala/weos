---
title: Feature Flags
parent: Explanation
layout: default
nav_order: 8
---

# Feature Flags

WeOS resolves a feature to one boolean for one caller, and every surface that gates something asks the same question through the same function. This page explains why it is shaped that way. For the API surface see the [Feature Flags reference]({% link _reference/feature-flags.md %}); to gate something of your own see [Gate a Capability]({% link _howto/gate-a-capability.md %}).

## The problem

An instance has capabilities that not everybody should have. Some are decisions for whoever runs the instance — "this deployment does not do ledger export at all". Some are decisions for a team — "our account does not want the assistant". Some are for one person — "give the auditor export for the next two weeks".

Those are three different decisions about the same capability, and they arrive from three different people. A single on/off switch cannot hold them, and three unrelated switches drift.

## Four layers, one answer

A feature is **declared** in code with a default, and then three stored layers may speak about it:

```
declaration default  →  instance override  →  account override  →  grant
```

Resolution walks them in that order and returns one boolean plus the layer that decided it. The whole rule is [`entities.ResolveFeature`](https://github.com/wepala/weos/blob/main/services/core/domain/entities/feature.go), and it is deliberately the only place precedence exists.

### An explicit off is final downward

If the instance says off, the answer is off, and no account override and no grant can lift it. The same applies to an account's off against a grant.

This is what makes an operator's switch mean something. An operator who turns a capability off is usually doing it because the instance cannot do it — no credentials, no licence, a compliance decision — and a grant that could override that would make the switch advice rather than a control.

### An explicit on is *not* final

If the instance turns something on, an account may still turn it off for themselves.

The asymmetry is the point. Off is a statement about what is possible; on is a statement about what is permitted. Permission flows down and can be narrowed; possibility does not flow back up.

### The declaration default is not an explicit off

A feature declared `default: false` is *unset everywhere*, not *off everywhere*. An account override or a grant can turn it on, because nobody has said no — they have only not yet said yes.

This is why the stored layers are **tri-state**: on, off, and nothing said. Row absence carries "nothing said", so there is no nullable column and no sentinel. It is also why turning a feature off and *resetting* it are different operations, with different endpoints and different CLI verbs — resetting returns a layer to silence, and a lower layer can then speak.

### `manageable` and `grantable` decide whether a layer is read at all

A declaration that says `manageable: false` means an account override is ignored — not rejected. A stored row may predate a declaration change, and resolution must never depend on whoever wrote it having been well behaved. The same holds for `grantable: false` and grants.

## Grants belong to an account

A grant names a **subject** — one agent, or a role — inside one account, and never an email address. An address is how an admin names somebody when granting; it is not what the grant is against, because an address can change and the grant should not follow it. Listings resolve the address back at read time.

A grant may carry a validity window. The window is half-open: the grant is on the instant `validFrom` arrives and off the instant `validThrough` passes.

**Expiry is lazy.** Nothing sweeps expired grants and nothing fires when a window closes; a row that has run out is simply not counted at the next resolution. That has a consequence worth knowing: there is no event at the moment a capability goes away, so nothing can be notified of it. See [Nothing announces a change](#nothing-announces-a-change).

A grant also stops applying when its subject leaves the account. Membership is checked when the grant is made, but nothing deletes the row when somebody leaves — so resolution skips grants for a caller with no membership, and listings report those rows as **orphaned** rather than expired. The two need different actions: an expired grant ran its course, an orphaned one is left-over access to clean up.

## Failing closed, and the one case that does not

If the stored state cannot be read, resolution answers **off**. A resolver that answered "on" on the way to a database error would hand out the capability at exactly the moment nobody can see why.

The one deliberate exception is a key **nobody declared**. That is registry drift — a deploy where a call site and the declarations disagree — and there the caller's own default stands and the instance logs it once. Closing every gate whose key does not resolve would turn one mistyped constant into a silent capability outage across an instance, with a surface that looks deliberate.

The two are not the same failure and must not be conflated: a typo leaves a capability where it was, a broken store takes gated capabilities away.

## Resolution is cached per caller

A caller's whole set is resolved once and read from memory afterwards. An agent turn evaluating thirty tools is ordinary, and resolving per key would multiply the database reads by the number of features rather than amortising them to one.

The cache key is **(agent, account)** — deliberately not a session. There is no session identifier on the MCP bearer surface, and keying on something one whole surface cannot supply would mean either no caching there or a silent fallback that behaves differently per surface. It is also strictly more correct: resolution reads the instance layer, the account layer, and the caller's grants and roles, and nothing else — so two sessions of one person in one account always resolve identically, and one invalidation reaches every device that person is signed in on.

Writes invalidate precisely: an instance change drops everything, an account change drops that account, a grant drops the people it reaches.

### Nothing announces a change

A validity window closing produces no event, so nothing can be pushed to a client at that moment. Every surface therefore treats a held answer as advisory:

- A tool list a client fetched an hour ago is **not** authorization. Calling a tool from it is resolved when the call arrives.
- A skill name held from a previous conversation is refused if the caller's features no longer reach it.
- A feature set a browser is holding decides what to *draw*, never what is *allowed*.

This is why every gated surface in WeOS has two gates, and why only one of them is the control. Hiding a tool, a skill, or a sidebar entry is a courtesy — it stops a model proposing something that will fail, and stops a person clicking a link that leads to a refusal. Refusing the call is the control. An implementation that filters the listing and forgets the handler has built an affordance and called it a control, and the difference is invisible right up until somebody uses a name they already had.

## Callers with no identity

An anonymous caller resolves from the **instance layer alone**. No account override and no grant is read, because there is no subject for either to attach to.

This deliberately differs from the rest of the codebase, where a nil identity means system context and bypasses gates. A background worker or a local `weos mcp` session must not silently receive every gated capability.

It has a practical consequence for the **stdio transport**: `weos mcp` is one local process with no session, no bearer token and no active account, so gating there is instance-wide and cannot be anything else. An account override or a personal grant makes no difference on that transport. Refusals are worded accordingly — "not enabled on this server" where there is no caller, "not enabled for you" where there is one — so nobody goes looking for a grant that cannot apply.

## Gates live where the thing lives

A gate is declared at the call site of the thing it gates: beside a tool's name and schema, in a skill's own definition, on the route's own registration. A capability declared in one file and gated in a registry somewhere else is two things that will drift.

The four surfaces WeOS gates today all consume the same resolved set, so a route and a tool gated on one key can never disagree about who holds it:

| Surface | Hidden | Refused |
|---------|--------|---------|
| MCP tools | absent from `tools/list` | `tools/call` returns an error result |
| Agent skills | not a coordinator sub-agent, so `transfer_to_agent` cannot reach it | direct invocation refused |
| HTTP routes | the admin does not draw the link | 403 with the feature named |
| Admin UI | sidebar entry and page absent | the route behind it refuses |

## Evaluation goes through OpenFeature

Call sites evaluate through the [OpenFeature](https://openfeature.dev) Go SDK rather than calling the resolver directly, on a named domain with the `feature.` key prefix. That buys a standard interface, room for a second provider on its own namespace later, and hooks for telemetry — without any call site knowing how resolution works.

One detail is load-bearing. When the store cannot be read, the provider returns `false` with reason `ERROR` and attaches **no** `ResolutionError`. The OpenFeature client treats a resolution error as "fall back to the caller's default", which would hand `true` to any call site that passed `true` — the exact opposite of failing closed, and invisible to a test that calls the provider directly instead of going through the client.

## Related

- [Gate a Capability]({% link _howto/gate-a-capability.md %}) — the task
- [Feature Flags reference]({% link _reference/feature-flags.md %}) — the precedence table, config, CLI, API and MCP surface
- [Auth, Roles and Access]({% link _tutorials/auth-roles-and-access.md %}) — roles are a different question from features, and the two are resolved separately
