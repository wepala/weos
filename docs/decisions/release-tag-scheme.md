---
title: "ADR: Release Tag Scheme"
parent: Architecture Decision Records
layout: default
nav_order: 9
---

# ADR: Moving the v3 Release Tag Scheme to `v3.0.1-beta.N`

**Status:** Accepted
**Date:** 2026-08-31
**Tracking:** beads `wm-1jkb`

## Problem

Every tag published on the `v3` line from `v3.0.1-alpha10` through
`v3.0.1-alpha21` sorts **below** `v3.0.1-alpha9`. Measured 2026-08-31:

```
$ go list -m -versions github.com/wepala/weos/v3
... highest reported: v3.0.1-alpha9
```

Twelve published tags are invisible as upgrades, and nothing reports an error.

Semver compares a pre-release string one dot-separated identifier at a time. An
identifier that is **not purely numeric** is compared as a string. `alpha21` and
`alpha9` are each a single identifier, so they compare lexically, and
`alpha2…` < `alpha9`. Only a purely numeric identifier compares numerically.

The cost is not limited to `go get -u` resolving stale. A pseudo-version derives
its base from the highest semver tag, so a commit on today's `v3` tip becomes
`v3.0.1-alpha9.0.<timestamp>-<sha>`, which reads to a human like a downgrade
from `alpha20`. Pinning the `alpha21` tag over that pseudo-version made `go get`
print `downgraded` for the same commit, `cac07cef`, in both directions.

This affects every consumer of weos, not one downstream service.

## Constraints

1. **The published tags are immutable.** A module proxy has already cached
   `v3.0.1-alpha1` … `v3.0.1-alpha21`, and a consumer pinned to one of them
   would break if a tag were renamed or deleted. Whatever is chosen has to sort
   above tags that cannot be moved.
2. **There is no release automation.** No workflow, script, Makefile target, or
   config in this repository creates a tag; a person types `git tag`. Only a
   written rule and an optional guard can change what that person types.
3. **The fix has to be provable.** Tagging is a human act, so the property under
   test is the *ordering rule*, not the act.

## Options Evaluated

### 1. `v3.0.1-alpha.22` — split the number into its own identifier

The idiomatic semver form, and the option the bug report originally prescribed.
It does not work on this line, and the reason is easy to miss.

Splitting `alpha22` into `alpha` + `22` makes the **first** identifier plain
`alpha`. Comparison reaches that identifier first, and `alpha` is a lexical
prefix of both `alpha9` and `alpha21`. A shorter prefix sorts first, so the
comparison is decided against `alpha.22` before the number `22` is ever
considered:

```
v3.0.1-alpha.22  <  v3.0.1-alpha9
```

Verified against `golang.org/x/mod/semver`, the library the Go toolchain itself
uses. **Rejected: it does not fix the bug.**

### 2. `v3.0.1-alpha022` — zero-pad so lexical order matches numeric order

Keeps one string identifier but pads it, so lexical comparison tracks numeric
order up to 999. It fixes ordering *among new tags* but not against the existing
ones, for the same prefix reason plus the leading zero:

```
v3.0.1-alpha022  <  v3.0.1-alpha9
```

**Rejected: it does not fix the bug either.** It also carries a hard ceiling and
an unusual shape that every future release engineer would have to be told about.

### 3. `v3.0.2-alpha.1` — bump the patch version and start clean

Sorts above everything published, and restores the idiomatic `alpha.N` form for
future lines. **Rejected** because it abandons `v3.0.1` before the line is
finished: the patch number would be spent to work around a tag-format defect
rather than to mark a change in the software, and downstream pins would read as
a version bump that nothing in the code justifies.

### 4. `v3.0.1-beta.N` — a later pre-release word, with the number split out

`beta` sorts above every `alphaN` identifier at the first comparison, and the
number is its own purely numeric identifier, so it compares numerically from
there on:

```
v3.0.1-beta.1  >  all 22 published v3 tags, including v3.0.1-alpha9
beta.1 < beta.2 < beta.9 < beta.10 < beta.21 < beta.100
```

**Chosen.**

## Decision

The `v3` line is tagged **`v3.0.1-beta.N`**, with a literal period before the
number. **`v3.0.1-beta.1` is the first tag in the new scheme.**

The existing `v3.0.1-alphaN` tags **stay exactly as they are** — never renamed,
never deleted. They sit below every `beta.N` tag from now on, which is harmless
once nothing new is published in the old shape.

Three artifacts carry the decision, and they are deliberately connected rather
than three independent restatements of the same string:

- **`Makefile` — `RELEASE_TAG_PREFIX`** is the single source of truth for the
  scheme.
