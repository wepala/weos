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

package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
)

// A stdio MCP session ends normally when the client closes stdin (io.EOF) or
// signals the process (ctx canceled). Those must read as clean shutdowns so
// `weos mcp` exits 0; a genuine failure must still propagate.
func TestIsCleanShutdown(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	live := context.Background()

	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"context canceled via ctx", canceled, errors.New("anything"), true},
		{"context canceled via err", live, context.Canceled, true},
		{"plain io.EOF", live, io.EOF, true},
		{"wrapped io.EOF", live, fmt.Errorf("read: %w", io.EOF), true},
		// The SDK joins the EOF cause with %v, so io.EOF is not in the chain —
		// the "server is closing" message is the only signal.
		{"server is closing: EOF (%v-joined)", live, errors.New("server is closing: EOF"), true},
		{"real failure", live, errors.New("database is locked"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCleanShutdown(tc.ctx, tc.err); got != tc.want {
				t.Errorf("isCleanShutdown(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
