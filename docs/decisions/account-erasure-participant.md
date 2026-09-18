---
title: "ADR: An Account Erasure Participant"
parent: Architecture Decision Records
layout: default
nav_order: 11
---

# ADR: An Account Erasure Participant

**Status:** Accepted (Implemented)
**Date:** 2026-09-17
**Ticket:** bead `wm-j2sg5`
**Base:** `v3` (the integration branch the `v3.0.1-beta.*` tags are cut from; `main` is the old line)

## Context and Problem Statement

`AccountErasureService.Erase` runs the whole deletion of an account — lock, drain,
enumerate, file folder, graph, SQL purge — and has no extension point. The decision
that built it is [What Core Carries for WeHungry]({% link decisions/wehungry-food-types-uploads-and-account-erasure.md %});
`AccountErasureDeps` is a fixed struct.

Two products built on core need a step of their own inside that sequence, and core
cannot carry either one:

- A deployment that links accounts to a bank aggregator has to remove the aggregator's
  items and the credentials they were linked with. The account's rows are what name
  those items, so the step has to run while they are still there.
- A deployment that signs people in with an identity provider has to revoke the
  provider's token for the person who is leaving. Apple's App Review requires it of an
  app that offers account deletion.

Neither belongs in core: core does not know about aggregators or about any one
provider's revocation endpoint, and `domain/` may not import outward (Article II).
Both have the same shape — one step, run inside the deletion, before the account's
data goes, able to fail the deletion.

The alternative available today is for the embedding service to do the work in its own
handler before calling `DELETE /api/account`. That is what makes this a decision
rather than a convenience: a step outside `Erase` runs outside the lock, so a request
admitted between the two can link the account to something new that the deletion then
never unlinks, and a step that fails still lets the deletion succeed — the account is
gone and the external link is stranded, with nothing left to find it by.

## Decision Drivers

- The step must run before anything of the account's is removed, so it can still read
  the account's rows, files and graph.
- It must run after the lock, so nothing new can be linked while it works.
- It must be able to fail the erasure. A stranded external link is worse than a
  deletion the person runs again.
- The synchronous guarantee has to stay: `AccountHandler.Delete` answers 200 only
  after `Erase` returns, and that 200 must keep meaning every step completed.
- The seam must carry the embedding service's own dependencies, which live in its own
  Fx graph, and core must import nothing to support it.
- One mechanism serves both consumers, and any later one.

## Considered Options

1. **An Fx value group of participants collected by the erasure service** — a
   downstream binary provides a constructor annotated into the group, and the service
   runs the group inside `Erase`.
2. **A process-global registry**, as `RegisterEchoConfigurer` and
   `RegisterMCPConfigurer` do — the binary appends a function in an `init()`.
3. **An event the embedding service subscribes to** — `Erase` emits
   `account.erasing` and a subscriber acts on it.
4. **Nothing in core; each service does its own step before calling the API** — the
   status quo.

## Decision

**Option 1.** `application.AccountErasureParticipant` is an interface with `Name()` and
`BeforeAccountErased(ctx, ErasingAccount)`. `AccountErasureParams` collects the
`account_erasure_participants` value group, and
`application.AsAccountErasureParticipant` (plus `…Participants` for a flattening
provider) annotates a constructor into it — the same shape as `AsSubscriberGroup`
(`application/worker_providers.go`) and `AsFeatureDeclarations`
(`application/feature_registry.go`), which is core's established seam for out-of-tree
contributions. `AccountErasureDeps.Participants` is the hand-wiring equivalent.

**Where it runs.** Inside `Erase`, after the lock and deactivation and after the
drain, immediately before `sweep`. The lock is what stops anything new being linked
while the step works; the drain is what makes the read model complete, so a
participant that reads a projection sees a link made seconds before the deletion; and
`sweep` is the first step that removes anything, so the account's data is whole in
front of the participant. 
**What the sweeps owe a participant.** The participants run before **every** sweep
that removes rows, not only the first. `Erase` sweeps again for rows a request
admitted just before the lock committed after the purge (wm-mnry2), and a row that
landed that way can name something outside this instance exactly as the first ones
did — so removing it without asking the participants would strand the external link
in precisely the race the lock exists to close. The rule is one invariant: no row
naming the account is removed without the participants having been asked since it
landed. `ErasingAccount.Pass` tells a participant which pass it is on.

The same invariant settles the other sweep. When the account's own row is already
gone — an earlier deletion finished and left rows behind — the participants still
run, with no lock (there is no row to hang one off) and with most of the account's
data already purged; `ErasingAccount.AccountGone` says so. The alternative, skipping
them there, would remove those rows with nothing asked about what they name.

