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

package web

import (
	"embed"
	"io/fs"
)

// embeddedAdmin holds the compiled admin SPA. Don't read this var
// directly — go through StaticFS() so a host binary's SetStaticFS
// replacement takes effect.
//
//go:embed all:dist
var embeddedAdmin embed.FS

// staticFS is the FS the static middleware actually reads from.
// Defaults to the embedded admin; thin-wrap host binaries can replace
// it via SetStaticFS before the serve command builds the HTTP stack.
var staticFS fs.FS = embeddedAdmin

// StaticFS returns the active admin static filesystem. Consumers
// (notably the serve command's static middleware wiring) must call
// this rather than reading a package-level value, so a host binary's
// SetStaticFS replacement takes effect.
func StaticFS() fs.FS { return staticFS }

// SetStaticFS replaces the admin static filesystem.
//
// Intended use: a thin-wrap host binary (e.g. services/ic-crm) embeds
// its own Nuxt-layer dist and calls SetStaticFS in main() before
// invoking the serve command. The replacement FS must be rooted to
// expose `dist/` (matching the embedded layout and the StaticConfig.Root
// the serve command passes to the middleware).
//
// Must be called before the serve command constructs the HTTP stack.
// Once the static middleware is wired, it captures the FS at config
// time — later SetStaticFS calls cannot reach already-running handlers.
//
// Panics if fsys is nil. A nil FS would crash the static middleware
// with a less actionable error during request serving; failing fast
// at the setter call site (in main()) gives a clear stack trace at
// the point of the bug.
//
// See docs/decisions/admin-index-override.md for the design rationale.
func SetStaticFS(fsys fs.FS) {
	if fsys == nil {
		panic("web.SetStaticFS: fsys is nil; pass a non-nil fs.FS rooted at dist/")
	}
	staticFS = fsys
}
