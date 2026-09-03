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
// The build info is read differently depending on which of two questions the
// build can answer, and the main module path is what decides between them.
//
// # Inside the weos binary
//
// A main module version the toolchain DERIVED is not reported. From a working
// tree the toolchain does not leave that version alone, and what it derives
// reads as a release. Measured on Go 1.25 against this repository at
// v3.0.1-alpha21 plus nine commits, `go build ./cmd/weos` recorded the main
// module as
//
//	v3.0.1-beta.1.0.20260901054947-5a14625a3469
//
// — a beta nobody tagged, sorting above every version that exists, and which
// nobody can go get. Reporting `dev` for such a build is the honest answer,
// and the one this bug was filed to get.
//
// The exception is the one commit where the toolchain knows the truth: on a
// clean checkout sitting exactly on a tag, Go 1.24+ records that tag itself
// rather than deriving anything, so `git checkout v3.0.1-beta.2 && go build
// ./cmd/weos` can name its release with no stamp at all. A revision alone is
// therefore not enough to reject a version — verified on Go 1.25, a clean
// checkout at v1.2.3 records "v1.2.3", and one commit later records a derived
// pseudo-version. Three conditions separate the two: the tree is clean, the
// version is not a pseudo-version, and it names a real release. A dirty tree
// fails the first outright, and the toolchain marks it for good measure by
// appending +dirty to the recorded version.
//
// # Inside a binary that embeds weos
//
// mini-me-weos, finexity and ic-crm build their own main module and reach weos
// through pkg/cli, running this same root command and MCP server. Their main
// module version is their own tag, so reading it would make
// `go install github.com/wepala/mini-me-weos@v1.4.0` announce weos v1.4.0 in
// the MCP handshake — a weos release that does not exist. What the toolchain
// records for them instead is the weos entry in the dependency list, holding
// the version `go get` resolved.
//
// A pseudo-version IS reported from there, which is the opposite of the rule
// above, and for the reason that rule gives: a resolved dependency version is
// one somebody asked for and one anybody can go get again, where a derived
// main module version is neither. A dependency replaced by a local checkout or
// a go.work workspace records "(devel)" and names no release, so it reports
// dev.
package version

import (
	"runtime/debug"

	"golang.org/x/mod/module"
)

// version is the version stamped at link time. It is deliberately empty by
// default rather than "dev": empty is what tells Version that nothing was
// stamped, which is the only way it can know to fall through to the build
// info below. Defaulting it to "dev" here would shadow the module version a
// `go install` records for free, and would be indistinguishable from a build
// that stamped "dev" on purpose.
var version string

// modulePath is this module's path. It is what tells Version whether it is
// running inside the weos binary or inside one that embeds weos.
const modulePath = "github.com/wepala/weos/v3"

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
	// weos is a library as much as a binary, and the VCS settings below
	// describe whatever tree the MAIN module was built from. In a wrapper's
	// binary that tree is the wrapper's, so neither its version nor its commit
	// says anything about which weos is running.
	if info.Main.Path != modulePath {
		return dependencyVersion(info.Deps)
	}

	revision, modified := vcsStamp(info.Settings)
	if revision == "" {
		// No tree was involved, so the main module version is one the
		// toolchain was told rather than one it derived: a `go install
		// module@version`.
		if isPublishedVersion(info.Main.Version) {
			return info.Main.Version
		}
		return devVersion
	}
	if isExactTag(info.Main.Version, modified) {
		return info.Main.Version
	}
	return devVersion + suffix(revision, modified)
}

// isExactTag reports whether the main module version the toolchain recorded
// for a source build is a tag HEAD actually sits on, rather than one derived
// from the commit. Only a clean tree can be sitting on a tag, and only a
// version that is neither derived nor a placeholder names a release — see the
// package comment for what each of the three conditions rules out.
func isExactTag(moduleVersion string, modified bool) bool {
	return !modified && isPublishedVersion(moduleVersion) && !module.IsPseudoVersion(moduleVersion)
}

// dependencyVersion finds the version of weos that a binary embedding weos was
// built against. It reports dev when weos is absent from the dependency list —
// which should not happen inside a binary running this code, and is not worth
// guessing about if it does — and when the dependency is replaced by a local
// checkout, which names no release anybody can go get.
func dependencyVersion(deps []*debug.Module) string {
	for _, dep := range deps {
		if dep == nil || dep.Path != modulePath {
			continue
		}
		// A `replace` directive, or a go.work workspace, points the build at a
		// different module than the one required; its version is the one that
		// was actually built.
		if dep.Replace != nil {
			dep = dep.Replace
		}
		if isPublishedVersion(dep.Version) {
			return dep.Version
		}
		return devVersion
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

// vcsStamp reads the commit an unstamped source build came from and whether
// that tree was clean. An empty revision means the toolchain recorded none,
// which is what tells resolve that this build did not come from a tree the
// toolchain recognized.
//
// It records none in more cases than the flag name suggests: with
// -buildvcs=false, from an unpacked source archive, with no git on PATH, from
// `go run` — verified on Go 1.25, which is why `make run` reports a bare
// "dev" — and, also verified there, from inside a `git worktree`, where .git
// is a file rather than a directory. The last one is why a build made in this
// project's own worktrees reports a bare "dev".
func vcsStamp(settings []debug.BuildSetting) (revision string, modified bool) {
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return revision, modified
}

// suffix names the commit a dev version came from, so that two dev builds are
// tellable apart — which is the whole complaint that a fixed version string
// leaves unanswered.
func suffix(revision string, modified bool) string {
	if len(revision) > revisionLength {
		revision = revision[:revisionLength]
	}
	out := "+" + revision
	// A tree with uncommitted changes is not the commit it names, and a
	// version that hides that sends a debugger to the wrong source.
	if modified {
		out += ".dirty"
	}
	return out
}