The cost is that a participant can be asked two or three times in one deletion and
may find nothing to work from, so the contract is stated the other way round from
"idempotent": **work already done, and nothing left to do, are both success**. A
participant that reports "already unlinked" or "no rows found" as an error wedges the
deletion and the cleanup behind it.

**What an error does.** The first participant to return an error stops the erasure with
`ErrErasureParticipantFailed`, wrapping the participant's name and its error. Nothing
has been removed, the account is left locked and inactive, and the handler answers 500
`account_erasure_unfinished` as it does for any other failed step. Running the deletion
again runs every participant again, so a participant is idempotent like every other
step of the sequence.

A participant that panics fails the same way: the panic is contained at the call,
logged with its stack, and returned as `ErrErasureParticipantFailed` naming the step.
Core does not sandbox a participant, which is the argument for containing it here —
an escaping panic would drop the caller's connection with no answer at all, name no
step in the log, and kill an operator's command mid-run.

**Where a participant is registered.** `cli.RegisterErasureFxOptions`, not
`cli.RegisterFxOptions`. The second is merged into the server's graph only, and
`account delete` builds its own graph — so a participant registered there would run
for a deletion asked for through the API and would be skipped when an operator
finishes that same deletion from the command line, which is the documented remedy for
a deletion that failed part-way. The two lists are kept apart rather than merged
because `RegisterFxOptions` is also where a binary starts its background work —
sweeps and pollers hung off `fx.Lifecycle` — and none of that belongs in a command
that opens the store, erases one account and stops. `account delete` names the steps
it will run before it starts, so an instance whose steps went to the wrong list reads
as "none registered" rather than erasing silently without them.

**Order.** dig shuffles the members of a value group deliberately
(`shuffledCopy`, dig v1.18), so the container path sorts participants by `Name` —
stable across processes and restarts, which is what makes a failed deletion reproduce
the same way twice. `AccountErasureDeps.Participants` runs in the order of the slice.
Participants are independent by design; a step that must follow another is one
participant that runs both.

## Rejected Options

- **The process-global registry (2)** is core's precedent for Echo routes and MCP
  tools, and it would give literal registration order. It was not taken because those
  configurers are handed their dependencies as arguments at call time, and a
  participant's dependencies — an aggregator client, a credential store — are the
  binary's own Fx-built services. A global would make the binary capture them from an
  `fx.Invoke` into a package variable, which is the pattern `RegisterEchoConfigurer`'s
  own doc comment describes as a caveat rather than a goal. Order was the only thing
  it bought, and ordering participants against each other is not a thing this seam
  should encourage.
- **An event (3)** breaks the requirement outright. A subscriber runs after the
  commit, asynchronously, and cannot fail the erasure; the drain would also have to
  wait for it, which is the same problem the erasure already solves for projections.
- **The status quo (4)** is what this record exists to replace; the two failure modes
  are in the problem statement.

## Consequences

- An instance that registers no participant runs exactly the sequence it ran before —
  the value group is empty and `runParticipants` is a no-op loop. The existing erasure
  tests are unchanged, which is the evidence for it.
- A participant is inside the erasure's 15-minute deadline and shares it with the
  bucket walk, and each step is additionally bounded by
  `ACCOUNT_ERASURE_PARTICIPANT_TIMEOUT_SECONDS` (default 2m) so one step that hangs on
  an external API cannot spend the whole budget — while it hangs, every retry is
  answered "a deletion is already running". A participant that ignores its context can
  still hang; nothing can preempt it, so each step is logged as it starts and not only
  when it finishes, which is what lets an operator name the one that is stuck.
- A deadline that passes between two steps is reported as a deadline, naming the step
  that spent the budget rather than the one that had not started. Blaming a step that
  never ran sent an operator to debug a healthy client mid-incident.
- A participant sees the account's data and is trusted with it. It is the embedding
  binary's own code, registered in its own graph — core neither validates nor sandboxes
  it.
- `ErrErasureParticipantFailed` is a new error the handler surfaces through the
  existing `account_erasure_unfinished` path; no new API response shape.
- Two consumers follow: the bank-link removal recorded on bead `wm-ujhy5.4`, and the
  identity-token revocation recorded on bead `wm-hhi0t`. Both wait on a tag of this
  change.

## References

- [What Core Carries for WeHungry — the Food Types, a Folder per Account, and Erasure of an Account]({% link decisions/wehungry-food-types-uploads-and-account-erasure.md %}) — the erasure sequence this extends.
- `application/account_erasure_participant.go` — the interface, the registration helpers, and the run.
- `application/worker_providers.go` (`AsSubscriberGroup`), `application/feature_registry.go` (`AsFeatureDeclarations`) — the registration precedent.
- `constitution.md`, Article II (dependencies point inward) and Article X (this record).
