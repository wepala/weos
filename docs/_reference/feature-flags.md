---
title: Feature Flags
parent: Reference
layout: default
nav_order: 9
---

# Feature Flags

Precise surface for the feature-flag subsystem. For why it is shaped this way see [Feature Flags]({% link _explanation/feature-flags.md %}); for the task see [Gate a Capability]({% link _howto/gate-a-capability.md %}).

## Declaration

A feature is declared once. `key` and `displayName` are required.

| Field | JSON | Meaning |
|-------|------|---------|
| `Key` | `key` | What call sites gate on, after the `feature.` prefix |
| `DisplayName` | `displayName` | What an operator reads in the CLI and admin |
| `Description` | `description` | Optional prose for the same readers |
| `Default` | `default` | The value when no layer has spoken. **Not** the same as an explicit off |
| `Manageable` | `manageable` | An account admin may override it. When false, a stored account override is ignored |
| `Grantable` | `grantable` | It may be granted to an agent or a role. When false, a stored grant is ignored |

Three sources, all read at boot:

| Source | How |
|--------|-----|
| Code | `fx.Provide(application.AsFeatureDeclarations(func() []entities.FeatureMeta { … }))` |
| Preset | `PresetDefinition.Features` — swept on every lookup, so installing a preset declares its features immediately |
| Configuration | the `FEATURES` environment variable |

A duplicate key is a boot error, not a last-wins. Two declarations of one key means two subsystems each believe they own it. A code declaration wins a collision with a *preset* (and logs it); a `FEATURES` entry that agrees with a code declaration about `default`, `manageable` and `grantable` is a no-op with a warning, and one that disagrees is a boot error naming both sides.

## Resolution

`ResolveFeature(meta, instance, account, granted)` returns the value and the deciding layer. Stored layers are tri-state: on, off, or unset (no row).

| Instance | Account | Grant | Result | Layer |
|----------|---------|-------|--------|-------|
| unset | unset | no | the declared `default` | `default` |
| **off** | anything | anything | **off** | `instance` |
| on | unset | no | on | `instance` |
| on | **off** | anything | **off** | `account` |
| unset | on | no | on | `account` |
| unset | unset | yes | on | `grant` |
| unset | **off** | yes | **off** | `account` |

`manageable: false` makes the account column read as unset. `grantable: false` makes the grant column read as no.

A store that cannot be read resolves **off**, with layer `error`. A key nobody declared answers the **caller's own default** and is logged once.

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `FEATURES` | *(empty)* | JSON array of declarations, same shape as a code declaration |
| `FEATURE_CACHE_MAX_AGE_SECONDS` | `900` | How long a resolved set may be served before re-resolution |
| `FEATURE_PRIMARY_ACCOUNT_ID` | *(empty)* | Which account's owners and admins may change instance-level settings |
| `FEATURE_NOTIFY_CHANNEL` | `weos_feature_cache` | Postgres `LISTEN/NOTIFY` channel for cross-replica cache invalidation |

```bash
FEATURES='[{"key":"ledger-export","displayName":"Ledger export","default":false,"manageable":true,"grantable":true}]'
```

A malformed `FEATURES` value stops the boot. Declaring nothing silently would look exactly like a working instance with every feature off.

`FEATURE_PRIMARY_ACCOUNT_ID` matters on multi-account instances. Instance-level writes require an owner or admin **of the instance account**; without this variable, a single-account instance uses that account and a multi-account instance refuses instance-level writes rather than guessing.

## CLI

```
weos feature list                      # every feature, its value, and the layer that decided it
weos feature enable <key>              # instance-level on
weos feature disable <key>             # instance-level off
weos feature reset <key>               # remove the instance override (not the same as disable)
weos feature grant <key>   --email <address> | --role <owner|admin|member> [--account <id>]
                           [--valid-from <RFC3339>] [--valid-until <RFC3339>]
weos feature revoke <key>  --email <address> | --role <role> [--account <id>]
weos feature grants <key>  [--account <id>]
```

`--account` is required on an instance with more than one account. Every command needs an explicit database DSN.

