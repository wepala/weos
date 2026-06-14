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

package gorm

import (
	"io"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestGormConfig_LogsToStderrNotStdout guards the `weos mcp` stdio transport:
// it speaks JSON-RPC over stdout, so ANY byte GORM writes to stdout corrupts
// the protocol stream and breaks the MCP client. GORM's default logger logs to
// stdout; gormConfig must redirect it to stderr.
//
// Deliberately NOT parallel: it swaps the os.Stdout/os.Stderr globals, and
// serial tests run to completion before any t.Parallel() test in the package
// resumes, so there is no interference.
func TestGormConfig_LogsToStderrNotStdout(t *testing.T) {
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	// Swap BEFORE building the config: log.New captures os.Stderr by value.
	os.Stdout, os.Stderr = outW, errW
	restore := func() { os.Stdout, os.Stderr = origOut, origErr }
	defer restore()

	db, err := gorm.Open(sqlite.Open(":memory:"), gormConfig())
	if err != nil {
		restore()
		t.Fatalf("open: %v", err)
	}
	// A query against a missing table is a real error GORM logs at Error level
	// (unlike RecordNotFound, which gormConfig ignores) — forcing the logger to
	// emit so we can see which stream it writes to.
	_ = db.Exec("SELECT * FROM table_that_does_not_exist").Error

	restore()
	_ = outW.Close()
	_ = errW.Close()
	outBytes, _ := io.ReadAll(outR)
	errBytes, _ := io.ReadAll(errR)

	// Two complementary regressions are covered:
	//   - The stderr assertion fires if gormConfig were reverted to the bare
	//     &gorm.Config{}: GORM's default logger captured the real os.Stdout at
	//     package init (before this swap), so nothing reaches the piped stderr.
	//   - The stdout assertion fires if gormConfig itself were changed to build
	//     its logger over os.Stdout: it captures the swapped stdout at call time.
	if len(outBytes) != 0 {
		t.Errorf("GORM logger wrote to stdout (corrupts stdio MCP protocol):\n%s", outBytes)
	}
	if !strings.Contains(string(errBytes), "table_that_does_not_exist") {
		t.Errorf("expected the GORM error log on stderr, got: %q", errBytes)
	}
}
