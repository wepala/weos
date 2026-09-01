package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// placeholder is the one file under web/dist that git tracks. .gitignore
// ignores web/dist/* and then un-ignores this single entry, because web/dist
// itself is generated and never committed.
//
// It is not decoration. web/embed.go declares `//go:embed all:dist`, and that
// directive does not compile against a directory with nothing in it — so the
// placeholder is the only reason github.com/wepala/weos/v3 builds for a
// consumer who never runs the Nuxt build. A recipe that deletes it leaves a
// tracked file staged for deletion in every developer's `git status`, and
// committing that deletion breaks the build for every downstream consumer
// while still passing locally, because the author's own web/dist is full of a
// real SPA at that moment.
const placeholder = "PLACEHOLDER"

// nuxtGenerate is the one line of the dev-build-frontend recipe this test
// cannot run: it shells out to `npx nuxt generate`, which needs an installed
// node_modules and the network. Everything after it is plain file shuffling,
// and that is the half that loses the placeholder, so dropping this line
// leaves the behavior under test intact.
const nuxtGenerate = "npx nuxt generate"

// devBuildFrontendRecipe returns the shell commands `make dev-build-frontend`
// would run, read out of the real Makefile rather than restated here. Reading
// the real recipe is the point: a test that re-implemented it would keep
// passing after someone reintroduced `rm -rf web/dist`.
func devBuildFrontendRecipe(t *testing.T) []string {
	t.Helper()

	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	var recipe []string
	inRecipe := false
	for _, line := range strings.Split(string(makefile), "\n") {
		if strings.HasPrefix(line, "dev-build-frontend:") {
			inRecipe = true
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
		if command == "" || strings.HasPrefix(command, "#") {
			continue
		}
		recipe = append(recipe, command)
	}

	if len(recipe) == 0 {
		t.Fatal("the Makefile declares no dev-build-frontend recipe; this test has nothing to prove")
	}
	return recipe
}

// TestDevBuildFrontendKeepsTheTrackedPlaceholder runs the real recipe against
// a fixture laid out like the repository and proves web/dist survives it with
// the tracked file still in place.
func TestDevBuildFrontendKeepsTheTrackedPlaceholder(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "web", "dist")
	generated := filepath.Join(root, "web", "admin", ".output", "public")

	// web/dist as a developer finds it after a previous build: the tracked
	// placeholder, plus stale generated output the recipe still has to clear.
	writeFile(t, filepath.Join(dist, placeholder), "")
	writeFile(t, filepath.Join(dist, "index.html"), "stale")
	writeFile(t, filepath.Join(dist, "_nuxt", "stale.js"), "stale")
	// Stale output that happens to carry the placeholder's name. Only the one
	// at web/dist/PLACEHOLDER is tracked, so this copy is ordinary stale output and
	// has to go. Sparing it by name instead of by path breaks differently on
	// every find: BSD leaves it and its directory behind, GNU and bfs abort the
	// recipe with "Directory not empty".
	writeFile(t, filepath.Join(dist, "_nuxt", placeholder), "stale")

	// What `npx nuxt generate` would have left behind, including a dotfile —
	// `cp -r src/* dst` drops those silently, so the recipe has to copy in a
	// way that does not.
	writeFile(t, filepath.Join(generated, "index.html"), "fresh")
	writeFile(t, filepath.Join(generated, "_nuxt", "app.js"), "fresh")
	writeFile(t, filepath.Join(generated, ".nojekyll"), "")

	for _, command := range devBuildFrontendRecipe(t) {
		if strings.Contains(command, nuxtGenerate) {
			continue
		}
		// A make variable reaching the shell verbatim would be silently wrong
		// rather than loud, so refuse it instead of running it.
		if strings.Contains(command, "$(") || strings.Contains(command, "${") {
			t.Fatalf("the recipe line %q expands a make variable, which this test runs unexpanded; teach it that line before relying on this result", command)
		}

		// Make runs each recipe line in its own shell, from the directory it
		// was invoked in.
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("recipe line %q failed: %v\n%s", command, err, output)
		}
	}

	if _, err := os.Stat(filepath.Join(dist, placeholder)); err != nil {
		t.Errorf("web/dist/%s did not survive `make dev-build-frontend`: %v\n"+
			"It is tracked, so every developer who builds the frontend now carries a spurious deletion, "+
			"and committing it breaks `//go:embed all:dist` for every consumer of the module.", placeholder, err)
	}

	// The recipe still has to do its actual job, or "keep the placeholder"
	// would be satisfiable by doing nothing at all.
	assertFileContains(t, filepath.Join(dist, "index.html"), "fresh")
	assertFileContains(t, filepath.Join(dist, "_nuxt", "app.js"), "fresh")

	if _, err := os.Stat(filepath.Join(dist, ".nojekyll")); err != nil {
		t.Errorf("web/dist/.nojekyll is missing, so the recipe copies the generated SPA in a way that drops dotfiles: %v", err)
	}
	for _, stale := range []string{"stale.js", placeholder} {
		if _, err := os.Stat(filepath.Join(dist, "_nuxt", stale)); err == nil {
			t.Errorf("web/dist/_nuxt/%s survived, so the recipe leaves output from the previous build behind", stale)
		}
	}
}

// TestPlaceholderIsTrackedAndDistIsNot pins the .gitignore pair the test above
// rests on. Drop the negation and the placeholder stops being tracked, at
// which point the recipe could delete it harmlessly and this suite would be
// guarding nothing.
func TestPlaceholderIsTrackedAndDistIsNot(t *testing.T) {
	gitignore, err := os.ReadFile("../../.gitignore")
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	for _, rule := range []string{"web/dist/*", "!web/dist/" + placeholder} {
		if !strings.Contains(string(gitignore), rule) {
			t.Errorf(".gitignore no longer declares %q; web/dist/%s is what keeps `//go:embed all:dist` compiling in a fresh clone", rule, placeholder)
		}
	}
}

// TestEmbedStillNeedsANonEmptyDist records why the placeholder has to exist at
// all. If this fails, the embed no longer depends on dist being non-empty and
// the constraint the tests above enforce is worth revisiting.
func TestEmbedStillNeedsANonEmptyDist(t *testing.T) {
	embed, err := os.ReadFile("../../web/embed.go")
	if err != nil {
		t.Fatalf("read web/embed.go: %v", err)
	}

	if !strings.Contains(string(embed), "//go:embed all:dist") {
		t.Errorf("web/embed.go no longer declares `//go:embed all:dist`; web/dist/%s exists only to satisfy that directive", placeholder)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path) // #nosec G304 -- path is built from t.TempDir()
	if err != nil {
		t.Errorf("read %s: %v", path, err)
		return
	}
	if string(got) != want {
		t.Errorf("%s holds %q, want %q; the freshly generated SPA did not land in web/dist", path, got, want)
	}
}
