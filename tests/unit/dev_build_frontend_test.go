package unit

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// cannot run: it shells out to the Nuxt build, which needs an installed
// node_modules and the network. Everything after it is plain file shuffling,
// and that is the half that loses the placeholder, so dropping this line
// leaves the behavior under test intact.
//
// It matches on the Nuxt command rather than the runner in front of it:
// anchored on `npx nuxt generate`, swapping npx for pnpm or yarn would stop
// the line being skipped and this test would go and run the frontend build.
const nuxtGenerate = "nuxt generate"

// repoRoot is this repository, seen from tests/unit. Several tests here pin
// the real tree rather than a fixture copy of it, because the failure they
// guard against is a file going missing from the repository itself.
const repoRoot = "../.."

// devBuildFrontendRecipe returns the shell commands `make dev-build-frontend`
// would run, read out of the real Makefile rather than restated here by the
// shared parser in makefile_test.go. Reading the real recipe is the point: a
// test that re-implemented it would keep passing after someone reintroduced
// `rm -rf web/dist`.
func devBuildFrontendRecipe(t *testing.T) []string {
	t.Helper()

	recipe, found := makefileRecipe(t, "dev-build-frontend")
	if !found {
		t.Fatal("the Makefile declares no dev-build-frontend target; either it was renamed or this test is stale")
	}
	if len(recipe) == 0 {
		t.Fatal("the dev-build-frontend recipe is empty; this test has nothing to prove")
	}
	return recipe
}

// writeFixture lays out a temp tree shaped like the repository just after a
// previous build: a web/dist holding the tracked placeholder and stale
// generated output, and the fresh output `nuxt generate` would have left.
func writeFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dist := filepath.Join(root, "web", "dist")
	generated := filepath.Join(root, "web", "admin", ".output", "public")

	writeFile(t, filepath.Join(dist, placeholder), placeholderContents)
	writeFile(t, filepath.Join(dist, "index.html"), "stale")
	writeFile(t, filepath.Join(dist, "_nuxt", "stale.js"), "stale")
	// Stale output that happens to carry the placeholder's name. Only the one
	// at web/dist/PLACEHOLDER is tracked, so this copy is ordinary stale output and
	// has to go. Sparing it by name instead of by path breaks differently on
	// every find: BSD leaves it and its directory behind, GNU and bfs abort the
	// recipe with "Directory not empty".
	writeFile(t, filepath.Join(dist, "_nuxt", placeholder), "stale")

	// What the Nuxt build would have left behind, including a dotfile —
	// `cp -r src/* dst` drops those silently, so the recipe has to copy in a
	// way that does not.
	writeFile(t, filepath.Join(generated, "index.html"), "fresh")
	writeFile(t, filepath.Join(generated, "_nuxt", "app.js"), "fresh")
	writeFile(t, filepath.Join(generated, ".nojekyll"), "")

	return root
}

// placeholderContents stands in for the real file's prose. It only has to be
// non-empty and recognizable: a restored placeholder has to come back with its
// committed contents, not as an empty file `touch` would leave.
const placeholderContents = "tracked, and the embed needs it\n"

// runDevBuildFrontend runs the real recipe against a fixture root, one line per
// shell, the way make does.
func runDevBuildFrontend(t *testing.T, root string) {
	t.Helper()

	for _, command := range devBuildFrontendRecipe(t) {
		if strings.Contains(command, nuxtGenerate) {
			continue
		}
		// A make variable reaching the shell verbatim would be silently wrong
		// rather than loud, so refuse it instead of running it. There is no
		// sibling guard for a continued line any more: the shared parser joins
		// a backslash continuation onto the line it continues, the way make
		// does, so `sh` is never handed half a command.
		if strings.Contains(command, "$(") || strings.Contains(command, "${") {
			t.Fatalf("the recipe line %q expands a make variable, which this test runs unexpanded; teach it that line before relying on this result", command)
		}
		// Make runs each recipe line in its own shell, from the directory it
		// was invoked in.
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = root
		// The recipe is trusted, but it is read off disk and run here, so keep
		// it inside the fixture: a `~/...` path in some future line resolves
		// under the temp root rather than in the developer's home directory.
		// PATH stays real — the recipe needs find, cp and mkdir.
		cmd.Env = []string{
			"HOME=" + root,
			"TMPDIR=" + root,
			"PATH=" + os.Getenv("PATH"),
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("recipe line %q failed: %v\n%s", command, err, output)
		}
	}
}

