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

// Package version reports the version of the running weos binary. It is the
// single source of truth for that string: the CLI's `--version` flag, the MCP
// server's handshake and the in-process tool-listing client all read Version,
// so they cannot drift from each other the way three hardcoded literals did.
//
// Two mechanisms can name a version, and neither covers the other's case, so
// this package uses both.
//
// The linker stamp is what a release build uses. `make build`, `make
// build-embedded` and the Dockerfile pass
// -X github.com/wepala/weos/v3/internal/version.version=<tag>, with the tag
// from `git describe`. Nothing else can name the tag of a binary built from a
// working tree: the toolchain records that main module as "(devel)".
//
// runtime/debug.ReadBuildInfo is what a consumer gets for free. Someone who
// runs `go install github.com/wepala/weos/v3/cmd/weos@v3.0.1-beta.2` passes no
// flags and builds no Makefile target, and the stamp is empty for them — but
// the toolchain has recorded the exact module version they asked for, so the
// binary can still name its tag.
//
// The stamp wins where both are present, because it is the only one of the two
// that a build deliberately set. A `go install` never reaches that branch,
// since no flags can be passed on that path at all.
//
// The build info is trusted only for a build that did NOT come from a working
// tree, which is what the absence of a vcs.revision setting means. From a tree
// the toolchain does not leave the main module version alone, it derives one,
// and what it derives reads as a release. Measured on Go 1.25 against this
// repository at v3.0.1-alpha21 plus nine commits, `go build ./cmd/weos`
// recorded the main module as
//
//	v3.0.1-beta.1.0.20260901054947-5a14625a3469
//
// — a beta nobody tagged, sorting above every version that exists, and which
// nobody can go get. Reporting `dev` for such a build is the honest answer,
// and the one this bug was filed to get.
package version

import "runtime/debug"

// version is the version stamped at link time. It is deliberately empty by
// default rather than "dev": empty is what tells Version that nothing was
// stamped, which is the only way it can know to fall through to the build
// info below. Defaulting it to "dev" here would shadow the module version a
// `go install` records for free, and would be indistinguishable from a build
// that stamped "dev" on purpose.
var version string

// devVersion is what an unstamped build with nothing else to go on reports. It
// is not a version anyone published, which is the point: a build that cannot
// name a release must not claim one.
const devVersion = "dev"

// revisionLength is how much of a commit hash the dev version carries. Twelve
// hex characters is what `git describe` and the Go module proxy both use for a
// pseudo-version, and it is far past the point of ambiguity in this repository.
const revisionLength = 12

// Version returns the version of the running binary. It never returns an empty
// string: every caller presents it to a person or to an MCP client, and an
// empty version field reads as a broken binary rather than an unknown one.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	return resolve(version, info, ok)
}

// resolve holds the decision so it can be tested against every kind of build
// without producing one of each. See the package comment for why the stamp
// outranks the build info.
func resolve(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "" {
		return stamped
	}
	if !ok || info == nil {
		return devVersion
	}
	// A revision means the toolchain built this from a working tree, so the
	// main module version it recorded is one it derived rather than one anyone
	// published. Name the commit instead.
	if suffix := vcsSuffix(info.Settings); suffix != "" {
		return devVersion + suffix
	}
	if isPublishedVersion(info.Main.Version) {
		return info.Main.Version
	}
	return devVersion
}

// isPublishedVersion reports whether the main module version the toolchain
// recorded names a release someone can go get. `go install module@version`
// records the version asked for; a build from a source tree records "(devel)",
// a test binary records nothing, and a binary built outside module mode
// records "unknown". None of those three is a version to report.
func isPublishedVersion(moduleVersion string) bool {
	return moduleVersion != "" && moduleVersion != "(devel)" && moduleVersion != "unknown"
}

// vcsSuffix names the commit an unstamped source build came from, so that two
// dev builds are tellable apart — which is the whole complaint that a fixed
// version string leaves unanswered. An empty result means the toolchain
// recorded no revision, which is what tells resolve that this build did not
// come from a tree the toolchain recognized.
//
// It records none in more cases than the flag name suggests: with
// -buildvcs=false, from an unpacked source archive, with no git on PATH, and —
// verified against Go 1.25 — from inside a `git worktree`, where .git is a file
// rather than a directory. The last one is why a build made in this project's
// own worktrees reports a bare "dev".
func vcsSuffix(settings []debug.BuildSetting) string {
	revision, modified := "", false
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}

	if len(revision) > revisionLength {
		revision = revision[:revisionLength]
	}
	suffix := "+" + revision
	// A tree with uncommitted changes is not the commit it names, and a
	// version that hides that sends a debugger to the wrong source.
	if modified {
		suffix += ".dirty"
	}
	return suffix
}
