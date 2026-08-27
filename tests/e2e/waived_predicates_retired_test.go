// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
)

// Issue #537: the twenty-three waivers #535 left standing are repaired and the
// waiver list is emptied. The world is #520's real-preset world (vocabWorld)
// and the guard steps are #535's, generalised there to take a preset scope, so
// "any installed type" is the same sweep over every type rather than a second
// implementation. "The build before the waived predicates were retired" is the
// default registry with this story's terms STRIPPED — the real preset, edited,
// so it cannot drift.

func TestWaivedPredicatesRetired(t *testing.T) {
	runFeatureWith(t, "waived-predicates-retired",
		"features/waived_predicates_retired.feature", initWaivedPredicatesScenario)
}

type waivedTermsWorld struct {
	*vocabWorld
}

func initWaivedPredicatesScenario(sc *godog.ScenarioContext) {
	w := &waivedTermsWorld{vocabWorld: newVocabWorld()}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})
	w.registerVocabSteps(sc)
	w.registerVocabGuardSteps(sc)

	// --- the builds either side of the story ---
	sc.Step(`^a WeOS database provisioned by the build before the waived predicates were retired$`,
		w.aDatabaseFromBeforeTheRepair)
	sc.Step(`^the twin restarts on the build that retires the waived predicates(?: again)?$`, w.restartOnThisBuild)
}

// --- the build before the waived predicates were retired -------------------

// preStoryWaivedContexts names, per type, every `@context` entry #537 ADDED:
// the twenty-three repaired terms, plus the seven prefix declarations that
// carry them — `core`, `notif`, `task` and `web` for the four minting presets,
// and `dct` on the one preset whose repair is published rather than house.
//
// REVERTING IS ONE OPERATION HERE, NOT TWO. Unlike #535 — where four properties
// carried a WRONG `fo:` term that had to be restored — every one of the 23 had
// NO term at all and rode `@vocab` into whichever published vocabulary the type
// names. So the shim is a pure STRIP, which is CONTRACT 8's "no second
// population" seen from the other side: with no term to diverge from, nothing
// can be held on the upgrade.
var preStoryWaivedContexts = map[string][]string{
	// core — an avatar and a logo are URLs schema.org spells differently, and
	// `slug` it does not publish at all.
	"person":       {"avatarURL"},
	"organization": {"logoURL", "slug", "core"},

	// knowledge — SKOS publishes neither name; both are Dublin Core. This
	// preset mints nothing, so it strips a `dct` prefix and no house one.
	"concept-scheme": {"title", "description", "dct"},

	// notifications — the largest group: two published renames and seven
	// house concepts schema.org never named.
	"notification": {
		"title", "body", "kind", "actionUrl", "actionLabel",
		"taskRef", "occurredAt", "read", "dedupeKey", "notif",
	},

	// tasks — one house predicate for `status` declared on BOTH types, so the
	// strip has to reach both or the upgrade runs against a half-old build.
	"project": {"status", "task"},
	"task":    {"status", "priority", "dueDate", "task"},

	// website — template machinery, plus the one published rename that sits on
	// the same type as `cssSelector`, which never moved and is not stripped.
	"web-page":          {"slug", "template", "web"},
	"web-page-element":  {"content"},
	"web-page-template": {"templateBody", "slots", "web"},
}

// buildBeforeTheRepair is the default registry with #537's context entries
// deleted — a transform over PresetResourceType.Context, not a second copy of
// the presets, so it cannot drift from what shipped.
func (*waivedTermsWorld) buildBeforeTheRepair() *application.PresetRegistry {
	stripped := map[string]bool{}
	reg := rewriteRegistry(presets.NewDefaultRegistry(), func(pt *application.PresetResourceType) {
		names, mine := preStoryWaivedContexts[pt.Slug]
		if !mine {
			return
		}
		var ctx map[string]any
		if json.Unmarshal(pt.Context, &ctx) != nil || ctx == nil {
			panic(fmt.Sprintf("the pre-#537 shim could not read the %q context: %s", pt.Slug, pt.Context))
		}
		for _, name := range names {
			if _, declared := ctx[name]; !declared {
				// The shim has gone stale against the preset it shims: the
				// story's entry is not there to strip, so every upgrade
				// scenario would run against the current build in disguise.
				panic(fmt.Sprintf("the pre-#537 shim expects the %q context to declare %q; it does not",
					pt.Slug, name))
			}
			delete(ctx, name)
			stripped[pt.Slug+"."+name] = true
		}
		encoded, err := json.Marshal(ctx)
		if err != nil {
			panic(fmt.Sprintf("the pre-#537 shim could not re-encode the %q context: %v", pt.Slug, err))
		}
		pt.Context = encoded
	})
	var want, missing []string
	for slug, names := range preStoryWaivedContexts {
		for _, name := range names {
			key := slug + "." + name
			want = append(want, key)
			if !stripped[key] {
				missing = append(missing, key)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		panic(fmt.Sprintf("the pre-#537 shim stripped %d of %d context entries; no type owns: %v",
			len(stripped), len(want), missing))
	}
	assertShimRecreatesTheFault(reg)
	return reg
}

// preStoryViolationCount is the number of waivers #535 shipped and #537
// retired.
const preStoryViolationCount = 23

// assertShimRecreatesTheFault is the shim's own acceptance test, run every time
// it builds. Stripping the right NAMES is not the same as recreating the right
// STATE: a strip that missed the sense of the story would still pass the
// name-by-name check above and then run every upgrade scenario against a build
// that was never broken. So the reverted registry is handed to the guard, and
// it must report exactly the twenty-three violations the waiver map used to
// hold — no more, no fewer.
func assertShimRecreatesTheFault(reg *application.PresetRegistry) {
	var types []application.PresetResourceType
	for _, preset := range reg.List() {
		types = append(types, preset.Types...)
	}
	found := presets.PublishedVocabularyViolations(types)
	if len(found) == preStoryViolationCount {
		return
	}
	panic(fmt.Sprintf("the pre-#537 shim reports %d violation(s), want %d — it is not the build before the "+
		"repair:\n  %s", len(found), preStoryViolationCount, renderViolations(found)))
}

func (w *waivedTermsWorld) aDatabaseFromBeforeTheRepair() error {
	w.registry = w.buildBeforeTheRepair
	return w.provision()
}
