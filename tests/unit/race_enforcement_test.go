package unit

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// raceCheckedTargets is the make targets Article V names as enforcing its
// -race rule. TestArticleVNamesNoTargetThisSuiteMisses keeps this list in step
// with the article, so a target added there cannot go unchecked here.
//
// test-e2e is deliberately absent, and its absence is the article's, not an
// oversight here. Article V's rule reaches tests/e2e and test-e2e is the only
// target that runs that layer, but the article's enforcement line does not
// name it, and the flag is not free there: measured on this package, 106s
// without -race against 790s with it. `go test` panics at a 10-minute default
// that the recipe does not override, so adding the flag alone would not make
// the target slower, it would make it fail outright. Adding test-e2e to the
// article is bead wm-rcyl; it needs a -timeout in the same edit.
var raceCheckedTargets = []string{
	"test",
	"test-unit",
	"test-integration",
	"test-graph-embedded",
}

// raceFlag matches -race as a whole argument. Substring matching would accept
// a recipe that merely mentions the word, and -race takes no value, so a
// whitespace-delimited token is the whole test.
var raceFlag = regexp.MustCompile(`(^|\s)-race(\s|$)`)

// makeTargetInBackticks matches the `make <target>` references the constitution
// writes its enforcement list as.
var makeTargetInBackticks = regexp.MustCompile("`make ([a-zA-Z0-9_-]+)`")

// goTestCommands returns the recipe lines of a make target that invoke
// `go test`, read out of the real Makefile. Reading the real recipe is the
// point: a test that restated the commands here would keep passing after
// someone dropped the flag from the Makefile.
func goTestCommands(t *testing.T, target string) []string {
	t.Helper()

	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	var commands []string
	found, inRecipe := false, false
	for _, line := range strings.Split(string(makefile), "\n") {
		if strings.HasPrefix(line, target+":") {
			found, inRecipe = true, true
			continue
		}
		if !inRecipe {
			continue
		}
		// In make, a line that does not begin with a tab ends the recipe.
		if !strings.HasPrefix(line, "\t") {
			break
		}
		// `@` silences the line, `-` ignores its exit status and `+` runs it
		// even under `make -n`. All three are make's, not the shell's.
		command := strings.TrimLeft(strings.TrimPrefix(line, "\t"), "@-+")
		if strings.Contains(command, "go test") {
			commands = append(commands, command)
		}
	}

	if !found {
		t.Fatalf("the Makefile declares no %s target; either it was renamed or this list is stale", target)
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
	constitution, err := os.ReadFile("../../constitution.md")
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

// TestArticleVNamesNoTargetThisSuiteMisses guards the list above against the
// article growing past it. It checks containment rather than equality, because
// a target may be worth race-checking before the article names it: what must
// never happen is the article naming one that nothing here proves.
func TestArticleVNamesNoTargetThisSuiteMisses(t *testing.T) {
	checked := make(map[string]bool, len(raceCheckedTargets))
	for _, target := range raceCheckedTargets {
		checked[target] = true
	}

	for _, target := range articleVEnforcementTargets(t) {
		if !checked[target] {
			t.Errorf("Article V names `make %s` as enforcing the -race rule, but raceCheckedTargets does not list it, "+
				"so nothing proves it passes the flag. Add it there.", target)
		}
	}
}
