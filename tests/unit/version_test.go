package unit

import (
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
// have reached the others. Listing them here is what keeps a fourth call site
// from quietly reintroducing the same defect.
var versionCallSites = []string{
	"internal/cli/root.go",
	"internal/mcp/server.go",
	"internal/mcp/agent_toolset.go",
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

// TestNoCallSiteHardcodesItsVersion proves the three places weos names its own
// version all read the one accessor. It reads the real source rather than
// restating it: a test that held its own copy of these lines would keep
// passing after someone typed a literal back into the tree.
func TestNoCallSiteHardcodesItsVersion(t *testing.T) {
	for _, site := range versionCallSites {
		source, err := os.ReadFile(filepath.Join("..", "..", site))
		if err != nil {
			t.Fatalf("read %s: %v", site, err)
		}

		if match := versionFieldLiteral.FindSubmatch(source); match != nil {
			t.Errorf("%s hardcodes Version: %q. The binary then reports that string whatever "+
				"tag it was built from, and the other call sites drift from it. Read %s instead.",
				site, match[1], versionAccessor)
		}
		if !strings.Contains(string(source), versionAccessor) {
			t.Errorf("%s does not read %s, so its version has some other source of truth.", site, versionAccessor)
		}
	}
}

// TestTheBinaryReportsTheVersionItWasBuiltWith builds cmd/weos both ways and
// runs it. This is the acceptance criterion itself: a stamped build has to
// name its tag, and a plain `go build` — which is what `go install`, `go run`
// and a library consumer all do — has to still build and report something
// honest rather than a number somebody typed in 2026.
func TestTheBinaryReportsTheVersionItWasBuiltWith(t *testing.T) {
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

			output, err := exec.Command(binary, "--version").CombinedOutput() // #nosec G204 -- binary is built into t.TempDir() above
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
// binary. `run` is absent on purpose: `go run` builds from the working tree,
// where the toolchain's own build info already names the commit.
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