// TestDevBuildFrontendKeepsTheTrackedPlaceholder runs the real recipe against
// a fixture laid out like the repository and proves web/dist survives it with
// the tracked file still in place.
func TestDevBuildFrontendKeepsTheTrackedPlaceholder(t *testing.T) {
	requireShell(t)

	root := writeFixture(t)
	dist := filepath.Join(root, "web", "dist")

	runDevBuildFrontend(t, root)

	if _, err := os.Stat(filepath.Join(dist, placeholder)); err != nil {
		t.Errorf("web/dist/%s did not survive `make dev-build-frontend`: %v\n"+
			"It is tracked, so every developer who builds the frontend now carries a spurious deletion, "+
			"and committing it breaks `//go:embed all:dist` for every consumer of the module.", placeholder, err)
	} else {
		// Surviving as an empty file would still show up as a modification in
		// `git status`, so the contents have to be untouched too.
		assertFileContains(t, filepath.Join(dist, placeholder), placeholderContents)
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

// TestDevBuildFrontendRestoresADeletedPlaceholder covers the tree the fix above
// does not reach on its own: every developer who ran the old `rm -rf web/dist`
// recipe still has the placeholder deleted, and sparing a file that is already
// gone changes nothing. `touch` would only produce a modified file — the prose
// has to come back from git — so the recipe restores it before it sweeps.
func TestDevBuildFrontendRestoresADeletedPlaceholder(t *testing.T) {
	requireShell(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH, so the recipe's restore step cannot be exercised")
	}

	root := writeFixture(t)
	dist := filepath.Join(root, "web", "dist")

	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "web/dist/" + placeholder},
		{"-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "track the placeholder"},
	} {
		if output, code := gitIn(t, root, args...); code != 0 {
			t.Skipf("cannot build a git fixture (`git %s` exited %d): %s", strings.Join(args, " "), code, output)
		}
	}

	// The state the old recipe left behind.
	if err := os.Remove(filepath.Join(dist, placeholder)); err != nil {
		t.Fatalf("delete the fixture's placeholder: %v", err)
	}

	runDevBuildFrontend(t, root)

	assertFileContains(t, filepath.Join(dist, placeholder), placeholderContents)
}

// TestTheRealPlaceholderIsPresentAndTracked pins the file in this repository,
// not a fixture copy of it. Nothing else does: the recipe tests reason about a
// temp directory, and CI's Go jobs download the built SPA into web/dist before
// they compile, so `go build` passes there whether or not the tracked file
// exists. A branch that committed the deletion would merge cleanly — a delete
// against an unmodified file is not a conflict — and stay green until the next
// tagged version failed to build for everyone importing weos/v3/web.
func TestTheRealPlaceholderIsPresentAndTracked(t *testing.T) {
	relative := "web/dist/" + placeholder

	info, err := os.Stat(filepath.Join(repoRoot, "web", "dist", placeholder))
	if err != nil {
		t.Fatalf("%s is missing: %v\n"+
			"`//go:embed all:dist` does not compile against an empty directory, so a fresh clone or a "+
			"module zip without this file breaks every consumer importing weos/v3/web. "+
			"Restore it with `git checkout -- %s`.", relative, err, relative)
	}
	if info.Size() == 0 {
		t.Errorf("%s is empty, and `//go:embed all:dist` needs a file with contents in it", relative)
	}

	requireGitRepository(t)

	if output, code := gitIn(t, repoRoot, "ls-files", "--error-unmatch", relative); code != 0 {
		t.Errorf("git does not track %s: %s\n"+
			"An untracked placeholder is absent from a fresh clone and from the module zip, which is the "+
			"failure it exists to prevent.", relative, strings.TrimSpace(output))
	}

	// The .gitignore rules that keep this file trackable are order-dependent: a
	// negation only un-ignores when it follows the pattern that ignored the
	// path. Reversed, the rules ignore the placeholder — it survives in the
	// index for now, because ignore rules do not touch a tracked file, but
	// anyone who deletes it can no longer `git add` it back, which is the one
	// recovery the recipe and `make build` both tell them to make. Reading the
	// two rules out of .gitignore as text cannot see the order, so ask git how
	// the pair resolves.
	if isIgnored(t, relative) {
		t.Errorf(".gitignore ignores %s, so it cannot be added back once it is deleted\n"+
			"`web/dist/*` has to come before `!%s` — git honors a negation only after the rule it undoes.",
			relative, relative)
	}
	if !isIgnored(t, "web/dist/index.html") {
		t.Error("git no longer ignores web/dist/index.html, so a generated SPA can be committed by accident")
	}
}

// TestEmbedStillNeedsANonEmptyDist records why the placeholder has to exist at
// all. It asks the Go toolchain rather than reading web/embed.go: a directive
// someone commented out (`// //go:embed all:dist`) still contains the text, so
// a substring match would keep passing after the embed stopped existing.
func TestEmbedStillNeedsANonEmptyDist(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go toolchain is not on PATH, so `go list` cannot report what web/embed.go embeds")
	}

	cmd := exec.Command("go", "list", "-f",
		"{{range .EmbedPatterns}}pattern {{.}}\n{{end}}{{range .EmbedFiles}}file {{.}}\n{{end}}", "./web")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ask `go list` what ./web embeds: %v\n%s", err, output)
	}
	embedded := string(output)

	if !strings.Contains(embedded, "pattern all:dist\n") {
		t.Errorf("the web package no longer embeds all:dist, so web/dist/%s exists for nothing; `go list` reports:\n%s",
			placeholder, embedded)
	}
	if !strings.Contains(embedded, "file dist/"+placeholder+"\n") {
		t.Errorf("web/dist/%s is not among the files the embed resolves to, so an empty web/dist would stop the "+
			"module compiling for a consumer who never runs the Nuxt build; `go list` reports:\n%s",
			placeholder, embedded)
	}
}

