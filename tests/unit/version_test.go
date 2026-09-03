package unit

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// versionCallSites is every file in which the weos binary states its own
// version to something outside the process: the CLI's `--version` flag, the
// MCP handshake the server presents to a client, and the identity the
// in-process tool-listing client presents back to it.
//
// Each of these used to carry its own "0.1.0" literal, so all three reported
// 0.1.0 whatever tag the binary was built from, and a change to one would not
// have reached the others. They are named here so the guard can assert the
// positive half — that each one still reads the accessor. The negative half is
// not a list at all: see TestNoCallSiteHardcodesItsVersion.
var versionCallSites = []string{
	"internal/cli/root.go",
	"internal/mcp/server.go",
	"internal/mcp/agent_toolset.go",
}

// versionPackage is the one package allowed to name a version, being the
// source of truth the rest of the tree reads. The walk below skips it so that
// a literal there — a fallback, a test fixture, a doc example — is not read as
// the defect it exists to prevent everywhere else.
const versionPackage = "internal/version"

// unwalkedDirs are the directories the guard does not descend into: none of
// them holds weos source. `.git` and `node_modules` are skipped because
// walking them is pure cost, and `vendor` because third-party code is not ours
// to hold to this rule. Everything else in the tree is walked, `web/` and
// `tests/` included — a call site can be added anywhere.
var unwalkedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// versionFieldLiteral matches a `Version:` struct field set to a string
// literal. Both the cobra command and the MCP implementation name the field
// exactly that, so the literal form is the whole defect: a version a human
// typed cannot track the tag the binary was built from.
var versionFieldLiteral = regexp.MustCompile(`Version:\s*"([^"]*)"`)

// versionAccessor is the one source of truth every call site has to read.
const versionAccessor = "version.Version()"

// stampedVersion is a plausible release tag under the scheme
// tests/unit/release_tag_test.go pins, chosen so it cannot be confused with
// the retired 0.1.0 literal.
const stampedVersion = "v3.0.1-beta.99"

// versionSymbol is the linker path of the variable a build stamps. It is
// spelled out here rather than derived, because that string is the contract
// between the Makefile, the Dockerfile and the package: a rename that missed
// one of them would stamp nothing and fail silently, which is exactly what
// this test is here to make loud.
const versionSymbol = "github.com/wepala/weos/v3/internal/version.version"

// TestNoCallSiteHardcodesItsVersion walks every non-test .go file in the
// repository and fails on any `Version:` struct field set to a string literal,
// then checks that the three known call sites read the accessor.
//
// The walk is the point. An earlier version of this test read only the three
// files in versionCallSites, and claimed that listing them kept a fourth call
// site from reintroducing the defect. It did not: a new file typing
// Version: "0.1.0" was never opened, so it passed. Only the whole tree can
// catch a call site nobody has written yet.
//
// Test files are excluded because a test client legitimately names itself — a
// godog world or an MCP probe passes its own Implementation.Version, which is
// the identity of the test, not the version of the binary. Every such literal
// in the tree today is in a _test.go file, so the suffix is the whole
// exclusion; no file needs naming.
func TestNoCallSiteHardcodesItsVersion(t *testing.T) {
	root := filepath.Join("..", "..")

	// Recorded for every file the walk reads, so the accessor check below
	// costs no second pass and can also tell "does not read it" apart from
	// "the walk never reached it" — a renamed call site must fail loudly
	// rather than silently stop being checked.
	readsAccessor := make(map[string]bool)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if unwalkedDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == versionPackage || strings.HasPrefix(relative, versionPackage+"/") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if match := versionFieldLiteral.FindSubmatch(source); match != nil {
			t.Errorf("%s hardcodes Version: %q. The binary then reports that string whatever "+
				"tag it was built from, and the other call sites drift from it. Read %s instead.",
				relative, match[1], versionAccessor)
		}
		readsAccessor[relative] = strings.Contains(string(source), versionAccessor)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	for _, site := range versionCallSites {
		reads, walked := readsAccessor[site]
		switch {
		case !walked:
			t.Errorf("%s was not read by the walk, so it is no longer where this test thinks it "+
				"is. Point versionCallSites at wherever the call site moved to.", site)
		case !reads:
			t.Errorf("%s does not read %s, so its version has some other source of truth.", site, versionAccessor)
		}
	}
}

