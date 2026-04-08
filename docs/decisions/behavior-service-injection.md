---
title: "ADR: Injecting Services into ResourceBehavior Implementations"
parent: Architecture Decision Records
layout: default
nav_order: 3
---

# ADR: Injecting Services into ResourceBehavior Implementations

**Status:** Proposed
**Date:** 2026-04-07
**Context:** Preset-defined `ResourceBehavior` implementations need access to application services (repositories, domain services, loggers, HTTP clients, etc.) but are currently instantiated before Fx wires the container.

## Problem

Preset packages register `ResourceBehavior` implementations at package-init time via `application.PresetRegistry.MustAdd()` (see `application/presets/core/preset.go:17`). The `PresetDefinition.Behaviors` map stores fully-constructed behavior instances:

```go
Behaviors: map[string]entities.ResourceBehavior{
    "person":       &personBehavior{},
    "organization": &organizationBehavior{},
},
```

Because behaviors are zero-valued structs built during `init()`, they cannot hold references to anything that Fx wires later — repositories, loggers, the event dispatcher, HTTP clients, other services, etc. Today this is tolerable because the only behaviors in the codebase are pure data transforms (e.g. `personBehavior` concatenates `givenName + familyName`). As soon as a behavior needs to:

- query a related resource from the `ResourceRepository`
- read triples via the `TripleRepository`
- call another domain service
- publish a metric or log through the typed `entities.Logger`
- make an outbound HTTP call through an injected client

...there is no clean way to give the behavior what it needs. The current workarounds are all bad: package-level globals, service locator lookups inside hooks, or stuffing dependencies into `context.Context` ad hoc.

The `ResourceBehavior` interface (`domain/entities/resource_behavior.go:24`) currently looks like this:

```go
type ResourceBehavior interface {
    BeforeCreate(ctx context.Context, data json.RawMessage, rt *ResourceType) (json.RawMessage, error)
    BeforeCreateCommit(ctx context.Context, resource *Resource) error
    AfterCreate(ctx context.Context, resource *Resource) error
    BeforeUpdate(ctx context.Context, existing *Resource, data json.RawMessage, rt *ResourceType) (json.RawMessage, error)
    BeforeUpdateCommit(ctx context.Context, resource *Resource) error
    AfterUpdate(ctx context.Context, resource *Resource) error
    BeforeDelete(ctx context.Context, resource *Resource) error
    AfterDelete(ctx context.Context, resource *Resource) error
}
```

We need a way for preset authors to write behaviors that depend on services, without breaking the existing pure-transform behaviors or the preset registration model.

## Options

---

### Option 1: Variadic `services ...Service` Argument on Every Method

Add a variadic parameter to each method of `ResourceBehavior`. The service layer passes whatever services are relevant at each call site; behaviors that don't need services ignore the argument.

**Example:**
```go
type Service any // or a narrow marker interface

type ResourceBehavior interface {
    BeforeCreate(ctx context.Context, data json.RawMessage, rt *ResourceType, services ...Service) (json.RawMessage, error)
    BeforeCreateCommit(ctx context.Context, resource *Resource, services ...Service) error
    // ... and so on for every method
}

// In resource_service.go:
data, err := behavior.BeforeCreate(ctx, cmd.Data, rt, s.repo, s.tripleRepo, s.logger)
```

**Pros:**
- Source-level backward compatible: existing behavior implementations that don't declare the parameter still satisfy the interface because Go's variadic rule allows the implementation to omit the parameter? *(Not actually true — see Cons.)*
- The service layer stays in control of which services are exposed.
- No two-phase initialization.

