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

package web_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	apimw "weos/api/middleware"
	"weos/web"

	"github.com/labstack/echo/v4"
)

// TestStaticFS_DefaultEmbed pins that the default StaticFS() (backed by
// the //go:embed all:dist directive on embeddedAdmin) exposes
// dist/index.html. Guards against a refactor that drops the embed
// directive, narrows the embed pattern, or re-points staticFS to an
// FS that doesn't include the admin shell — any of which would only
// surface at runtime when serving /.
//
// Not parallel: other tests in this file mutate the package-level
// staticFS via SetStaticFS. Marking this test parallel would create
// a read/write race on staticFS under -race.
func TestStaticFS_DefaultEmbed(t *testing.T) {
	if _, err := fs.Stat(web.StaticFS(), "dist/index.html"); err != nil {
		t.Fatalf("default StaticFS() should expose dist/index.html: %v", err)
	}
}

// TestSetStaticFS_RoundTripsToServedContent is the end-to-end smoke test
// for the swap mechanism a thin-wrap host binary will rely on. It pins
// the read-through path from SetStaticFS through StaticFS() into the
// static middleware. A regression where StaticFS() stops reading
// staticFS (e.g. a refactor that returns embeddedAdmin directly from
// the accessor) would break the override contract and surface here.
//
// Note: the static middleware legitimately captures the FS at config
// time, so this test calls SetStaticFS *before* wiring the middleware,
// matching the documented contract on SetStaticFS.
func TestSetStaticFS_RoundTripsToServedContent(t *testing.T) {
	// MUST NOT be converted to t.Parallel — SetStaticFS mutates a
	// package-level variable with no synchronization (the documented
	// contract is "call once from main() before serve"), so a parallel
	// run would race under -race and produce flaky failures.

	const sentinel = "HOST_OVERRIDE_SENTINEL"

	orig := web.StaticFS()
	t.Cleanup(func() { web.SetStaticFS(orig) })

	hostFS := fstest.MapFS{
		"dist/index.html": &fstest.MapFile{
			Data: []byte("<html><body>" + sentinel + "</body></html>"),
		},
	}
	web.SetStaticFS(hostFS)

	// Wire the Static middleware the same way internal/cli/serve.go
	// does: read web.StaticFS(), pass "dist" as Root.
	e := echo.New()
	e.Use(apimw.Static(apimw.StaticConfig{
		Filesystem: web.StaticFS(),
		Root:       "dist",
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), sentinel) {
		t.Fatalf("GET / body did not contain host sentinel %q; got: %q",
			sentinel, rec.Body.String())
	}
}

// TestSetStaticFS_LastWriteWins pins the idempotence semantics — the
// most recent SetStaticFS call wins. Cheap to maintain; prevents a
// future maintainer from "fixing" the setter to reject second calls
// (e.g., interpreting it as initialization-only).
func TestSetStaticFS_LastWriteWins(t *testing.T) {
	// MUST NOT be converted to t.Parallel — see TestSetStaticFS_RoundTripsToServedContent.

	orig := web.StaticFS()
	t.Cleanup(func() { web.SetStaticFS(orig) })

	first := fstest.MapFS{"dist/marker.txt": &fstest.MapFile{Data: []byte("first")}}
	second := fstest.MapFS{"dist/marker.txt": &fstest.MapFile{Data: []byte("second")}}

	web.SetStaticFS(first)
	web.SetStaticFS(second)

	got, err := fs.ReadFile(web.StaticFS(), "dist/marker.txt")
	if err != nil {
		t.Fatalf("read marker after two SetStaticFS calls: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("StaticFS() after two SetStaticFS calls returned %q; want %q (last-write-wins)",
			string(got), "second")
	}
}

// TestSetStaticFS_Nil pins the nil-handling contract: SetStaticFS panics
// at the call site rather than letting the nil propagate to the static
// middleware (which would then panic with a less actionable error
// during request serving). A host binary that hits this gets a clean
// stack trace at the bug — the SetStaticFS call in main().
//
// Covers both bare-nil (literal `nil` passed via the fs.FS interface)
// and typed-nil (a concrete nil pointer assigned to the fs.FS
// interface variable — Go's `x == nil` on the interface is false in
// this case, so the setter needs a reflection-based check).
func TestSetStaticFS_Nil(t *testing.T) {
	// MUST NOT be converted to t.Parallel — see TestSetStaticFS_RoundTripsToServedContent.
	// (No cleanup needed; the panic prevents the assignment.)

	assertPanics := func(t *testing.T, name string, call func()) {
		t.Helper()
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("%s: expected panic, returned normally", name)
			}
			msg, ok := r.(string)
			if !ok {
				t.Fatalf("%s: panic value should be a string; got %T: %v", name, r, r)
			}
			if !strings.Contains(msg, "nil") {
				t.Errorf("%s: panic message should mention nil; got %q", name, msg)
			}
		}()
		call()
	}

	t.Run("bare nil", func(t *testing.T) {
		assertPanics(t, "SetStaticFS(nil)", func() { web.SetStaticFS(nil) })
	})

	t.Run("typed-nil pointer", func(t *testing.T) {
		var p *fstest.MapFS // nil pointer to a concrete fs.FS implementer
		assertPanics(t, "SetStaticFS(typed-nil *fstest.MapFS)", func() { web.SetStaticFS(p) })
	})

	t.Run("typed-nil map", func(t *testing.T) {
		var m fstest.MapFS // nil map; fstest.MapFS is a map type
		assertPanics(t, "SetStaticFS(typed-nil fstest.MapFS)", func() { web.SetStaticFS(m) })
	})
}

// TestSetStaticFS_CleanupRestoresOriginal pins the t.Cleanup pattern
// used by the swap test: capturing the original FS, swapping, and
// restoring works end-to-end. If StaticFS() ever stopped read-through
// (e.g. cached the embed at first call), restoration would silently
// fail and other tests in this package would observe a swapped FS.
//
// Uses behavioral assertions (a sentinel file present only in the
// swapped FS) rather than identity comparison, since fstest.MapFS is
// a map type and not == comparable.
func TestSetStaticFS_CleanupRestoresOriginal(t *testing.T) {
	// MUST NOT be converted to t.Parallel — see TestSetStaticFS_RoundTripsToServedContent.

	orig := web.StaticFS()
	const sentinelPath = "dist/swap-only-sentinel.txt"
	swapped := fstest.MapFS{
		sentinelPath: &fstest.MapFile{Data: []byte("swapped")},
	}

	// Sanity: the original embed must NOT contain the sentinel — the
	// behavioral check below depends on this. If a future change adds
	// such a file under dist/, pick a different name.
	if _, err := fs.Stat(orig, sentinelPath); err == nil {
		t.Fatalf("test setup invalid: %s exists in original FS; pick another name", sentinelPath)
	}

	web.SetStaticFS(swapped)
	if _, err := fs.Stat(web.StaticFS(), sentinelPath); err != nil {
		t.Fatalf("after SetStaticFS(swapped), sentinel should be readable: %v", err)
	}

	web.SetStaticFS(orig)
	if _, err := fs.Stat(web.StaticFS(), sentinelPath); err == nil {
		t.Fatal("after SetStaticFS(orig), sentinel should be gone — restoration leaked the swapped FS")
	}
}