- **`make check-release-tag TAG=…`** refuses any tag outside it. The pattern is
  derived from `RELEASE_TAG_PREFIX`, so it pins the version as well as the
  shape: `v3.0.1-alpha22`, `v3.0.1-beta22`, `v3.0.1-alpha022` and
  `v3.0.1-alpha.22` are all refused, and so is a tag on a different line until
  the prefix is deliberately moved. `v3.0.1-beta.01` is refused as well —
  semver forbids a leading zero in a numeric identifier, so the Go toolchain
  drops such a tag entirely and it would reproduce the original symptom with
  the guard's blessing. The target then runs the ordering tests below, because
  it derives its pattern from the very prefix it is checking and so cannot
  catch a typo in that prefix on its own.
- **`tests/unit/release_tag_test.go`** reads `RELEASE_TAG_PREFIX` out of the
  Makefile and asserts, against `golang.org/x/mod/semver`, that what it declares
  sorts above all 22 published v3 tags and that `beta.10` sorts above `beta.9`.
  It also pins the measurement that rejected options 1 and 2, so the discarded
  prescription cannot be adopted later by someone reading the bug report alone.
  One test in it shells back out to `make check-release-tag`, so the guard's
  pattern is executed rather than merely written down — without it the escaping
  that makes the pattern a guard could be dropped and the suite would stay
  green.

The coupling is the point. A future engineer who edits the scheme in the
Makefile gets the ordering re-proven by `make test`; one who edits only the
prose changes nothing that is checked.

### How this version line ends

`RELEASE_TAG_PREFIX` describes a *pre-release* line — a fixed prefix followed by
a number — and it cannot express graduation. The first stable release, `v3.0.1`
with no pre-release identifier at all, has no number to append and therefore no
prefix that produces it. This is the first thing the guard cannot handle, and it
arrives before any move to a new pre-release line does.

Setting `RELEASE_TAG_PREFIX := v3.0.1` to cut that release is not merely
unhelpful, it is wrong in both directions: the derived pattern becomes
`^v3\.0\.1[1-9][0-9]*$`, which **refuses `v3.0.1`** — the very tag being cut —
and **accepts `v3.0.11`**, a version on a different patch line entirely.

So the release that graduates this line is cut by hand, without the guard, and
`RELEASE_TAG_PREFIX` moves in the same commit to whatever pre-release line comes
next (`v3.0.2-beta.`, say) so the guard covers the tags after it. Extending the
guard to cover stable tags would mean expressing "prefix plus a number" and "an
exact version" in one variable; that is not worth doing for a tag cut once per
line. `check-release-tag` guards the pre-release tags *within* a line, not the
release that ends it, and that limit is recorded here rather than discovered at
the moment of a release.

## Consequences

### Good

- `go get -u github.com/wepala/weos/v3` will reach new tags again, for every
  consumer, from the first `beta.N` tag onward. Nothing changes for a consumer
  until that tag is cut — this decision sets the scheme; cutting `v3.0.1-beta.1`
  against it is tracked as beads `wm-o53y`.
- Pseudo-versions will derive from a base that advances once that tag exists, so
  a commit on the tip stops reading as a downgrade.
- The failure mode cannot recur silently: the ordering rule has a test, and the
  tag shape has a guard that runs before the tag is created.
- No published tag moves, so no pinned consumer breaks.

### Neutral

- Nothing is backfilled. The twelve stranded `alphaN` tags remain unreachable as
  upgrades; they are reachable by explicit pin, as they always were.
- Consumers still have to run `go get` themselves — a new tag does not move a
  pin. The stale pins across the workspace are tracked separately as `wm-v2tb`.
- `golang.org/x/mod` becomes a **direct** requirement of a public module, for a
  test-only import (`semver`, in `tests/unit/release_tag_test.go`). Consumers'
  minimal version selection will pull or bump `x/mod` to `v0.35.0` and see
  `go.sum` churn on their next `go get`. It raises no toolchain floor — weos
  already requires go 1.25.4 — and the alternative, hand-rolling the comparison
  in the test, would prove the ordering against something other than the library
  the Go toolchain itself uses. Knowingly accepted.

### Risks

- **The `v3.0.1` line is labelled `beta` from now on and cannot return to
  `alpha`.** Any `alpha` tag on this line would sort below the `beta` tags, so
  the word is spent. Accepted: `beta` is the honest label for the line's
  maturity, and the alternative was spending the patch number instead.
- **The guard is advisory, not binding.** `git tag` does not consult the
  Makefile. `make check-release-tag` only helps a release engineer who runs it,
  which is why the rule is written into `CONTRIBUTING.md` as well. Making it
  binding needs release automation, which does not exist here yet.
- **The test reads the Makefile by relative path.** Moving either file breaks
  the link. It fails loudly rather than silently passing, which is the safe
  direction, but it is a coupling worth knowing about.

## Further Reading

- `CONTRIBUTING.md` at the repository root, "Cutting a release" — the rule as a
  release engineer meets it. It is not part of this documentation site, so it is
  named here rather than linked.