**Cons:**
- **Not actually backward compatible.** Go interface satisfaction is structural: if `ResourceBehavior.BeforeCreate` declares `services ...Service`, then every implementation must declare the same parameter. Existing behaviors like `personBehavior.BeforeCreate` will stop compiling. Variadic only makes the *call site* optional, not the *definition*.
- **No type safety.** The only way to accept heterogeneous services through a single variadic is `...any` or a marker interface. Every behavior that needs, say, a `ResourceRepository`, must iterate the slice and type-assert — error-prone and verbose.
- **Ordering is fragile.** Either the service layer establishes a convention ("repo is always element 0, logger is element 1") — which is brittle — or behaviors scan the slice by type, which hides wiring errors until runtime.
- **Every method signature gets polluted** with a parameter that most behaviors don't use. Eight methods, one mostly-ignored parameter each.
- **Every call site must enumerate services.** `resource_service.go` has to pass the full service list at 8+ call sites; easy to pass a partial list and have a behavior fail at runtime.
- **`CompositeBehavior` must forward the slice** on every hop, adding noise to the chain logic.
- Doesn't scale: adding a new service means touching every call site.

---

### Option 2: Optional Initializer Interface (Setter Injection)

Leave the `ResourceBehavior` interface untouched. Introduce a separate optional interface that behaviors implement if they need services. After Fx wires the container, the registry builder iterates all registered behaviors and calls `Init` on those that opt in.

**Example:**
```go
// New — in domain/entities/resource_behavior.go
type BehaviorServices struct {
    Resources    repositories.ResourceRepository
    Triples      repositories.TripleRepository
    ResourceTypes repositories.ResourceTypeRepository
    Logger       Logger
    // extend as needed
}

type BehaviorInitializer interface {
    Init(services BehaviorServices) error
}
```

```go
// In application/resource_behaviors.go
func ProvideResourceBehaviorRegistry(
    registry *PresetRegistry,
    services entities.BehaviorServices,
) (ResourceBehaviorRegistry, error) {
    behaviors := registry.Behaviors()
    for slug, b := range behaviors {
        if init, ok := b.(entities.BehaviorInitializer); ok {
            if err := init.Init(services); err != nil {
                return nil, fmt.Errorf("init behavior %q: %w", slug, err)
            }
        }
    }
    return behaviors, nil
}
```

```go
// A behavior that needs services
type productBehavior struct {
    entities.DefaultBehavior
    triples repositories.TripleRepository
}

func (b *productBehavior) Init(s entities.BehaviorServices) error {
    b.triples = s.Triples
    return nil
}
```

**Pros:**
- **Fully backward compatible.** The `ResourceBehavior` interface doesn't change, so `personBehavior`, `organizationBehavior`, and any existing user-written behaviors continue to compile unchanged.
- **Opt-in.** Only behaviors that need services implement `BehaviorInitializer`. The vast majority stay simple.
- **Type-safe.** Services are accessed as named fields on a struct, not by position or type assertion.
- **Services are resolved once at startup**, not on every hook call. Behaviors close over references; hooks run without reflection.
- **Single place to extend.** Adding a new service means adding a field to `BehaviorServices` and a provider in `ProvideResourceBehaviorRegistry`. No changes to call sites or interface methods.
- Natural idiom for Go codebases (e.g. how many stdlib libraries use optional interfaces like `io.Closer`).