// requireShell skips a test that runs Makefile recipe lines. The recipe is
// written for a POSIX shell, and a native Windows toolchain (not Git Bash, not
// WSL) has no `sh` to run them with.
func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the dev-build-frontend recipe runs through `sh`, which native Windows does not provide")
	}
}

// requireGitRepository skips when there is no repository to ask — a consumer
// running these tests out of a module zip has the files but no git metadata.
func requireGitRepository(t *testing.T) {
	t.Helper()
	if _, code := gitIn(t, repoRoot, "rev-parse", "--git-dir"); code != 0 {
		t.Skip("this tree is not a git repository, so git cannot be asked what it tracks")
	}
}

// gitIn runs one git command in dir and returns its combined output and exit
// code. It skips the test when git cannot be run at all, which is a missing
// toolchain rather than an answer about the tree.
func gitIn(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()

	cmd := exec.Command("git", args...) // #nosec G204 -- arguments are literals from this file
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(output), exitErr.ExitCode()
	}
	t.Skipf("cannot run `git %s`: %v", strings.Join(args, " "), err)
	return "", 0
}

// isIgnored reports what .gitignore says about path, ignoring whether git
// already tracks it. `--no-index` is what makes the answer mean anything here:
// by default check-ignore skips a tracked path and reports "not ignored" for
// it, since ignore rules do not apply to files already in the index — so the
// question would answer itself for the one file this suite is about, and stay
// green with the rules reversed. `-q` exits 0 when the rules ignore the path
// and 1 when they do not; any other code is git failing rather than answering.
func isIgnored(t *testing.T, path string) bool {
	t.Helper()

	output, code := gitIn(t, repoRoot, "check-ignore", "--no-index", "-q", path)
	switch code {
	case 0:
		return true
	case 1:
		return false
	default:
		t.Fatalf("git check-ignore %s exited %d: %s", path, code, output)
		return false
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
