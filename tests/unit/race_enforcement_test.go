package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// raceCheckedTargets is the make targets Article V names as enforcing its
// -race rule. TestArticleVNamesNoTargetThisSuiteMisses keeps this list in step
// with the article, so a target added there cannot go unchecked here.
var raceCheckedTargets = []string{
	"test",
	"test-unit",
	"test-integration",
	"test-graph-embedded",
}

// raceExemptTargets is the make targets that run `go test` and deliberately do
// not pass -race, each against the reason it is allowed to. It is the one place
// an exemption is written down, and both directions of the guard read it: the
// inverse sweep below lets these targets through, and the Article V containment
// check lets the article name them without demanding they be race-checked.
//
// That second use is the whole reason the set exists rather than a special case
// in one test. Article V's enforcement paragraph is prose, and a sentence
// naming a target as an EXEMPTION is written with the same `make X` backticks
// as a sentence naming it as ENFORCEMENT — bead wm-rcyl is exactly that edit
// for test-e2e. Without this set, writing the documented exemption turns this
// suite red, and the failure message walks the editor into putting -race on a
// target that then panics at the default timeout.
//
// An entry here is a claim that someone decided, not that nobody noticed. Add
// one only with a reason a reader can weigh.
var raceExemptTargets = map[string]string{
	"test-e2e": "the godog acceptance layer measures 106s without -race and 790s with it, " +
		"and the recipe overrides no -timeout, so the flag alone would not slow the target down, " +
		"it would make it panic at `go test`'s 10-minute default. Adding it needs a -timeout in the " +
		"same edit; that is bead wm-rcyl",
	"check-release-tag": "it re-runs the release-tag ordering tests as a release guard, and they are " +
		"string comparisons over published tags with no concurrency in them. Whether it should carry " +
		"the flag anyway is a behavior change to a release guard and is Akeem's call, not this " +
		"suite's; it is filed as bead wm-mfot",
}

// raceFlag matches -race as a whole argument. Substring matching would accept
// a recipe that merely mentions the word, and -race takes no value, so a
// whitespace-delimited token is the whole test.
var raceFlag = regexp.MustCompile(`(^|\s)-race(\s|$)`)

// goTestInvocation matches a `go test` that a shell would actually run, rather
// than the words appearing anywhere in the line. The leading class covers the
// places a command can begin — start of line, after whitespace, and after the
// operators that chain one command to the next — so an env-var prefix like
// `CGO_LDFLAGS="..." go test` and a `... || go test` both count, while a path
// or a message that happens to read "go test" does not.
var goTestInvocation = regexp.MustCompile(`(^|[\s;&|(])go test(\s|$)`)

// makeTargetInBackticks matches the `make <target>` references the constitution
// writes its enforcement list as.
var makeTargetInBackticks = regexp.MustCompile("`make ([a-zA-Z0-9_-]+)`")

// goTestCommands returns the recipe lines of a make target that invoke
// `go test`, read out of the real Makefile by the shared parser in
// makefile_test.go.
//
// The two failures below are deliberately different diagnostics, which is why
// the parser reports "found" separately from "returned lines". A missing target
// means the list above is stale after a rename. A target that exists but runs
// no tests means it was quietly repurposed into something else, and that is the
// one a single "not found" message would hide.
func goTestCommands(t *testing.T, target string) []string {
	t.Helper()

	recipe, found := makefileRecipe(t, target)
	if !found {
		t.Fatalf("the Makefile declares no %s target; either it was renamed or this list is stale", target)
	}

	var commands []string
	for _, command := range recipe {
		if goTestInvocation.MatchString(command) {
			commands = append(commands, command)
		}
	}
	if len(commands) == 0 {
		t.Fatalf("the %s recipe runs no `go test`; it can no longer be what runs a layer of the suite, so this list is stale", target)
	}
	return commands
}

