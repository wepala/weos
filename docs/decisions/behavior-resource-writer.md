---
title: "ADR: Cross-Resource Writes from ResourceBehavior Hooks"
parent: Architecture Decision Records
layout: default
nav_order: 4
---

# ADR: Cross-Resource Writes from ResourceBehavior Hooks

**Status:** Accepted (Implemented)
**Date:** 2026-04-07
**Context:** With the factory-based service injection from the [Injecting Services into ResourceBehavior]({% link decisions/behavior-service-injection.md %}) ADR, behaviors can now hold references to application services. This ADR decides **which** service behaviors use when they need to create, update, or delete *other* resources, and how that dependency is wired without breaking Fx construction ordering.

## Problem

Presets increasingly need reactive logic that spans more than one resource type. A concrete driver: porting the legacy `education` preset from ic-crm ([`wepala/weos-private-presets#1`](https://github.com/wepala/weos-private-presets/issues/1)) requires behaviors that:

- create N `attendance-record`s when an `enrollment` is created
- generate `education-event`s from a `course-instance`'s schedule
- delete all `attendance-record`s when an `education-event` is deleted
- mark an `invoice` as `paid` when a `payment` arrives

The previous ADR added a `BehaviorServices` struct with `Resources` (`ResourceRepository`), `Triples` (`TripleRepository`), `ResourceTypes` (`ResourceTypeRepository`), and `Logger`. These are sufficient for **reads**, but not for creating, updating, or deleting other resources.

### Why `ResourceRepository.Save()` is the wrong answer

`BehaviorServices.Resources` is a `ResourceRepository`, which does expose `Save`, `Update`, `Delete`. But those methods take already-constructed `*entities.Resource` values and write them to the projection/event store directly. Using them from a behavior would bypass:

- Schema validation (`validateAgainstSchema`)
- JSON-LD graph assembly (`BuildResourceGraph`)
- Reference extraction into triple events (`ExtractAndStripReferences`)
- `Resource.Published` signal recording
- `BeforeCreate` / `BeforeCreateCommit` / `AfterCreate` hooks on the target type
- UnitOfWork transactional commit with atomic triple events

Every one of those is load-bearing. Skipping them would corrupt projections, lose references, and silently break other behaviors.

The correct write path is `application.ResourceService.Create` / `Update` / `Delete`, which runs the full pipeline. The question is how to hand that to behaviors.

### Why we can't put `ResourceService` directly in `BehaviorServices`

`resourceService` holds a `ResourceBehaviorRegistry` (the `behaviors` field in `application/resource_service.go`) so it can dispatch hooks. `ResourceBehaviorRegistry` is built by `ProvideResourceBehaviorRegistry`, which (after the factory ADR) invokes each preset factory with a `BehaviorServices`. If `BehaviorServices` carried a `ResourceService`, Fx would need:

```
ResourceService  →  ResourceBehaviorRegistry  →  BehaviorServices  →  ResourceService
```

A cycle. Fx detects this at container build and fails.

We need a way to give behaviors a full-pipeline write capability without creating that construction cycle.

## Options

---

### Option 1: Expose the full `ResourceService` via a lazy proxy

Define a narrow `ResourceWriter` interface containing just `Create`, `Update`, `Delete` — the subset behaviors actually need. Add a `lazyResourceWriter` struct that satisfies `ResourceWriter` but starts with a nil inner target. Construct it early, hand it to `BehaviorServices`, build the registry with factories closing over it. Then, after `ProvideResourceService` has returned the real service, run an `fx.Invoke` (`WireResourceWriter`) that sets the proxy's target. By the time any hook fires during a request, the proxy forwards to the real service.

**Example:**
```go
type ResourceWriter interface {
    Create(ctx context.Context, cmd CreateResourceCommand) (*entities.Resource, error)
    Update(ctx context.Context, cmd UpdateResourceCommand) (*entities.Resource, error)
    Delete(ctx context.Context, cmd DeleteResourceCommand) error
}

type lazyResourceWriter struct {
    svc ResourceWriter // set post-construction
}

func (l *lazyResourceWriter) Create(ctx context.Context, cmd CreateResourceCommand) (*entities.Resource, error) {
    if l.svc == nil {
        return nil, fmt.Errorf("ResourceWriter.Create called before wiring")
    }
    return l.svc.Create(ctx, cmd)
}
// Update, Delete similarly.

func (l *lazyResourceWriter) SetTarget(svc ResourceWriter) { l.svc = svc }
```

Fx wiring in `application/module.go`:
```go
fx.Provide(newLazyResourceWriter),
fx.Provide(ProvideResourceBehaviorRegistry),       // receives *lazyResourceWriter
// ...
fx.Provide(ProvideResourceService),
fx.Invoke(WireResourceWriter),                      // svc -> proxy
```

Behaviors close over the proxy via `BehaviorServices.Writer`:
```go
func newEnrollmentBehavior(s application.BehaviorServices) entities.ResourceBehavior {
    return &enrollmentBehavior{writer: s.Writer, resources: s.Resources, logger: s.Logger}
}
```

**Pros:**
- **Breaks the cycle cleanly.** Construction order is `proxy → registry → service → invoke wires proxy`. No Fx-visible cycle.
- **Preserves full semantics.** Writes go through schema validation, graph assembly, triple extraction, event recording, UoW commit, and nested behavior dispatch. The target type's own behaviors fire naturally — a course-instance behavior creating education-events correctly triggers the education-event behavior that seeds attendance.
- **Narrow interface.** `ResourceWriter` has three methods. Behaviors never see `List*` / `GetByID` via the writer; they use `BehaviorServices.Resources` for reads, which is already the correct separation.
- **Fails loudly on misuse.** If a factory or something else accidentally invokes a hook during startup (before `WireResourceWriter` runs), the proxy returns a clear error instead of a nil-pointer panic.
- **Zero changes to existing behaviors.** `personBehavior` and `organizationBehavior` ignore `Writer` — they continue to work unchanged. `StaticBehavior` still hides service plumbing for pure-transform hooks.
- **Idiomatic Go proxy pattern.** The two-phase setup is an explicit, readable seam, not hidden magic.

**Cons:**
- **Two-phase construction is explicit.** There's a brief window — between `newLazyResourceWriter` returning and `WireResourceWriter` running — when calls through the proxy fail. This is an intentional trade-off: it's how we break the cycle. The failure mode is clear and the window is startup-only.
- **Mutable state in a singleton.** The proxy's target is set exactly once, but it is mutated after construction. This can look surprising compared to fully immutable DI. Mitigation: `SetTarget` is doc'd as one-shot, `WireResourceWriter` is the only caller in production code.
- **Slight indirection at runtime.** Every write through a behavior pays one extra interface hop. Negligible compared to the work the real service does.
- **Requires a recursion guard.** Now that behaviors can create resources, we have to bound cascades. This is actually a benefit (see §"Recursion guard" below), but it's a new piece of surface area.

---

### Option 2: Split `resourceService` into an author + a behavior dispatcher

Factor `resourceService` into two pieces: a `resourceAuthor` that does the full write pipeline (validation, graph assembly, event recording, UoW commit) **without** calling any behaviors, and a thin `resourceService` that wraps it and layers behavior dispatch on top. `BehaviorServices.Writer` gets the `resourceAuthor`. Because the author has no behavior dependency, there is no cycle.

**Pros:**
- **No lazy state.** The author is fully constructed, nothing is set after the fact.
- **Architecturally cleaner.** Separates "write a resource" from "run user-supplied hooks around writes".
- **No fx.Invoke dance.** Standard constructor injection.

**Cons:**
- **Kills nested behaviors.** If a course-instance behavior creates an education-event via the author, the education-event's own behavior never fires. That means the education-event behavior would not create attendance records when the course-instance behavior generates events. We would need to re-implement the entire cascade inside the course-instance behavior, manually invoking every downstream behavior. This is exactly the logic we want to avoid writing by hand — it's the whole reason behaviors exist.
- **Silent semantic divergence.** Writes initiated by a user through the HTTP API run behaviors; writes initiated by a behavior do not. Two code paths with the same signature and different semantics is a bug magnet.
- **Double implementation.** The author needs its own test coverage of validation, graph assembly, triple extraction, etc. — a duplication of what `resourceService` already does.
- **More refactor surface.** Tests that construct `resourceService` have to decide which piece they want.

---

### Option 3: Provide the full `ResourceService` via lazy proxy (no interface narrowing)

Same as Option 1 but the proxy satisfies the full `ResourceService` interface (queries included), not a narrow `ResourceWriter`. Behaviors get one thing that does everything.

**Pros:**
- One service instead of two on `BehaviorServices` (the `Resources` repo becomes redundant for most purposes).
- Slightly less code in `resource_behaviors.go`.

**Cons:**
- **Blurs the read/write seam.** Reads should go through `Resources` (the repository, which respects visibility scopes via `FindAllByTypeWithFilters`'s scope parameter). Writes should go through the service (which enforces permissions via `checkInstanceAccess`). Merging them tempts behaviors to do both through the service and forget the scope arguments.
- **Wider surface for mistakes.** A behavior calling `ResourceService.List` inside a hook is almost always wrong — it's doing per-row work in a per-request context. Keeping the query methods out of `BehaviorServices.Writer` discourages this.
- **Ties `BehaviorServices` to the larger interface.** Every time `ResourceService` grows a method, the behavior surface grows with it.

---

### Option 4: Require the `education` preset (and anything like it) to subscribe to domain events instead of using behaviors

Accept that behaviors remain read-only + same-entity mutation. Cross-resource work goes through `Resource.Published` event subscribers (per the [Event Handler Data Availability ADR]({% link decisions/event-handler-data-availability.md %})). The preset would export a `Subscribe` function that `presets/register_custom.go` (or the core module) wires to the event dispatcher.

**Pros:**
- No changes to `BehaviorServices` or the behavior interface.
- Reuses the existing `Resource.Published` signal and event subscription infrastructure.
- Preset handlers get full DI via Fx.

**Cons:**
- **Splits preset logic across two mechanisms.** Type definitions + trivial transforms go in `Behaviors`; anything touching another resource goes in `Subscribe`. A preset author has to learn both models and keep them in sync.
- **Presets lose self-containment at the Fx level.** Subscribing to events requires an `fx.Invoke` or similar wiring outside the `PresetDefinition` struct. The private-preset package would need to export more than just `Register(registry)` — a real breaking change to the custom-preset contract.
- **Weaker story for account-level toggling.** `BehaviorMeta.Manageable` already gives per-account behavior on/off. Event subscribers don't benefit from that — we'd need to reinvent it.
- **Harder to test.** Event subscribers need the dispatcher and pericarp plumbing to exercise; behaviors can be called directly from a unit test.

---

## Comparison Matrix

| Criteria | Option 1: Lazy `ResourceWriter` | Option 2: Author split | Option 3: Lazy full `ResourceService` | Option 4: Event subscribers |
|---|---|---|---|---|
| **Breaks the construction cycle** | Yes (post-construct wire) | Yes (no dependency) | Yes (post-construct wire) | N/A (no dependency) |
| **Preserves nested behavior cascade** | Yes | No | Yes | N/A (behaviors unused) |
| **Write path runs full pipeline** | Yes | Yes (in author) | Yes | Yes (via service) |
| **Read/write API clearly separated** | Yes | Yes | No | N/A |
| **Changes to `BehaviorServices`** | Adds `Writer` field | Adds `Writer` field | Adds `Writer` field | None |
| **Changes to preset registration contract** | None | None | None | Yes — preset must export subscribers |
| **Changes to existing behaviors** | None | None | None | None |
| **Ease of writing an `education`-scale preset** | Direct: all logic in behaviors | Painful: cascade must be hand-rolled | Direct | Split across behaviors + handlers |
| **Failure mode on misuse** | Clear error (proxy not wired) | N/A | Clear error | N/A |
| **New machinery to maintain** | Proxy + 1 `fx.Invoke` + recursion guard | Two services with overlapping code | Proxy + 1 `fx.Invoke` + recursion guard | Event wiring in private-preset package |

## Decision

**Option 1 (lazy `ResourceWriter`).** It preserves nested behavior semantics — the whole reason we have behaviors — while cleanly breaking the construction cycle. The narrow interface keeps the write surface small and discourages behaviors from doing per-request queries through the service. Existing behaviors don't change; new behaviors opt into the `Writer` field only when they need it.

Option 2 was tempting for its architectural purity, but losing the nested cascade would force preset authors to reimplement the dispatch loop by hand — exactly the boilerplate that behaviors exist to eliminate.

Option 3 is a strict superset of Option 1 and adds nothing we need; the narrow interface is a better default.

Option 4 remains valid for *cross-aggregate* reactions spanning multiple UoW commits (the case the Event Handler Data Availability ADR was written for). It is not the right tool for the in-request, same-transaction cascades the `education` preset needs.

## Recursion guard

Now that behaviors can create resources, runaway cascades are a real risk. A bug in behavior A that (indirectly) triggers behavior A again would hang or blow the stack. `application/resource_service.go` tracks a behavior-cascade depth on `context.Context` via the unexported `enterResourceCall` helper and a `maxBehaviorRecursionDepth` constant; `Create`, `Update`, and `Delete` each call it at their top.

Two properties matter:

- **Sibling-safe accounting.** Because `context.Context` is immutable, two writes issued from the same hook both derive children at depth N+1 — not N+1 and N+2. Legitimate fan-out (an enrollment behavior creating many attendance records) does not inflate the counter.
- **Conservative ceiling.** The depth counter only tracks cascades within a *single* request-scoped `ResourceService` call. The deepest legitimate in-request cascade in the education preset is course-instance → education-event → attendance-record (3 levels, fired when a course-instance is created or updated). Payment-driven invoice settlement bottoms out at depth 2 (user write at 1 → `writer.Update(invoice)` at 2) and attendance-driven invoice generation has the same shape; neither nests under the schedule-generation cascade because each begins from a different root resource. The limit of 8 leaves generous headroom for future presets whose hooks chain further while still failing fast on cycles.

If a real-world preset needs deeper nesting, raising the constant is a one-line change and a new test. It is not a config knob because it should be a code-reviewed decision, not per-deployment tuning.

## Implementation

- [x] `application/resource_behaviors.go`: define `ResourceWriter`, `lazyResourceWriter`, `newLazyResourceWriter`, `WireResourceWriter`
- [x] `application/resource_behaviors.go`: add `Writer ResourceWriter` field to `BehaviorServices`
- [x] `application/resource_behaviors.go`: `ProvideResourceBehaviorRegistry` takes `*lazyResourceWriter` and passes it in `BehaviorServices`
- [x] `application/module.go`: `fx.Provide(newLazyResourceWriter)` + `fx.Invoke(WireResourceWriter)` after `ProvideResourceService`
- [x] `application/resource_service.go`: `enterResourceCall` + `maxBehaviorRecursionDepth` + calls at the top of `Create`, `Update`, `Delete`
- [x] `application/resource_behaviors_test.go`: test fixtures supply a fake or real `*lazyResourceWriter`
- [x] `application/resource_service_test.go`: direct tests for `enterResourceCall` (boundary, sibling immutability) and per-method guard enforcement
- [x] `docs/_howto/create-behavior.md`: §3a mentions `Writer` and shows a cross-resource example
- [ ] End-to-end cascade test once a consumer (e.g. the education preset) lands