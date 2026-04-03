# Setup Project: $ARGUMENTS

You are creating a new project from the WeOS v3 template. This skill clones the WeOS repo, renames the Go module, updates all references, and prepares a clean project ready for development.

---

## Phase 1: Parse & Confirm Configuration

### Step 1: Validate Project Name

The project name is: **$ARGUMENTS**

If `$ARGUMENTS` is empty, ask the user:
> "What would you like to name your project? Use lowercase letters, numbers, and hyphens (e.g., `ic-crm`, `my-app`, `church-site`)."

Validate the project name:
- Must be lowercase alphanumeric with hyphens only (regex: `^[a-z][a-z0-9-]*$`)
- Must NOT be `"weos"` (that's the template itself)
- Must be at least 2 characters

If invalid, explain the constraint and ask again.

Store the validated name as `$NAME` for the rest of this skill.

### Step 2: Ask Configuration Questions

Ask the user the following (present defaults in brackets):

1. **Target directory** — Where should the project be created? [default: `~/GolandProjects/$NAME`]
2. **GitHub repo** — Should I create a GitHub repo? If yes, public or private? [default: private]
3. **GitHub org/user** — Which GitHub org or user should own the repo? [default: the authenticated `gh` user]
4. **Pericarp `replace` directive** — The `go.mod` has a local `replace` directive for the `pericarp` library pointing to `/Users/akeem/GolandProjects/vine-os/core/pericarp`. Should I:
   - **keep** it as-is (you have the same local path)
   - **update** it to a different local path (you'll provide the path)
   - **remove** it (use the published module from GitHub)

   [default: keep]

### Step 3: Pre-flight Checks

Run these checks before proceeding. Stop and report if any fail:

1. Target directory does NOT already exist
2. `git` is available on PATH
3. `gh` CLI is available and authenticated (`gh auth status`)
4. `go` is available on PATH
5. The WeOS v3 branch is accessible: `git ls-remote https://github.com/wepala/weos.git v3`

If `gh` is not authenticated but the user wants a GitHub repo, ask them to run `gh auth login` first.

---

## Phase 2: Clone & Configure Git

### Step 1: Clone the Template

```bash
git clone -b v3 https://github.com/wepala/weos.git TARGET_DIR
cd TARGET_DIR
```

### Step 2: Reconfigure Remotes

```bash
git remote rename origin upstream
```

### Step 3: Create GitHub Repo (if requested)

If the user opted for a GitHub repo:

```bash
gh repo create ORG_OR_USER/$NAME --private --source=. --remote=origin
```

Use `--public` if they chose public visibility.

### Step 4: Rename the Branch

```bash
git branch -m v3 main
```

If an origin remote exists:
```bash
git push -u origin main
```

---

## Phase 3: Rename Go Module

This is the core of the setup. Perform all replacements in the cloned project directory.

### Step 1: `go.mod`

**Line 1** — Module declaration:
- `module weos` → `module $NAME`

**replace directive** (currently line 97) — based on user's choice in Phase 1:
- **keep**: leave as-is
- **update**: change the path to the user-provided path
- **remove**: delete the entire `replace` line

### Step 2: Go Import Paths (~48 files)

Find all `.go` files containing `"weos/` and replace every occurrence:
- `"weos/` → `"$NAME/`

This covers all import paths across the project. Use a tool like `grep -rl '"weos/' --include='*.go' .` to find them, then perform the replacements.

**Important:** Only replace the import path prefix `"weos/`, not other occurrences of the word "weos" (like in strings, comments, or identifiers).

### Step 3: Rename `cmd/weos/` Directory

```bash
mv cmd/weos cmd/$NAME
```

### Step 4: `Makefile`

Update the build target and run target to use the new binary name and cmd path:

- Line 26: `go build -o bin/weos ./cmd/weos` → `go build -o bin/$NAME ./cmd/$NAME`
- Line 29: `go run ./cmd/weos serve` → `go run ./cmd/$NAME serve`

### Step 5: `internal/cli/root.go`

- Line 25: `Use: "weos"` → `Use: "$NAME"`
- Line 26: `Short: "WeOS - AI-powered website system"` → `Short: "$NAME - powered by WeOS"`
- Line 27: Update the `Long` description to reference the new project name

### Step 6: `internal/mcp/server.go`

- Line 113: `Name: "weos"` → `Name: "$NAME"`
- Line 114: `Title: "WeOS MCP Server"` → `Title: "$NAME MCP Server"`

### Step 7: `internal/config/config.go`

- Line 35 (comment): `"weos.db"` → `"$NAME.db"`
- Line 108 (default value): `DatabaseDSN: "weos.db"` → `DatabaseDSN: "$NAME.db"`

### Step 8: `application/auth_providers.go`

- Line 56: `"weos-session"` → `"$NAME-session"`

### Step 9: `web/admin/package.json`

- Line 2: `"name": "weos-admin"` → `"name": "$NAME-admin"`

### Step 10: `.github/workflows/ci.yml`

The CI workflow has stale build targets. Update them to use the unified binary:

- Line 21: `go build -o bin/weos-api ./cmd/api` → `go build -o bin/$NAME ./cmd/$NAME`
- Lines 23-24: Remove the separate "Build CLI" step entirely (there's only one binary now)

---

## Phase 4: Optional Doc Updates

Ask the user:
> "Would you like me to also update documentation files (CLAUDE.md, README.md) and the `/add-concept` command template to use the new project name? **Recommended: yes**"

If yes, perform these updates:

### CLAUDE.md
- Replace references to "WeOS" with the project name where appropriate (project description, binary name references)
- Update `cmd/weos` paths to `cmd/$NAME`
- Update `weos.db` references to `$NAME.db`
- Update `weos serve` / `weos mcp` command examples to `$NAME serve` / `$NAME mcp`

### README.md
- Update project name and description references

### `.claude/commands/add-concept.md`
- This file contains ~9 hardcoded `"weos/` import path references in its code templates
- Replace `"weos/` → `"$NAME/` in the template code blocks

---

## Phase 5: Verify

### Step 1: Tidy Dependencies

```bash
cd TARGET_DIR
go mod tidy
```

If this fails, investigate and fix. Common issue: the `replace` directive path doesn't exist on this machine.

### Step 2: Build

```bash
make build
```

Report pass or fail.

### Step 3: Check for Remaining References

Search for any remaining `"weos/` import references that were missed:

```bash
grep -r '"weos/' --include='*.go' .
```

If any are found, fix them and rebuild.

### Step 4: Check for Remaining `weos.db` References

```bash
grep -rn 'weos\.db' --include='*.go' .
```

If any are found outside of comments, fix them.

---

## Phase 6: Commit & Summary

### Step 1: Commit

```bash
git add -A
git commit -m "chore: initialize $NAME project from WeOS v3 template

- Renamed Go module from weos to $NAME
- Updated all import paths, CLI names, config defaults
- Updated CI workflow for unified binary
- Ready for development"
```

### Step 2: Push (if origin exists)

```bash
git push -u origin main
```

### Step 3: Print Summary

Print a summary like this:

```
Project "$NAME" is ready!

  Location:    TARGET_DIR
  GitHub:      https://github.com/ORG/$NAME (or "no remote")
  Database:    $NAME.db (SQLite default)
  Binary:      bin/$NAME

Files modified: XX
Next steps:
  1. cd TARGET_DIR
  2. make run          — start the dev server
  3. $NAME mcp         — start the MCP server
  4. Edit CLAUDE.md to describe your project's domain

To sync with upstream WeOS updates:
  git fetch upstream
  git merge upstream/v3 --allow-unrelated-histories
```