// TestTheBinaryReportsTheVersionItWasBuiltWith builds cmd/weos both ways and
// runs it. This is the acceptance criterion itself: a stamped build has to
// name its tag, and a plain `go build` — which is what `go install` and a
// library consumer both do — has to still build and report something honest
// rather than a number somebody typed in 2026.
//
// It links cmd/weos twice, which is ~14s, so -short skips it. That is the
// whole reason `make test-unit` passes -short: the fast loop keeps every test
// in this package that reads source or expands a recipe, and drops the one
// that invokes the compiler. The full `make test` and CI's race job still run
// it, so the acceptance criterion is never merged unproven.
func TestTheBinaryReportsTheVersionItWasBuiltWith(t *testing.T) {
	if testing.Short() {
		t.Skip("builds cmd/weos twice; run without -short to check the stamp reaches the CLI")
	}

	for _, probe := range []struct {
		name    string
		ldflags []string
		assert  func(t *testing.T, reported string)
	}{
		{
			name:    "stamped",
			ldflags: []string{"-ldflags", "-X " + versionSymbol + "=" + stampedVersion},
			assert: func(t *testing.T, reported string) {
				if reported != stampedVersion {
					t.Errorf("a binary stamped with %s reports %q; the stamp does not reach the CLI, "+
						"so a release build cannot name its own tag", stampedVersion, reported)
				}
			},
		},
		{
			name: "plain",
			assert: func(t *testing.T, reported string) {
				if !strings.HasPrefix(reported, "dev") {
					t.Errorf("an unstamped `go build` reports %q, want a version starting with \"dev\"; "+
						"an unstamped build must say so rather than claim a release it is not", reported)
				}
			},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "weos")
			build := append([]string{"build", "-o", binary}, probe.ldflags...)
			cmd := exec.Command("go", append(build, "./cmd/weos")...)
			cmd.Dir = filepath.Join("..", "..")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build %v: %v\n%s", probe.ldflags, err, output)
			}

			output, err := exec.Command(binary, "--version").CombinedOutput()
			if err != nil {
				t.Fatalf("weos --version: %v\n%s", err, output)
			}

			// cobra prints "weos version <v>"; the version is the rest of the
			// line, so it is read off the end rather than split on spaces —
			// `git describe` output carries no spaces but a stamp could.
			reported := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(output)), "weos version"))
			probe.assert(t, reported)
		})
	}
}

// stampedTargets is every make target that produces a distributable weos
// binary. `run` is absent because it produces none: it is the dev loop, and
// the binary it builds is thrown away at exit.
//
// It is worth saying what `make run` therefore reports, because the obvious
// guess is wrong. `go run` does not fall back on the toolchain's VCS stamping
// — verified on Go 1.25, it records no vcs.revision at all and calls the main
// module "(devel)" — so `make run` reports a bare `dev`, not `dev+<commit>`.
// Adding -ldflags to the run target would fix that; it is deliberately not
// done here, because `make run` is not a build anyone ships or supports.
var stampedTargets = []string{"build", "build-embedded"}

