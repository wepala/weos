---
title: "ADR: Admin Index Override for Thin-Wrap Binaries"
parent: Architecture Decision Records
layout: default
nav_order: 5
---

# ADR: Admin Index Override for Thin-Wrap Binaries

**Status:** Proposed
**Date:** 2026-04-14
**Context:** weos core ships a compiled Nuxt admin SPA embedded into every binary via `//go:embed all:dist` in `web/embed.go`. Thin-wrap products built on top of weos (e.g., `ic-crm`) want to replace just the admin's index page (`/`) with a product-specific dashboard, without forking the admin SPA or re-implementing the rest of its routes, components, and composables. This ADR decides the mechanism weos core exposes for that override.

## Problem

`ic-crm` is a thin-wrap binary around weos that registers the `education` and `finance` presets and delegates everything else to weos core. Its `CLAUDE.md` is explicit: any CRM-shaped behavior beyond preset wiring belongs upstream, not in the wrapper. That design goal is being tested by a real requirement: ic-crm has a product-specific `Dashboard.vue` (billing summary, weekly class schedule, outstanding invoices) that should render at `/`. Core's generic resource-types grid at `pages/index.vue` is wrong for this product.

The preset screen system (today's `PresetDefinition.Screens` + `/api/resource-types/presets/:name/screens/:typeSlug/:file`) is scoped per resource type: files live at `<typeSlug>/<ScreenName>.mjs` and are reachable only at `/resources/<typeSlug>[/<id>]/screens/<name>`. There is **no top-level app-page hook**. ic-crm's Dashboard.vue has therefore been parked at `services/ic-crm/web/admin/src/pages/Dashboard.vue` with a README documenting the block.

We need a way for a host binary to replace the admin's index without dragging in a general-purpose preset-app-pages framework (which would be a much larger design than this one requirement justifies).

### Why each naive approach fails

- **Edit `services/core/web/admin/pages/index.vue` directly from ic-crm.** Requires ic-crm to touch `services/core`'s working tree at build time, which means every thin-wrap build dirties a different repo. Concurrent branch work in core is at risk and any Makefile failure mid-build leaves core in a modified state.
- **Swap only `dist/index.html` at serve time.** Nuxt's generated `index.html` is a 1-KB shell that imports hashed `_nuxt/*.js` chunks; the index route's component lives inside one of those chunks, not in the HTML. Swapping `index.html` alone does not change what renders at `/`.
- **Fork the admin SPA into the thin-wrap.** Defeats the point of the thin-wrap pattern. Every admin-side improvement in core would then need to be manually reconciled in every product.

## Constraints

Verified during exploration. Any option must respect these.

- **`web/embed.go:21`** — `var StaticFS embed.FS` is today a concrete `embed.FS`. Read from exactly one place.
- **`internal/cli/serve.go:147-150`** — the single consumer:
  ```go
  e.Use(apimw.Static(apimw.StaticConfig{
      Filesystem: web.StaticFS,
      Root:       "dist",
  }))
  ```
- **`api/middleware/static.go:29-34`** — `StaticConfig.Filesystem` is already typed as `fs.FS` (interface). Any implementation satisfies it.
- **`api/middleware/static.go:73-81`** — SPA fallback: for any path that isn't `/api/*` and doesn't match a real file, the middleware rewrites the URL to `/` and serves `index.html` from the FS. This means any override mechanism that replaces the FS must ship a complete SPA tree (including `index.html` and the `_nuxt/` chunks).
- **`web/admin/composables/usePresetScreens.ts:99-102`** — existing preset-screen dynamic-import pattern is Blob-URL + `import()` of a module fetched from an API endpoint. Already-proven in-tree.
- **Preset `.mjs` files are hand-maintained** in Options API + string-template format. There is no Vite/SFC build in the education preset today (confirmed in `presets/weos-private-presets/education/screens/src/README.md`).
- **`services/core/web/admin/nuxt.config.ts`** — no `extends` / layers config today; Nuxt layers are a greenfield option.

## Options

---

### Option 1 — Host-replaceable admin FS (whole-SPA substitution via Nuxt layers)

Expose the admin's static FS as a settable value on the `web` package. A host binary provides its own `fs.FS` at startup; the static middleware reads through it.

**Core change (~5-10 LOC):**
- `services/core/web/embed.go`: change the package-level var to an `fs.FS` typed accessor, and add a setter:
  ```go
  var embeddedFS embed.FS // the //go:embed target

  var staticFS fs.FS = embeddedFS

  // StaticFS returns the admin static filesystem.
  // Thin-wrap binaries can replace it with SetStaticFS before Execute().
  func StaticFS() fs.FS { return staticFS }

  // SetStaticFS replaces the admin static filesystem.
  // Must be called before the serve command constructs the HTTP stack.
  func SetStaticFS(fsys fs.FS) { staticFS = fsys }
  ```
- `services/core/internal/cli/serve.go:148`: read via `web.StaticFS()`.
- No middleware changes — `StaticConfig.Filesystem` is already `fs.FS`.

**Host binary responsibilities:**
- Produce a complete admin SPA dist and ship it as an `fs.FS`.
- Easiest path to that dist: **Nuxt layers**. Host has a minimal Nuxt project:
  ```
  services/ic-crm/web/admin/
    nuxt.config.ts        # export default defineNuxtConfig({ extends: ['../../../core/web/admin'] })
    pages/index.vue       # the product dashboard (shadows core's pages/index.vue)
    package.json          # nuxt generate
  ```
  `nuxt generate` walks the layer chain and emits a full dist in which the host's `pages/index.vue` wins for `/`. Every other admin file (components, composables, Ant Design plugin, other routes) is inherited from core.
- `//go:embed` the generated dist into an `fs.FS` and call `web.SetStaticFS(hostFS)` from `main.go` before `weoscli.Execute()`.

**Pros:**
- **Smallest core change of all options.** Two-method public API on `web`; no new routes, handlers, composables, or Vue refactors.
- **Full Vue SFC authoring on the host.** `<script setup>`, computed, Ant Design components, Nuxt auto-imports, TypeScript type-checking — all preserved.
- **Layer inheritance is free.** The host project typically needs only `nuxt.config.ts` + `pages/index.vue` + `package.json`. Every admin route not explicitly shadowed is inherited.
- **Generalizes.** The same mechanism lets a host shadow any number of pages by dropping files into its own `pages/`. The core API does not need to grow to support a second slot.
- **Matches Nuxt's intended extension model.** Layers exist precisely for this use case.

**Cons:**
- **Host binary gains a Node/Nuxt build stage.** The `services/ic-crm/CLAUDE.md` note that currently says "drop the frontend stage" was about avoiding a *fork* of the admin; a 1-file layer is not a fork, but the note needs updating.
- **Host ships its own full compiled dist.** Most of it is byte-identical to core's (the layer only changes the one shadowed page), so the admin bundle exists in two places in the build tree. At runtime only the host's copy is served; no duplication in the binary.
- **Layer resolution happens at the host's Nuxt build time.** A core admin change requires the host to re-run `nuxt generate` to pick it up. (Same constraint as any embedded SPA, and the same Makefile target already orchestrates it.)

---

### Option 2 — Runtime index-component slot

Keep the admin SPA single-sourced in core. Add a registration point for a single `.mjs` component that core's `pages/index.vue` loads at runtime and renders in place of its default content.

**Core change (~60-80 LOC across Go + Vue + TS):**
- Registration point: `application.SetIndexComponent(fs.FS, filename string)` (or a method on a dedicated registry).
- New handler `GET /api/admin/index-component` that serves the registered `.mjs` (404 if none).
- Rewrite `services/core/web/admin/pages/index.vue`:
  - Extract today's resource-types grid into a new `components/DefaultResourceGrid.vue`.
  - On mount, `$fetch('/api/admin/index-component')`. If a body comes back, Blob-URL + `import()` (copy the pattern from `usePresetScreens.ts:99-102`). Render the imported component via `<component :is="...">`.
  - If 404, render `<DefaultResourceGrid />`.
- New composable `useIndexComponent.ts` (parallel to `usePresetScreens.ts`).
- Tests for the handler, the fallback behavior, and the error path.

**Host binary responsibilities:**
- Hand-maintain an `.mjs` in Options API + string-template format (matching `education/screens/src/README.md` conventions):
  ```js
  export const meta = { name: 'Dashboard', label: 'Dashboard' }
  export default {
    props: { /* ... */ },
    data() { return { /* ... */ } },
    computed: { /* ... */ },
    template: `
      <div>
        <h2>Dashboard</h2>
        <a-row :gutter="[16, 16]"> ... </a-row>
        ...
      </div>
    `
  }
  ```
- `//go:embed` the `.mjs` and call the setter at startup.

**Pros:**
- **No Node / Nuxt build stage in the host.** A host binary stays pure Go + one hand-written `.mjs`.
- **Surgical slot.** Only the index is overridable; the mechanism cannot accidentally affect any other route.
- **Reuses the already-proven dynamic-import pattern** from preset screens.

**Cons:**
- **Cannot reuse the existing ic-crm `Dashboard.vue`.** It's a full SFC with `<script setup>`, `computed`, Ant Design components, multiple composables — roughly 300 lines. Converting it to Options API with a string template means a rewrite, not a mechanical transform.
- **Ongoing maintenance burden.** Every future edit to the dashboard lives in the hand-maintained `.mjs`: no `.vue` SFC tooling, no template type-checking, no component import linting, no IDE auto-complete inside the string template.
- **Larger core footprint.** New Go handler + new TS composable + pages/index.vue refactor + tests. Roughly 10× the core LOC of Option 1, for one slot.
- **Single-slot by design.** If a future thin-wrap wants to also override a settings index or a reports landing page, that's another round of the same surgery. The mechanism does not generalize without rework.
- **Fetch-at-mount cost.** Every `/` load does an extra round-trip to `/api/admin/index-component` before showing anything. Avoidable with SSG only if the slot is known at build time, which defeats the point of runtime registration.

---

### Option 3 — FS overlay (partial substitution)

Accept a second `fs.FS` on the static middleware and consult it before the main one. Files present in the overlay win; everything else falls through to the main FS and the existing SPA fallback.

**Core change (~20 LOC):** extend `StaticConfig` with an optional `Overlay fs.FS`; the request handler tries `Overlay.Open(filePath)` first, then `root.Open(filePath)`.

**Host binary responsibilities:** ship only the files it wants to override, embedded into its own `fs.FS`.

**Pros:**
- Feels minimal: "just overlay my files on top of core's."

**Cons — critical:**
- **Does not solve the stated problem.** Nuxt's `index.html` is a shell that imports hashed `_nuxt/*.js` chunks. The component that renders at `/` lives inside one of those chunks, not in `index.html`. Swapping just `index.html` does not change what renders at `/`.
- **To actually change `/`, the overlay must include a compiled replacement chunk and patch `index.html` to load it.** At that point the host is shipping a full Nuxt build anyway — functionally identical to Option 1 but without the Nuxt-layer ergonomics that make Option 1 pleasant.
- **Serves static files only.** No way to hot-swap SFC-level components without a full build on the host; see above.

Included so the ADR records explicitly why "just overlay the files" doesn't work, and so future readers don't try to revisit it.

---

### Option 4 — No core change; dirty overlay in the host build

Host `make build` target copies its `Dashboard.vue` over `services/core/web/admin/pages/index.vue`, runs `nuxt generate` inside `services/core`, runs `go build` of the host, then reverts the core file.

**Pros:**
- Zero upstream change.

**Cons:**
- **Dirties `services/core`'s working tree on every host build.** Any concurrent branch work in core is at risk.
- **Fails open on Makefile errors.** If `nuxt generate` or `go build` fails between the copy and the revert, core is left in a modified state.
- **Each host binary reinvents this dance.** Not one upstream mechanism but N brittle host-side scripts.
- **Blocks sharing the admin among multiple products running in the same workspace.** Two wrappers trying to build concurrently race on core's `pages/index.vue`.

Included so the ADR records explicitly why "do nothing upstream" is not an option, even though it is technically possible today.

---

## Comparison Matrix

| Criteria | Option 1: Host-replaceable FS | Option 2: Runtime slot | Option 3: FS overlay | Option 4: Dirty host build |
|---|---|---|---|---|
| **Solves the stated problem** | Yes | Yes | No (see Cons) | Yes |
| **Core LOC** | ~5-10 Go | ~60-80 Go + Vue + TS | ~20 Go | 0 |
| **Host needs Node/Nuxt build** | Yes (minimal — 1 layer project) | No | Yes (see Cons) | Yes (in core's tree) |
| **Keeps `.vue` SFC authoring for the override** | Yes | No (Options API + string template) | Yes | Yes |
| **Generalizes to N overridden pages** | Yes (just add more files to host layer) | No (one slot per rewrite) | N/A (doesn't work) | Yes (but with worse tradeoffs per page) |
| **Touches `services/core`'s working tree at build time** | No | No | No | Yes — the core concern |
| **Fetch-at-mount latency for `/`** | None (static) | Extra round-trip | None | None |
| **Preserves thin-wrap boundary** | Yes | Yes | Yes | No |
| **Idiomatic to the ecosystem** | Yes (Nuxt layers are the intended extension model) | Reinvents what layers already do | No | No |

## Recommendation

**Option 1 — Host-replaceable admin FS via Nuxt layers.** The smallest core change, the cleanest separation of concerns, and the only option that preserves the thin-wrap invariant while keeping full SFC authoring on the host. The Node-build cost in the host is bounded: a host binary that only wants to override one page ships a 3-file Nuxt layer project, and `nuxt generate` becomes one step in the host's existing `make build` chain. The two-method `web` API is small enough that we can ship it without committing to a general-purpose preset-app-pages framework, and any future requirement to override additional pages is handled by the host dropping more files into its `pages/` directory with zero further core change.

## Decision

**TBD — pending owner sign-off.** The decision will be recorded here once accepted.

## Consequences

The consequences depend on which option is chosen. Written conditionally so the ADR captures the trade-offs each path imposes; update to reflect the chosen option once the decision lands.

**If Option 1 is accepted:**
- `web.SetStaticFS` / `web.StaticFS` become part of the `weos` public API. Breaking changes to that signature become a semver concern.
- `services/ic-crm/CLAUDE.md` needs a follow-up edit: the "drop the frontend stage" note should be amended to "no fork; a host layer project is expected and supported."
- `services/ic-crm`'s build gains a Node/Nuxt step. Its `Makefile`, CI, and Dockerfile need updating to run `nuxt generate` on the layer project before `go build`.
- `services/ic-crm/web/admin/src/` (the `src/pages/Dashboard.vue` staging area and its README) gets replaced by a real Nuxt layer project. The old README's "blocked" note is resolved.
- Future thin-wrap products follow the same pattern at no additional core cost.

**If Option 2 is accepted:**
- `application.SetIndexComponent` (or equivalent) becomes a new public registration point. Its contract (slot name semantics, precedence when called more than once, lifecycle) is part of the weos API.
- `pages/index.vue` and its companion composable get shipped as a small new subsystem. Tests for the fallback path and the error path are required.
- ic-crm's Dashboard.vue is rewritten as a hand-maintained `.mjs`. Future ic-crm dashboard changes live in that file.
- A future requirement to override another page requires another ADR or an extension of this one to a multi-slot model.

**If Option 3 or 4 is accepted:**
- Not recommended; both have load-bearing problems documented above. Accepting either should include a written explanation of why the recommendation was overturned.

## References

- `services/core/web/embed.go` — current static FS declaration
- `services/core/api/middleware/static.go` — static file serving + SPA fallback
- `services/core/internal/cli/serve.go` — single consumer of `web.StaticFS`
- `services/core/web/admin/composables/usePresetScreens.ts` — dynamic-import pattern reused by Option 2
- `services/core/application/preset_registry.go` — existing preset contract (for comparison with the slot registration Option 2 would add)
- `services/ic-crm/web/admin/src/README.md` — the "blocked" note this ADR unblocks
- [Nuxt Layers documentation](https://nuxt.com/docs/getting-started/layers) — the mechanism Option 1 depends on