## HTTP API

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| `GET` | `/api/features` | optional | The caller's resolved set. A caller with no session is answered the instance view with HTTP 200 |
| `PUT` | `/api/features/{key}/instance` | operator | `{"enabled": true\|false}` — the field is required |
| `DELETE` | `/api/features/{key}/instance` | operator | Reset to unset |
| `PUT` | `/api/features/{key}/account` | account admin | `{"enabled": true\|false}` |
| `DELETE` | `/api/features/{key}/account` | account admin | Reset to unset |
| `GET` | `/api/features/{key}/grants` | account admin | Who holds it, with each grant's window and state |
| `POST` | `/api/features/{key}/grants` | account admin | `{"email"\|"role", "validFrom", "validThrough"}` |
| `DELETE` | `/api/features/{key}/grants` | account admin | Take one back |
| `GET` | `/api/features/grants?email=` | account admin | Everything one person holds, directly or by role |

`GET /api/features` returns one entry per declared feature:

```json
{
  "data": [
    {
      "key": "agent-chat",
      "displayName": "Assistant",
      "description": "Talk to this instance's assistant",
      "enabled": true,
      "source": "instance",
      "default": true,
      "manageable": true,
      "grantable": true
    }
  ]
}
```

`source` is `default`, `instance`, `account`, `grant`, or `error`. It never carries anybody's grant rows.

When the store cannot be read, the listing answers **200 with every feature off**, `source: "error"`, and a warning in `messages`. It agrees with the gates during an outage rather than leaving a client to invent a policy.

## MCP tools

Available on the HTTP transport by default. On stdio they are **opt in** — `weos mcp --services resource,feature` — because that transport is trusted without a permission check.

| Tool | Purpose |
|------|---------|
| `feature_list` | Every feature, resolved for the caller, with its deciding layer |
| `feature_set` | Instance or account level; `enabled` is required |
| `feature_reset` | Return a layer to unset |
| `feature_grant` | Grant to a person or a role, with an optional window |
| `feature_revoke` | Take a grant back |
| `feature_grants` | Who holds a feature |

## Grant states

A listing reports each grant's window state:

| State | Meaning | What to do |
|-------|---------|-----------|
| `active` | Applies now | — |
| `pending` | `validFrom` has not arrived | Wait, or revoke |
| `expired` | `validThrough` has passed | The grant ran its course; the row can be cleaned up |
| `orphaned` | The subject is no longer a member of the account | Left-over access; revoke it |

## Go API

| Symbol | Package | Purpose |
|--------|---------|---------|
| `AsFeatureDeclarations` | `application` | Contribute declarations to the fx value group |
| `ToolFeatureGate(client)` | `application` | Build a `func(ctx, key) bool` over the OpenFeature client |
| `SkillFeatureGate(client, registry, logger)` | `application` | The same, for agent skills, naming the skill in the drift log |
| `FeatureFlagPrefix` | `application` | `"feature."` — the key namespace |
| `FeatureProviderDomain` | `application` | `"weos"` — the OpenFeature domain |
| `AddGatedTool` | `pkg/cli` | Add an MCP tool and its gate in one statement |
| `MCPFeatureGates` | `pkg/cli` | The gate index handed to a configurer as `deps.Gates` |
| `RequireFeature(gate, key)` | `api/middleware` | Echo middleware refusing a route with 403 |
| `SetSkillGate` | `application/agents` | Wire the skill gate onto the orchestrator |
| `RoutableSkills(ctx)` | `application/agents` | The skills a caller can be routed to |
| `GateRefusal(ctx, subject, key)` | `domain/entities` | The shared refusal wording |
| `FeatureCacheInvalidator` | `domain/repositories` | `InvalidateAll` / `InvalidateAccount` / `InvalidateAgents` |

## Admin SPA

`useFeatures()` exposes the set to components.

| Member | Purpose |
|--------|---------|
| `isEnabled(key)` | Whether a feature is on for the signed-in person |
| `ensureLoaded()` | Fetch once per app boot |
| `refresh()` | Fetch again because the caller changed |
| `unavailable` | The set could not be fetched at all |

An unknown key is **off** in the browser and **on** on the server. The server leaves a drifted capability where it was because closing it would be a silent outage; the browser draws nothing, because the server refuses it either way and a link that leads to a refusal is the worse mistake.

## Related

- [Feature Flags]({% link _explanation/feature-flags.md %})
- [Gate a Capability]({% link _howto/gate-a-capability.md %})
- [Environment Variables]({% link _reference/environment-variables.md %})