// articleVEnforcementTargets returns the make targets Article V's "How this is
// enforced" line names.
func articleVEnforcementTargets(t *testing.T) []string {
	t.Helper()

	// Lowercase on purpose: the tracked filename is constitution.md, and CI
	// runs on a case-sensitive filesystem where CONSTITUTION.md does not exist.
	constitution, err := os.ReadFile(filepath.Join(repoRoot, "constitution.md"))
	if err != nil {
		t.Fatalf("read constitution.md: %v", err)
	}

	_, rest, found := strings.Cut(string(constitution), "## Article V —")
	if !found {
		t.Fatal("constitution.md declares no Article V; this suite exists to enforce it")
	}
	article, _, _ := strings.Cut(rest, "\n## ")

	_, enforcement, found := strings.Cut(article, "**How this is enforced.**")
	if !found {
		t.Fatal("Article V states no enforcement; this suite has nothing to check against")
	}
	// The list runs to the end of the paragraph, which may wrap over several
	// lines. A blank line ends it.
	paragraph, _, _ := strings.Cut(enforcement, "\n\n")

	var targets []string
	for _, match := range makeTargetInBackticks.FindAllStringSubmatch(paragraph, -1) {
		targets = append(targets, match[1])
	}
	if len(targets) == 0 {
		t.Fatal("Article V's enforcement line names no make target; it no longer says what enforces the rule")
	}
	return targets
}

// TestTestTargetsRunUnderRace is the reason this file exists. Article V says
// the test layers "all run under -race" and points a developer at the per-layer
// make targets as how that is enforced. A target without the flag hands back a
// green that carries no race detection at all, and the developer has no way to
// tell it apart from one that does.
func TestTestTargetsRunUnderRace(t *testing.T) {
	for _, target := range raceCheckedTargets {
		for _, command := range goTestCommands(t, target) {
			if !raceFlag.MatchString(command) {
				t.Errorf("`make %s` runs %q, which carries no -race.\n"+
					"Article V requires every test layer to run under race detection, and names the per-layer "+
					"targets as how that is enforced, so this target reports a green nobody can trust.", target, command)
			}
		}
	}
}

// TestNoMakeTargetRunsGoTestWithoutRace sweeps the other way. The test above
// proves that every target some list names passes the flag; it cannot prove
// there is no target the list forgot, so a new one is born unguarded and
// nothing notices.
//
// That is not hypothetical. check-release-tag arrived running
// `go test -count=1 ./tests/unit/ -run 'ReleaseTag|Scheme'` with no -race, and
// no guard in this file had anything to say about it, because no list named it.
// So walk every target the Makefile declares instead of a list, and require
// each `go test` it runs to carry the flag or to be in raceExemptTargets with a
// written reason.
//
// The sweep covers every `go test`, not only the ones pointed at ./tests/...:
// Article V's rule is about the test suite, and test-graph-embedded runs a
// layer of it out of ./infrastructure/graph/..., which a ./tests/-only sweep
// would wave through.
func TestNoMakeTargetRunsGoTestWithoutRace(t *testing.T) {
	swept := 0
	for _, target := range makefileTargets(t) {
		recipe, _ := makefileRecipe(t, target)
		for _, command := range recipe {
			if !goTestInvocation.MatchString(command) {
				continue
			}
			swept++
			if raceFlag.MatchString(command) {
				continue
			}
			if reason, exempt := raceExemptTargets[target]; exempt {
				if strings.TrimSpace(reason) == "" {
					t.Errorf("`make %s` is exempt from -race with no reason recorded; an exemption "+
						"nobody can weigh is indistinguishable from an oversight", target)
				}
				continue
			}
			t.Errorf("`make %s` runs %q, which carries no -race.\n"+
				"Article V requires the test suite to run under race detection. Add the flag, or — if this "+
				"target deliberately runs without it — add %q to raceExemptTargets with the reason why.",
				target, command, target)
		}
	}

	// A sweep that matched nothing would pass silently and prove nothing, which
	// is the failure mode of every guard that parses a file it does not own.
	if swept == 0 {
		t.Error("the sweep found no `go test` in any make recipe, so it is proving nothing; " +
			"either the Makefile stopped running tests or the recipe parser no longer reads it")
	}
}

// TestRaceExemptionsAreNotAlsoClaimedAsEnforcement keeps the two lists from
// contradicting each other. A target in both would be simultaneously required
// to carry -race and excused from carrying it, and which guard won would depend
// on which one ran — so say it here instead of discovering it as a confusing
// failure somewhere else.
func TestRaceExemptionsAreNotAlsoClaimedAsEnforcement(t *testing.T) {
	for _, target := range raceCheckedTargets {
		if reason, exempt := raceExemptTargets[target]; exempt {
			t.Errorf("`make %s` is listed as race-checked and also exempted (%q); it cannot be both", target, reason)
		}
	}
	for target := range raceExemptTargets {
		if _, found := makefileRecipe(t, target); !found {
			t.Errorf("raceExemptTargets excuses `make %s`, which the Makefile no longer declares; "+
				"an exemption for a target that does not exist only hides the next one", target)
		}
	}
}

