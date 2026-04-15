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
func TestStaticFS_DefaultEmbed(t *testing.T) {
	t.Parallel()
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
func TestSetStaticFS_Nil(t *testing.T) {
	// MUST NOT be converted to t.Parallel — see TestSetStaticFS_RoundTripsToServedContent.
	// (No cleanup needed; the panic prevents the assignment.)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("SetStaticFS(nil) should panic, but returned normally")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value should be a string with a clear message; got %T: %v", r, r)
		}
		if !strings.Contains(msg, "nil") {
			t.Errorf("panic message should mention nil; got %q", msg)
		}
	}()

	web.SetStaticFS(nil)
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
