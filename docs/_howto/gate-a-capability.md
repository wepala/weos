---
title: Gate a Capability
parent: How-to Guides
layout: default
nav_order: 10
---

# Gate a Capability

This guide shows how to put one of your own capabilities behind a feature flag, on each of the four surfaces WeOS gates: an MCP tool, an agent skill, an HTTP route, and an admin UI section.

Read [Feature Flags]({% link _explanation/feature-flags.md %}) first if you have not — the precedence rules and the fail-closed behaviour explain why the steps below are the shape they are.

## Prerequisites

- A working WeOS build (`make build` succeeds)
- A capability you want to gate

## 1. Declare the feature

Nothing resolves for a key nobody declared — it answers the caller's own default and logs once. Declare it before you gate on it.

From a downstream binary, contribute to the fx value group:

```go
package main

import (
    "go.uber.org/fx"

    "github.com/wepala/weos/v3/application"
    "github.com/wepala/weos/v3/domain/entities"
    "github.com/wepala/weos/v3/pkg/cli"
)

func init() {
    cli.RegisterFxOptions(
        fx.Provide(application.AsFeatureDeclarations(func() []entities.FeatureMeta {
            return []entities.FeatureMeta{{
                Key:         "invoice-export",
                DisplayName: "Invoice export",
                Description: "Export invoices to a spreadsheet.",
                Default:     false,
                Manageable:  true,
                Grantable:   true,
            }}
        })),
    )
}
```

From a preset, use `PresetDefinition.Features`. Without a build at all, set `FEATURES`:

```bash
FEATURES='[{"key":"invoice-export","displayName":"Invoice export","default":false,"manageable":true,"grantable":true}]'
```

### Choosing the default

Pick `default: true` for a capability that **already ships**. Introducing a gate with the default off takes a working capability away from every instance that upgrades, and nobody finds out until somebody complains.

Pick `default: false` for something new, or something an instance should opt into.

### Choosing `manageable` and `grantable`

`manageable: false` means only the operator decides — right for anything that costs money or carries compliance weight. `grantable: false` means it cannot be given to one person — right for anything that only makes sense instance-wide.

## 2. Gate the capability

Declare the gate at the call site of the thing it gates. A capability declared in one file and gated in a registry somewhere else is two things that will drift.

### An MCP tool

```go
cli.RegisterMCPConfigurer(func(server *mcp.Server, deps cli.MCPConfigurerDeps) {
    cli.AddGatedTool(server, deps.Gates, "invoice-export", &mcp.Tool{
        Name:        "invoice_export",
        Description: "Export invoices to a spreadsheet.",
        Annotations: /* … */,
    }, invoiceExportHandler)
})
```

The tool is then absent from `tools/list` for a caller whose feature is off, and a call to it is refused with an error the model can read. A tool added with plain `mcp.AddTool` is ungated and behaves exactly as before — no lookup, no new failure mode.

### An agent skill

Name the feature in the skill's own definition:

```json
{
  "schemaVersion": 1,
  "name": "invoice_reporter",
  "description": "Answers questions about invoices",
  "instructions": "…",
  "tools": ["invoice_export"],
  "mode": "task",
  "gatedBy": "invoice-export"
}
```

The skill is then not offered to the coordinator, so `transfer_to_agent` cannot reach it, and invoking it directly is refused.

Note that a skill's power is its tools, and every gated tool gates independently against the same caller. A skill that somehow becomes reachable can still run nothing the caller's features do not already allow.

### An HTTP route

```go
gate := apimw.RequireFeature(application.ToolFeatureGate(featureClient), "invoice-export")

group.POST("/invoices/exports", exportHandler, gate)
```

The route answers 403 with the feature named. Put the feature gate **before** any role check: a capability that is off is off for its owner too, and answering "you are not allowed" to somebody who would be allowed if it were on sends them to fix the wrong thing.

### An admin UI section

```vue
<script setup lang="ts">
const { isEnabled } = useFeatures()
const showInvoices = computed(() => isEnabled('invoice-export'))
</script>

<template>
  <a-menu-item v-if="showInvoices" key="invoices">
    <NuxtLink to="/invoices">Invoices</NuxtLink>
  </a-menu-item>
</template>
```

The set is fetched once per app boot and shared, so reading it in a component costs nothing.

**Hiding is presentation, never a control.** Anybody can type the address. Always pair a hidden section with a gated route — the section is a courtesy that stops people clicking a link that leads to a refusal.

## 3. Turn it on and off

```bash
weos feature list --database-dsn ./weos.db
weos feature enable invoice-export --database-dsn ./weos.db
weos feature disable invoice-export --database-dsn ./weos.db
weos feature reset invoice-export --database-dsn ./weos.db
```

`reset` is not `disable`. Disabling stores an explicit off that no account and no grant can lift; resetting returns the layer to silence so a lower layer can speak.

## 4. Grant it to somebody

```bash
weos feature grant invoice-export --email auditor@example.com --account acct-123 \
  --valid-until 2026-09-30T00:00:00Z --database-dsn ./weos.db

weos feature grants invoice-export --account acct-123 --database-dsn ./weos.db
weos feature revoke invoice-export --email auditor@example.com --account acct-123 --database-dsn ./weos.db
```

Grant to a role with `--role owner|admin|member` instead of `--email`.

A grant cannot lift an explicit off at the instance or account level. If a grant appears to do nothing, run `weos feature list` and look at the deciding layer.

## 5. Check what a caller actually gets

```bash
curl -s localhost:8080/api/features | jq '.data[] | {key, enabled, source}'
```

`source` tells you which layer decided, which is usually the answer to "why is this off?".

## Testing a gated capability

Write the scenario that hides it **and** the scenario that calls it anyway. A test that only checks the listing passes against an implementation with no control at all, and the gap stays invisible until somebody uses a name they already had.

Prove the cost too, if the capability sits on a hot path: a caller's whole set resolves once, so a turn making thirty calls should read the database no more often than one making none.

## Common problems

**The gate does nothing.** Check the key is declared — `weos feature list` shows every declared feature. An undeclared key answers the caller's default and logs once, so grep the logs for `nobody declared`.

**A grant has no effect.** Check the deciding layer. An explicit off above it wins, and `grantable: false` makes the grant unreadable.

**A change did not reach somebody.** Writes invalidate caches immediately, but a resolved set is served for up to 15 minutes otherwise, and a separate process (`weos mcp`) has its own cache. Lower `FEATURE_CACHE_MAX_AGE_SECONDS` if you need tighter propagation.

**Instance-level writes are refused on a multi-account instance.** Set `FEATURE_PRIMARY_ACCOUNT_ID` to the account whose owners and admins may change instance settings.

**A local `weos mcp` session ignores a grant.** It cannot do otherwise: stdio has no caller identity, so gating there is instance-wide.

## Related

- [Feature Flags]({% link _explanation/feature-flags.md %}) — why the layers behave as they do
- [Feature Flags reference]({% link _reference/feature-flags.md %}) — every flag, endpoint and command
- [Create a Behavior]({% link _howto/create-behavior.md %}) — behaviors are a different mechanism; they fail open, features fail closed
