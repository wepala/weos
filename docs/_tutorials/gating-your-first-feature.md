---
title: Gating Your First Feature
parent: Tutorials
layout: default
nav_order: 6
---

# Gating Your First Feature

In this tutorial you will take a capability WeOS already ships — the `episodic_recall` MCP tool — and watch it disappear and come back as you change one feature. Then you will grant a second feature to one person and watch an operator's decision overrule it.

By the end you will have seen every layer decide the same feature, and you will know how to read which one won.

Nothing here changes code. You are driving the switches an operator has.

**You will need:** a built binary (`make build`) and about fifteen minutes.

## 1. Start with a clean instance

```bash
cd services/core
rm -f tutorial.db
export DSN=./tutorial.db
```

Every command passes `--database-dsn $DSN`, so you are always working on this database and never on a real one.

## 2. See what the instance declares

```bash
./bin/weos feature list --database-dsn $DSN
```

```
KEY              NAME             STATE  SOURCE
agent-chat       Assistant        on     declared default
episodic-recall  Episodic recall  on     declared default
```

Two things to notice. Both are **on**, and the source is **declared default** — nobody has stored anything, so the value is the one the declaration carries.

That is not the same as *on*. It means "nothing has spoken yet", and it matters in step 5.

## 3. Watch a tool disappear

`episodic-recall` gates a real MCP tool. Ask the server for its tool list:

```bash
ask_mcp() {
  { printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"tutorial","version":"1"}}}'
    printf '%s\n' "$1"
    perl -e 'select(undef,undef,undef,3)'   # hold stdin open while the server answers
  } | DATABASE_DSN=$DSN ./bin/weos mcp 2>/dev/null
}

ask_mcp '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | grep -c episodic_recall
```

You get a non-zero count — the tool is there. Now turn the feature off:

```bash
./bin/weos feature disable episodic-recall --database-dsn $DSN
ask_mcp '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | grep -c episodic_recall
```

`0`. The tool is **gone** — not disabled, not greyed out, absent. A model that cannot see a tool cannot propose it.

## 4. Confirm the half that matters

A client could still have an older list. Call the tool by name anyway:

```bash
ask_mcp '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"episodic_recall","arguments":{}}}' | tail -2
```

```json
{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text",
 "text":"episodic_recall is not available: the \"episodic-recall\" capability is not enabled on this server."}],
 "isError":true}}
```

**This is the control.** The missing list entry was only a courtesy — a stale tool list is never authorization.

Notice the wording: *on this server*, not *for you*. The stdio transport has no caller identity, so gating there is instance-wide and no personal grant could change it. Over HTTP, where there is a caller, the same refusal says "for you".

## 5. Learn what `reset` does

```bash
./bin/weos feature list --database-dsn $DSN
```

`episodic-recall` is now `off`, source `instance override`. There are two ways back:

```bash
./bin/weos feature reset episodic-recall --database-dsn $DSN
./bin/weos feature list --database-dsn $DSN     # on, "declared default"

./bin/weos feature disable episodic-recall --database-dsn $DSN
./bin/weos feature enable  episodic-recall --database-dsn $DSN
./bin/weos feature list --database-dsn $DSN     # on, "instance override"
```

The values match; the meanings do not. **Reset** returns the layer to silence, so an account or a grant can decide. **Enable** stores an explicit yes. Reset it again before moving on:

```bash
./bin/weos feature reset episodic-recall --database-dsn $DSN
```

## 6. Declare a feature of your own

Declarations are never stored, so every command has to read the same ones. Export it once:

```bash
export FEATURES='[{"key":"ledger-export","displayName":"Ledger export","default":false,"manageable":true,"grantable":true}]'
./bin/weos feature list --database-dsn $DSN
```

```
ledger-export    Ledger export    off    declared default
```

Declared off — but nobody has said no. That distinction is about to earn its keep.

## 7. Grant it to one person

Grants live inside an account, so create one:

```bash
echo "correct-horse-battery-staple" | \
  ./bin/weos account create --email ada@example.com --password-stdin --database-dsn $DSN

./bin/weos feature grant ledger-export --email ada@example.com --database-dsn $DSN
```

```
Granted "ledger-export" to ada@example.com in account 3IC0zPjK….
  window: —   status: active
```

The grant reaches her, because nothing above it said no.

## 8. Watch an explicit off beat the grant

```bash
./bin/weos feature disable ledger-export --database-dsn $DSN
./bin/weos feature grants ledger-export --database-dsn $DSN
```

```
SUBJECT          WINDOW  STATUS  GRANTED BY
ada@example.com  —       active  —
```

The grant is still there and still `active` — and it no longer reaches her, because **an explicit off is final downward**. An operator's no is a statement about what this instance does, and a grant cannot argue with it.

This is the most useful rule to carry away. When a grant seems to do nothing, look at the deciding layer:

```bash
./bin/weos feature list --database-dsn $DSN | grep ledger
# ledger-export    Ledger export    off    instance override
```

## 9. Give access that ends by itself

```bash
./bin/weos feature reset  ledger-export --database-dsn $DSN
./bin/weos feature revoke ledger-export --email ada@example.com --database-dsn $DSN
./bin/weos feature grant  ledger-export --email ada@example.com \
  --valid-until "$(date -u -v+1M +%Y-%m-%dT%H:%M:%SZ)" --database-dsn $DSN
```

```
  window: until 2026-08-20T19:19:34Z   status: active
```

Wait for the minute to pass and list the grants again — the status is now `expired`.

Nothing ran to expire it. There is no sweeper and no scheduled job; the row is simply not counted once the moment passes. That is why access ends exactly on time without anything being scheduled — and also why nothing can announce the moment it ends.

## Clean up

```bash
rm -f tutorial.db
unset FEATURES DSN
```

## What you learned

- A feature has four layers, and the listing always names the one that decided.
- `declared default` means nothing has spoken; **off** means something has.
- An explicit off is final downward. An explicit on is not.
- Hiding a capability is a courtesy; refusing the call is the control.
- Refusals say whose answer they are, so nobody chases a grant that cannot apply.
- Grants can carry a window, and expiry costs nothing because nothing watches for it.

## Next

- [Gate a Capability]({% link _howto/gate-a-capability.md %}) — put one of your own capabilities behind a flag
- [Feature Flags]({% link _explanation/feature-flags.md %}) — why the layers behave this way
- [Feature Flags reference]({% link _reference/feature-flags.md %}) — every command, endpoint and variable
