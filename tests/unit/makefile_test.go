package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Several tests in this package assert things about the real Makefile rather
// than about Go code: that `make dev-build-frontend` spares a tracked file,
// that every target running `go test` passes -race, that docs/_reference/cli.md
// still quotes the commands the recipes actually run. All of them need the same
// thing first — the recipe lines of one target, read out of the Makefile — and
// all of them are wrong in the same ways if that reading is naive.
//
// This file is that reading, once. It is a _test.go file in package unit on
// purpose: github.com/wepala/weos/v3 is an imported library, and Makefile
// scaffolding has no production consumer, so an internal/ package would grow
// the compiled surface for nobody. The _test.go suffix is what guarantees it
// never ships.

// makeLinePrefixes is the three characters make strips from the front of a
// recipe line before handing it to the shell: `@` silences the line, `-`
// ignores its exit status and `+` runs it even under `make -n`.
const makeLinePrefixes = "@-+"

// parseMakefileRecipe returns the recipe lines of target as the shell would
// receive them, and whether the Makefile declares the target at all.
//
// The `found` result is not the same question as "did it return lines". A
// target that exists with an empty recipe, and a target that does not exist,
// are different mistakes with different fixes, and callers here give them
// different diagnostics — see goTestCommands, which distinguishes "no such
// target" from "that target no longer runs any tests".
//
// Lines come back verbatim and joined, not tokenized. TestDevBuildFrontend*
// executes them through `sh -c`, so it needs the exact command; a structured
// form would need a real shell tokenizer to be correct (quoting, `$(...)`, and
// the CGO_LDFLAGS="..." env prefixes test-graph-embedded's lines carry).
// Everything else here substring- and regex-matches, which works on the same
// string.
//
// Four things about make's recipe syntax that a naive reader gets wrong, all of
// which have already bitten this repository:
//
//  1. A blank line and a column-0 comment do NOT end a recipe. Make skips both
//     and carries on to the next tab-indented line. check-release-tag has a
//     six-line comment block at column 0 in the middle of its recipe, with the
//     `go test` line below it — a parser that stopped at the comment would
//     report that target as running no tests at all.
//  2. A tab-indented `#` line is a comment, not a command. Reported as a
//     command it becomes the thing an error message blames.
//  3. A trailing backslash continues the line. Left unjoined, `go test -v \`
//     looks like a `go test` invocation carrying no -race while the -race sits
//     on a fragment nothing associates with it.
//  4. Make's `@`, `-` and `+` prefixes are make's, not the shell's.
//
// Known limitation, deliberately not solved: a target is recognized by
// strings.HasPrefix(line, target+":"), which misses a multi-target rule
// (`a b: ...`) and a double-colon rule (`a:: ...`). Neither appears in this
// Makefile. makefileTargets below does split multi-target rules, so the inverse
// race sweep would at least notice one arriving.
func parseMakefileRecipe(makefile, target string) (lines []string, found bool) {
	inRecipe := false
	for _, line := range strings.Split(makefile, "\n") {
		if !inRecipe {
			if strings.HasPrefix(line, target+":") {
				found, inRecipe = true, true
			}
			continue
		}

		// A line whose predecessor ended in a backslash continues it, tab or
		// no tab: make splices the pair before it decides anything else about
		// the line. The backslash is dropped and nothing is put in its place,
		// because the shell removes a backslash-newline entirely — `go te\` +
		// `st` really is `gotest`, and inserting a space here would invent a
		// token break make does not make. Only one leading tab comes off, the
		// same one make strips from every recipe line; the rest of the
		// indentation reaches the shell as the whitespace it already is.
		if n := len(lines); n > 0 && strings.HasSuffix(lines[n-1], `\`) {
			lines[n-1] = strings.TrimSuffix(lines[n-1], `\`) + strings.TrimPrefix(line, "\t")
			continue
		}

		// The tab is hardcoded on purpose: .RECIPEPREFIX arrived in GNU Make
		// 3.82, this repository builds under 3.81, and nothing sets it. A
		// "blank" line of spaces still counts as blank, hence the TrimSpace.
		if !strings.HasPrefix(line, "\t") {
			skipped := strings.TrimSpace(line)
			if skipped == "" || strings.HasPrefix(skipped, "#") {
				continue
			}
			break
		}

		command := strings.TrimLeft(strings.TrimPrefix(line, "\t"), makeLinePrefixes)
		if trimmed := strings.TrimSpace(command); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		lines = append(lines, command)
	}
	return lines, found
}

// makefileRecipe is parseMakefileRecipe against the Makefile in this
// repository. Reading the real recipe is the point of every caller: a test that
// restated the commands here would keep passing after someone edited the
// Makefile out from under it.
func makefileRecipe(t *testing.T, target string) (lines []string, found bool) {
	t.Helper()

	makefile, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	return parseMakefileRecipe(string(makefile), target)
}

// makefileRule matches the target side of a rule: a line that starts in column
// 0 with something other than a comment, up to a colon that is not part of a
// `:=` assignment. The targets themselves are whitespace-separated before that
// colon, so a multi-target rule yields each of its targets.
var makefileRule = regexp.MustCompile(`^([^\t#][^:=]*):([^=]|$)`)

// parseMakefileTargets returns every target the Makefile declares, in the order
// it declares them. The inverse -race sweep needs this: checking only the
// targets some list already names can never notice a new target arriving
// unguarded, which is exactly how check-release-tag was born without the flag.
//
// Special targets (.PHONY and friends) are skipped — they carry no recipe worth
// sweeping, and .PHONY's prerequisites are other targets' names, not its own.
func parseMakefileTargets(makefile string) []string {
	var targets []string
	seen := map[string]bool{}
	for _, line := range strings.Split(makefile, "\n") {
		match := makefileRule.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		for _, target := range strings.Fields(match[1]) {
			if strings.HasPrefix(target, ".") || seen[target] {
				continue
			}
			seen[target] = true
			targets = append(targets, target)
		}
	}
	return targets
}

// makefileTargets is parseMakefileTargets against the Makefile in this
// repository.
func makefileTargets(t *testing.T) []string {
	t.Helper()

	makefile, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	targets := parseMakefileTargets(string(makefile))
	if len(targets) == 0 {
		t.Fatal("the Makefile declares no targets at all; the rule pattern no longer matches how it is written")
	}
	return targets
}

// TestParseMakefileRecipe covers the parser itself. Nothing did before: the two
// suites that use it only ever tested what it was pointed at, so each of the
// cases below shipped as a bug in one of them and survived two reviews.
func TestParseMakefileRecipe(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		makefile string
		target   string
		want     []string
		wantNone bool
	}{
		{
			name:     "a blank line inside a recipe does not end it",
			makefile: "build:\n\tfirst\n\n\tsecond\n",
			target:   "build",
			want:     []string{"first", "second"},
		},
		{
			name:     "a column-0 comment inside a recipe does not end it",
			makefile: "build:\n\tfirst\n# why the next line is here\n\tsecond\n",
			target:   "build",
			want:     []string{"first", "second"},
		},
		{
			name:     "a tab-indented comment is not a command",
			makefile: "build:\n\t# not a command\n\tfirst\n",
			target:   "build",
			want:     []string{"first"},
		},
		{
			name:     "a continuation joins onto the line it continues",
			makefile: "build:\n\tgo test -v \\\n\t\t-race ./...\n",
			target:   "build",
			// One line, and the -race is on it. Unjoined, the first fragment
			// reads as a `go test` with no race detection.
			want: []string{"go test -v \t-race ./..."},
		},
		{
			name:     "a continuation splices with no space inserted",
			makefile: "build:\n\tgo te\\\nst ./...\n",
			target:   "build",
			want:     []string{"go test ./..."},
		},
		{
			name:     "make's line prefixes are stripped",
			makefile: "build:\n\t@-+echo hi\n",
			target:   "build",
			want:     []string{"echo hi"},
		},
		{
			name:     "the next rule ends the recipe",
			makefile: "build:\n\tfirst\n\nother:\n\tsecond\n",
			target:   "build",
			want:     []string{"first"},
		},
		{
			name:     "a longer target name is not a prefix match",
			makefile: "test-unit:\n\tfirst\n",
			target:   "test",
			wantNone: true,
		},
		{
			name:     "a declared target with an empty recipe is found",
			makefile: "dev-setup: dev-seed dev-serve\n\nother:\n\tfirst\n",
			target:   "dev-setup",
			want:     nil,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			lines, found := parseMakefileRecipe(testCase.makefile, testCase.target)
			if found == testCase.wantNone {
				t.Fatalf("found = %t, want %t", found, !testCase.wantNone)
			}
			if strings.Join(lines, "\n") != strings.Join(testCase.want, "\n") {
				t.Errorf("recipe = %q, want %q", lines, testCase.want)
			}
		})
	}
}

// TestParseMakefileTargets covers the shapes the target enumerator has to tell
// apart, in particular the `:=` assignments this Makefile is full of.
func TestParseMakefileTargets(t *testing.T) {
	makefile := strings.Join([]string{
		".PHONY: build test",
		"# a comment: with a colon in it",
		"VAR := value: not a target",
		"OTHER ?= value",
		"build: ## build it",
		"\tgo build ./...",
		"a b: ## a multi-target rule",
		"\techo hi",
		"\tnested: not a target, it is indented",
		"build: ## declared twice, reported once",
		"",
	}, "\n")

	got := parseMakefileTargets(makefile)
	want := []string{"build", "a", "b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("targets = %q, want %q", got, want)
	}
}

// TestMakefileTargetsCoversTheRealMakefile pins the enumerator against the
// Makefile it actually sweeps. The table above proves the shapes; this proves
// the pattern still matches how this repository writes them, so the inverse
// -race sweep cannot quietly start checking nothing.
func TestMakefileTargetsCoversTheRealMakefile(t *testing.T) {
	found := map[string]bool{}
	for _, target := range makefileTargets(t) {
		found[target] = true
	}

	for _, target := range []string{
		"test", "test-unit", "test-integration", "test-e2e",
		"test-graph-embedded", "check-release-tag", "dev-build-frontend",
		"build", "lint",
	} {
		if !found[target] {
			t.Errorf("the target sweep did not find `%s`, which the Makefile declares; "+
				"targets it cannot see are targets the -race sweep cannot check", target)
		}
	}
}
