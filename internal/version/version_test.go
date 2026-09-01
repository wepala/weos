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

package version

import (
	"runtime/debug"
	"testing"
)

// buildInfo is the shape the toolchain hands back for a given main-module
// version and set of VCS settings. resolve reads nothing else from it, so a
// literal is a complete stand-in for a real build and the branches below can
// be driven without building a binary per case.
func buildInfo(mainVersion string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{
		Main:     debug.Module{Path: "github.com/wepala/weos/v3", Version: mainVersion},
		Settings: settings,
	}
}

func vcs(revision string, modified bool) []debug.BuildSetting {
	dirty := "false"
	if modified {
		dirty = "true"
	}
	return []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: revision},
		{Key: "vcs.modified", Value: dirty},
	}
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{
			// The release path: `make build` and the Dockerfile pass the tag
			// at link time, and it wins outright. Nothing else can name the
			// tag of a build made from a working tree.
			name:    "a stamped version wins",
			stamped: "v3.0.1-beta.2",
			info:    buildInfo("(devel)", vcs("d34db33fd34db33fd34db33f", false)...),
			ok:      true,
			want:    "v3.0.1-beta.2",
		},
		{
			// The `go install github.com/wepala/weos/v3/cmd/weos@v3.0.1-beta.2`
			// path. No flags can be passed there, so the module version the
			// toolchain recorded is the only thing that knows the tag.
			name: "an installed module reports its module version",
			info: buildInfo("v3.0.1-beta.2"),
			ok:   true,
			want: "v3.0.1-beta.2",
		},
		{
			name: "a source build names its commit",
			info: buildInfo("(devel)", vcs("d34db33fd34db33fd34db33f", false)...),
			ok:   true,
			want: "dev+d34db33fd34d",
		},
		{
			// The main module version below is not invented: it is what Go
			// 1.25 recorded for `go build ./cmd/weos` in a checkout of this
			// repository at v3.0.1-alpha21 plus nine commits. It reads as a
			// beta release, sorts above every tag that exists, and nobody can
			// go get it. A revision alongside it is the signal that the
			// toolchain derived it rather than that anyone asked for it, so
			// the commit is what gets reported.
			name: "a derived module version is not reported as a release",
			info: buildInfo("v3.0.1-beta.1.0.20260901054947-d34db33fd34d", vcs("d34db33fd34db33fd34db33f", false)...),
			ok:   true,
			want: "dev+d34db33fd34d",
		},
		{
			// A build from a tree with uncommitted changes is not the commit
			// it names, and saying so is the difference between a reproducible
			// answer and a misleading one.
			name: "a dirty source build says so",
			info: buildInfo("(devel)", vcs("d34db33fd34db33fd34db33f", true)...),
			ok:   true,
			want: "dev+d34db33fd34d.dirty",
		},
		{
			// -buildvcs=false, an unpacked source archive, or a test binary:
			// the toolchain records no revision and there is nothing honest to
			// add.
			name: "a build with no VCS stamp is plain dev",
			info: buildInfo("(devel)"),
			ok:   true,
			want: "dev",
		},
		{
			name: "a build recording no module version is dev",
			info: buildInfo(""),
			ok:   true,
			want: "dev",
		},
		{
			name: "a build recording an unknown module version is dev",
			info: buildInfo("unknown"),
			ok:   true,
			want: "dev",
		},
		{
			// ReadBuildInfo fails for a binary the toolchain did not build.
			// Reporting dev is the honest answer; panicking on a nil info
			// would take the process down over a version string.
			name: "unreadable build info is dev",
			info: nil,
			ok:   false,
			want: "dev",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.stamped, tc.info, tc.ok); got != tc.want {
				t.Errorf("resolve() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestVersionIsNeverEmpty pins the one property every caller depends on: an
// MCP handshake with an empty version field, or a `weos --version` that prints
// nothing, is worse than a wrong version because it reads as a broken binary.
func TestVersionIsNeverEmpty(t *testing.T) {
	if Version() == "" {
		t.Error("Version() is empty; every call site presents this string to a user or an MCP client")
	}
}