// TestEveryBuildPathStampsTheVersion closes the seam between the package and
// the build. TestTheBinaryReportsTheVersionItWasBuiltWith proves the stamp
// reaches the CLI when a build passes it; nothing there proves the project's
// own build paths do pass it, and a release built by a recipe that quietly
// dropped the flag would report `dev` while looking entirely healthy.
//
// The make targets are read by expanding the real recipe rather than by
// matching text, so the $(if) that suppresses the flag for an empty VERSION is
// exercised as make would run it.
func TestEveryBuildPathStampsTheVersion(t *testing.T) {
	wantFlag := "-X " + versionSymbol + "=" + stampedVersion

	for _, target := range stampedTargets {
		t.Run(target, func(t *testing.T) {
			cmd := exec.Command("make", "-n", target, "VERSION="+stampedVersion)
			cmd.Dir = filepath.Join("..", "..")
			// -n prints the recipe without running it, so a failing
			// prerequisite here is a broken Makefile, not a broken build.
			recipe, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("make -n %s: %v\n%s", target, err, recipe)
			}
			if !strings.Contains(string(recipe), wantFlag) {
				t.Errorf("`make %s` builds without %q, so a release built by it reports %s "+
					"instead of the tag it came from:\n%s", target, wantFlag, "dev", recipe)
			}
		})
	}

	t.Run("Dockerfile", func(t *testing.T) {
		// The image cannot derive its own version: .dockerignore excludes
		// .git, so neither `git describe` nor the toolchain's VCS stamping
		// has anything to read inside the builder stage. The build arg is
		// the only channel, which is why it is pinned here.
		dockerfile, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
		if err != nil {
			t.Fatalf("read Dockerfile: %v", err)
		}
		if !strings.Contains(string(dockerfile), "-X "+versionSymbol+"=$VERSION") {
			t.Errorf("the Dockerfile does not stamp %s from its VERSION build arg, so every "+
				"deployed image reports the fallback version", versionSymbol)
		}
	})
}

// environmentWithout returns the process environment with every entry for the
// named variable dropped.
//
// cmd.Env is a list rather than a map, so appending to os.Environ() does not
// override a variable the parent already exports — it leaves two entries for
// it, and which one the child reads is undefined. Measured here, GNU Make 3.81
// takes the last, so the poison below happens to win; a make that took the
// first would read the parent's VERSION instead, and the subtest would pass
// without ever having poisoned anything. Dropping the inherited entry first
// removes the coin flip, so `VERSION=whatever go test ./tests/unit/...` proves
// the same thing a clean shell does.
func environmentWithout(name string) []string {
	prefix := name + "="
	inherited := os.Environ()
	kept := make([]string, 0, len(inherited))
	for _, entry := range inherited {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// TestTheEnvironmentCannotStampTheVersion pins the difference between a
// version somebody asked for and one that merely happened to be lying around.
//
// VERSION is a generic name. A shell profile, a wrapper script or an earlier
// step of a CI job can leave one exported, and a Makefile that read it would
// stamp that unrelated string onto a release binary — reporting a version
// nobody built, with every step green. `make VERSION=v3.2.0 build` is still
// an explicit instruction and still wins, so both halves are pinned here.
func TestTheEnvironmentCannotStampTheVersion(t *testing.T) {
	const impostor = "v9.9.9-exported-by-something-else"

	recipeFor := func(t *testing.T, args []string, env ...string) string {
		t.Helper()
		cmd := exec.Command("make", append([]string{"-n"}, args...)...)
		cmd.Dir = filepath.Join("..", "..")
		// The inherited VERSION is dropped rather than shadowed, so the only
		// entry make can read is the one this test put there.
		cmd.Env = append(environmentWithout("VERSION"), env...)
		// -n expands the recipe without running it, so nothing here builds.
		recipe, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make -n %s: %v\n%s", strings.Join(args, " "), err, recipe)
		}
		return string(recipe)
	}

	t.Run("an exported VERSION is ignored", func(t *testing.T) {
		recipe := recipeFor(t, []string{"build"}, "VERSION="+impostor)
		if strings.Contains(recipe, impostor) {
			t.Errorf("`make build` stamped %q out of the environment. A binary then reports a "+
				"version nobody built it from, which is the defect this stamping exists to "+
				"fix:\n%s", impostor, recipe)
		}
	})

	t.Run("an explicit make VERSION= still wins", func(t *testing.T) {
		recipe := recipeFor(t, []string{"build", "VERSION=" + stampedVersion}, "VERSION="+impostor)
		wantFlag := "-X " + versionSymbol + "=" + stampedVersion
		if !strings.Contains(recipe, wantFlag) {
			t.Errorf("`make build VERSION=%s` builds without %q, so a release cut by hand or by "+
				"a tag-less CI job cannot name its own version:\n%s",
				stampedVersion, wantFlag, recipe)
		}
	})
}