**Cons:**
- **Two-phase construction.** A behavior exists briefly between `new(productBehavior)` and `Init(services)` with nil service fields. If someone calls hooks during that window (they shouldn't, but still), nil-pointer panics are possible. Mitigation: assert initialization happened in the registry builder before returning, or panic on first use.
- **Mutable state.** Behaviors become mutable after construction. Presets can no longer rely on "what I registered is what runs" without trusting that nobody calls `Init` twice. Mitigation: document that `Init` is called exactly once, or use `sync.Once` inside the behavior.
- **The `BehaviorServices` struct becomes a god-bag.** Every service anyone ever needs gets a field, and all behaviors get access to all services even if they only use one. Mitigation: keep the struct small, or introduce sub-scoped service interfaces over time.
- **Two interfaces to document.** New contributors must know to check for both `ResourceBehavior` and `BehaviorInitializer`.

---

### Option 3: Behavior Factories in Presets (Closure-Based DI)

Change `PresetDefinition.Behaviors` from a map of instances to a map of factory functions. Each factory receives a `BehaviorServices` struct and returns a ready-to-use behavior. The registry builder calls each factory once at startup.

**Example:**
```go
// application/preset_registry.go
type BehaviorFactory func(services entities.BehaviorServices) entities.ResourceBehavior

type PresetDefinition struct {
    Name         string
    Description  string
    Types        []PresetResourceType
    Behaviors    map[string]BehaviorFactory  // changed from map[string]entities.ResourceBehavior
    BehaviorMeta map[string]entities.BehaviorMeta
    // ...
}
```

```go
// application/presets/core/preset.go
Behaviors: map[string]application.BehaviorFactory{
    "person": func(s entities.BehaviorServices) entities.ResourceBehavior {
        return &personBehavior{} // doesn't use services
    },
    "organization": func(s entities.BehaviorServices) entities.ResourceBehavior {
        return &organizationBehavior{triples: s.Triples} // uses services via closure
    },
},
```

```go
// application/resource_behaviors.go
func ProvideResourceBehaviorRegistry(
    registry *PresetRegistry,
    services entities.BehaviorServices,
) ResourceBehaviorRegistry {
    factories := registry.BehaviorFactories()
    built := make(ResourceBehaviorRegistry, len(factories))
    for slug, factory := range factories {
        built[slug] = factory(services)
    }
    return built
}
```

**Pros:**
- **No two-phase state.** Behaviors are constructed with services in hand; they are immutable for their lifetime.
- **Type-safe.** Services closed over at construction, no assertions.
- **Explicit per-preset.** The factory body makes it obvious what a behavior depends on.
- **No `ResourceBehavior` interface change** — hooks still take `(ctx, data, rt)`, so existing signatures stay clean.
- **Services resolved once.** Same performance characteristics as Option 2.
- Scales naturally: adding a service means adding a field to `BehaviorServices`.

**Cons:**
- **Breaking change to `PresetDefinition`.** Every existing preset (currently just `core`) and any third-party preset must migrate from `map[string]ResourceBehavior` to `map[string]BehaviorFactory`. A shim could preserve the old field temporarily.
- **Slightly more ceremony for trivial behaviors.** `func(_ entities.BehaviorServices) entities.ResourceBehavior { return &personBehavior{} }` is wordier than `&personBehavior{}`. A helper like `application.StaticBehavior(&personBehavior{})` can hide the boilerplate.
- **Preset registration is no longer purely declarative.** The factory is code, not data — harder to serialize or introspect if presets ever need to be loaded from YAML/JSON.
- **Tests that build a registry manually** (see `application/resource_behaviors_test.go`) must wrap each test behavior in a factory.

---

### Option 4: Fx-Provided Behaviors with Group Tags

Move behavior construction out of the preset registry entirely. Each behavior becomes an Fx provider tagged into a group; the `ResourceBehaviorRegistry` is built by Fx from the group. Preset metadata (names, display names, screens) stays in the registry; behavior *construction* becomes an Fx concern.

**Example:**
```go
// application/presets/core/module.go
var Module = fx.Module("core-preset",
    fx.Provide(
        fx.Annotate(
            NewPersonBehavior,
            fx.ResultTags(`name:"behavior-person"`, `group:"behaviors"`),
        ),
        fx.Annotate(
            NewOrganizationBehavior,
            fx.ResultTags(`name:"behavior-organization"`, `group:"behaviors"`),
        ),
    ),
)

func NewPersonBehavior() SlugBehavior {
    return SlugBehavior{Slug: "person", Behavior: &personBehavior{}}
}

func NewOrganizationBehavior(triples repositories.TripleRepository) SlugBehavior {
    return SlugBehavior{Slug: "organization", Behavior: &organizationBehavior{triples: triples}}
}
```

```go
// application/resource_behaviors.go
type SlugBehavior struct {
    Slug     string
    Behavior entities.ResourceBehavior
}

func ProvideResourceBehaviorRegistry(in struct {
    fx.In
    Behaviors []SlugBehavior `group:"behaviors"`
}) ResourceBehaviorRegistry {
    reg := make(ResourceBehaviorRegistry, len(in.Behaviors))
    for _, sb := range in.Behaviors {
        reg[sb.Slug] = sb.Behavior
    }
    return reg
}
```

**Pros:**
- **Full DI.** Behaviors receive whatever Fx can provide — no shared `BehaviorServices` struct, no god-bag.
- **Idiomatic Fx.** Uses the same group-tag pattern already employed elsewhere in the module.
- **Per-behavior scoping.** Each behavior declares exactly what it needs.
- Clean separation: `PresetRegistry` holds declarative metadata; Fx holds constructed behaviors.

**Cons:**
- **Largest refactor.** Every preset must expose an Fx module, not just a `Register` function. The `RegisterAll` pattern is replaced by composing Fx modules in `application/module.go`.
- **Presets are no longer self-contained.** Behavior construction moves from `presets/core/preset.go` to `presets/core/module.go`; metadata stays behind. Reading a preset now means reading two files.
- **Harder to support dynamic presets.** If WeOS ever wants to load presets from disk or a plugin system at runtime, Fx providers are harder to register after the container is built than entries in a map.
- **More Fx boilerplate.** `fx.Annotate`, `fx.ResultTags`, and group wiring are unfamiliar to new contributors; the current preset model is just "put a struct in a map".
- **Behavior discovery is implicit.** You can no longer inspect `PresetDefinition.Behaviors` to see what a preset provides — you have to trace Fx group membership.
- Testing becomes harder: tests that want to exercise the registry with fake behaviors now either spin up an Fx container or bypass it entirely.

---

### Option 5: Stash Services in `context.Context`

Don't change the interface. Before calling any behavior hook, the service layer attaches a `BehaviorServices` value to the context. Behaviors retrieve it via a typed helper.

**Example:**
```go
// domain/entities/resource_behavior.go
type behaviorServicesKey struct{}

func WithBehaviorServices(ctx context.Context, s BehaviorServices) context.Context {
    return context.WithValue(ctx, behaviorServicesKey{}, s)
}

func ServicesFromContext(ctx context.Context) (BehaviorServices, bool) {
    s, ok := ctx.Value(behaviorServicesKey{}).(BehaviorServices)
    return s, ok
}
```

```go
// In resource_service.go before calling hooks:
ctx = entities.WithBehaviorServices(ctx, s.behaviorServices)
data, err := behavior.BeforeCreate(ctx, cmd.Data, rt)
```

```go
// In a behavior:
func (b *productBehavior) BeforeCreate(ctx context.Context, data json.RawMessage, rt *entities.ResourceType) (json.RawMessage, error) {
    services, ok := entities.ServicesFromContext(ctx)
    if !ok {
        return nil, errors.New("behavior services not available")
    }
    related, err := services.Resources.FindByID(ctx, "...")
    // ...
}
```

**Pros:**
- **Zero interface changes.** Fully backward compatible with all existing behaviors.
- **Every hook already has `ctx`** — no new plumbing.
- **No two-phase construction** of behaviors.
- **Opt-in per hook.** Only the hooks that need services look them up.

**Cons:**
- **Implicit, "magic" dependency.** `context.Context` is documented as a mechanism for request-scoped values, cancellation, and deadlines — using it for a DI container is an anti-pattern widely discouraged in Go (including by the `context` package docs themselves).
- **Compile-time guarantees lost.** Forgetting to call `WithBehaviorServices` before invoking a hook fails at runtime inside the behavior, with an unhelpful error.
- **Testing is awkward.** Every behavior test must now construct a context with the services attached, even when the behavior only reads one field.
- **Leaks up the stack.** Once services are in the context, they flow into every downstream call the behavior makes (repository calls, other services), creating accidental coupling.
- **Services become visible everywhere.** Any code with the context can pull services out — weakens the encapsulation that DI normally provides.

---

## Comparison Matrix

| Criteria | Option 1: Variadic | Option 2: Initializer | Option 3: Factory | Option 4: Fx Groups | Option 5: Context |
|---|---|---|---|---|---|
| **Interface change** | Yes (breaking) | No | No | No | No |
| **`PresetDefinition` change** | No | No | Yes (breaking) | Yes (moves to Fx) | No |
| **Source-level backward compat** | No* | Yes | No (shim possible) | No | Yes |
| **Behaviors are immutable after build** | Yes | No | Yes | Yes | Yes |
| **Type-safe service access** | No | Yes | Yes | Yes | Yes |
| **Services resolved once vs. per-call** | Per-call | Once | Once | Once | Per-call |
| **Per-behavior service scoping** | Yes (caller-decided) | No (shared struct) | No (shared struct) | Yes (per provider) | No (shared struct) |
| **Works for dynamic/plugin presets** | Yes | Yes | Yes | No | Yes |
| **Migration burden for existing presets** | High (every hook) | None | Low (wrap in factory) | High (Fx modules) | None |
| **Migration burden for tests** | High | None | Low | High | Medium |
| **Fits existing Fx/preset patterns** | Poor | Good | Good | Excellent (Fx) | Poor |
| **Risk of nil-deref at runtime** | Low | Medium | Low | Low | Medium |

\* Option 1 claimed backward compat in the original proposal, but Go interface satisfaction requires identical signatures on the implementation — existing behaviors would stop compiling until updated.

## Recommendation

**Option 3 (Factory in `PresetDefinition`)** is the preferred approach. It keeps the `ResourceBehavior` interface clean, makes dependencies explicit at construction, avoids two-phase state, and leaves preset self-containment intact. The breaking change to `PresetDefinition.Behaviors` is small (only the `core` preset exists today; migration is one file) and can be eased with a helper like `application.StaticBehavior(&personBehavior{})` for behaviors that don't need services.

**Option 2 (Initializer interface)** is the recommended fallback if the project prioritizes zero breaking changes. It is fully backward compatible at the source level, requires no preset migration, and gives service-needing behaviors a clean way to opt in. The main downsides — two-phase construction and mutable state — are manageable with a single point of initialization in `ProvideResourceBehaviorRegistry`.

**Option 1 (Variadic on interface methods)** — the original proposal — should be rejected. The claimed backward-compat benefit is illusory because Go requires the implementation's signature to match the interface; every existing behavior would need to be updated anyway. Once you accept that breakage, Options 2 and 3 give strictly better type safety, call-site ergonomics, and testability for the same migration cost.

**Option 4 (Fx group tags)** is a valid long-term direction if WeOS moves toward more Fx-native composition, but it is overkill for today's needs and makes dynamic preset loading harder in the future.

**Option 5 (context-based DI)** should be rejected as an anti-pattern — `context.Context` is explicitly not for passing dependencies, and the implicit wiring creates runtime failure modes that the other options avoid.

## Follow-Up Work (If Option 3 Is Accepted)

- [ ] Define `entities.BehaviorServices` with an initial set of fields (`Resources`, `Triples`, `ResourceTypes`, `Logger`) in `domain/entities/resource_behavior.go`
- [ ] Change `PresetDefinition.Behaviors` to `map[string]BehaviorFactory` in `application/preset_registry.go`
- [ ] Add `application.StaticBehavior(b entities.ResourceBehavior) BehaviorFactory` helper for no-dep behaviors
- [ ] Update `ProvideResourceBehaviorRegistry` in `application/resource_behaviors.go` to accept `BehaviorServices` and call factories
- [ ] Wire `entities.BehaviorServices` in `application/module.go` as an Fx provider that assembles the struct from existing repositories
- [ ] Migrate `application/presets/core/preset.go` to the factory form
- [ ] Update `application/resource_behaviors_test.go` test helpers to wrap test behaviors in factories
- [ ] Update `docs/_howto/create-behavior.md` with the new factory form and an example behavior that uses a service
- [ ] Update `docs/_explanation/behaviors.md` to describe service injection