// TestArticleVNamesNoTargetThisSuiteMisses guards the list above against the
// article growing past it. It checks containment rather than equality, because
// a target may be worth race-checking before the article names it: what must
// never happen is the article naming one that nothing here proves.
//
// Exempt targets are subtracted first. The article's enforcement paragraph is
// prose, and the sentence that documents an exemption reads to this parser
// exactly like the sentence that claims enforcement — so without the
// subtraction, writing down an exemption we already made turns this red.
func TestArticleVNamesNoTargetThisSuiteMisses(t *testing.T) {
	checked := make(map[string]bool, len(raceCheckedTargets))
	for _, target := range raceCheckedTargets {
		checked[target] = true
	}

	for _, target := range articleVEnforcementTargets(t) {
		if checked[target] {
			continue
		}
		if _, exempt := raceExemptTargets[target]; exempt {
			continue
		}
		t.Errorf("Article V names `make %s` as enforcing the -race rule, but raceCheckedTargets does not list it, "+
			"so nothing proves it passes the flag.\n"+
			"Add it to raceCheckedTargets if the target really does enforce the rule. If the article mentions it as "+
			"an EXEMPTION rather than as enforcement, add it to raceExemptTargets with the reason instead — do not "+
			"put -race on a target that cannot carry it.", target)
	}
}

// cliDocumentedTargets is the make targets docs/_reference/cli.md quotes a
// runnable command for and this suite therefore pins. It is the five targets
// Article V's rule reaches, not the whole table: several rows summarize what a
// target does rather than quote it (`make fmt` is documented as "`go fmt` +
// `goimports`", which is prose and not a command), and a blanket check would go
// red on those the moment it was written.
var cliDocumentedTargets = []string{
	"test",
	"test-unit",
	"test-integration",
	"test-e2e",
	"test-graph-embedded",
}

// backtickedSpan matches one `code span` in a markdown table cell.
var backtickedSpan = regexp.MustCompile("`([^`]*)`")

// cliDocRow returns the commands docs/_reference/cli.md's Make Targets table
// quotes for one target, in the order the row lists them.
func cliDocRow(t *testing.T, target string) ([]string, bool) {
	t.Helper()

	doc, err := os.ReadFile(filepath.Join(repoRoot, "docs", "_reference", "cli.md"))
	if err != nil {
		t.Fatalf("read docs/_reference/cli.md: %v", err)
	}

	_, table, found := strings.Cut(string(doc), "## Make Targets")
	if !found {
		t.Fatal("docs/_reference/cli.md has no Make Targets section; this guard has nothing to check")
	}
	table, _, _ = strings.Cut(table, "\n## ")

	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		if strings.TrimSpace(cells[1]) != "`make "+target+"`" {
			continue
		}
		var commands []string
		for _, match := range backtickedSpan.FindAllStringSubmatch(cells[2], -1) {
			commands = append(commands, match[1])
		}
		return commands, true
	}
	return nil, false
}

// TestCLIDocsQuoteTheRealRecipes pins the reference table to the Makefile. The
// table restates each recipe as a command a reader can run, and a reader who
// runs what it says believes that is what the target does — so a recipe edited
// without the table is a document that lies about whether the tests are
// race-checked. That is exactly the drift this branch found: two rows still
// quoted the pre-race commands after the Makefile had moved on.
func TestCLIDocsQuoteTheRealRecipes(t *testing.T) {
	for _, target := range cliDocumentedTargets {
		documented, listed := cliDocRow(t, target)
		if !listed {
			t.Errorf("docs/_reference/cli.md's Make Targets table has no row for `make %s`.\n"+
				"It is one of the targets Article V's -race rule reaches, so a reader looking the table up "+
				"has no way to see what it runs.", target)
			continue
		}

		recipe, found := makefileRecipe(t, target)
		if !found {
			t.Errorf("docs/_reference/cli.md documents `make %s`, which the Makefile no longer declares", target)
			continue
		}

		if strings.Join(documented, "\n") != strings.Join(recipe, "\n") {
			t.Errorf("docs/_reference/cli.md quotes `make %s` as running:\n  %s\nbut the Makefile runs:\n  %s\n"+
				"Update the table row; a reader who runs the documented command believes that is what the target does.",
				target, strings.Join(documented, "\n  "), strings.Join(recipe, "\n  "))
		}
	}
}
