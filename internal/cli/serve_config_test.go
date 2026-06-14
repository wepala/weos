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

package cli

import (
	"testing"

	"github.com/wepala/weos/v3/internal/config"
)

// loadServeConfig is the ONLY place that decides whether this process runs the
// background subscribers. Short-lived CLI commands must never start workers,
// even when WORKER_RUN_IN_PROCESS is set in a shared environment — so the var
// is honored here and nowhere in Config.LoadFromEnvironment.
func TestLoadServeConfig_RunInProcess(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string // "" means unset
		want bool
	}{
		{"default enables workers", "", true},
		{"explicit true", "true", true},
		{"explicit false runs API-only", "false", false},
		{"unparseable falls back to default", "garbage", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				// t.Setenv can't unset; clear by setting empty (the code treats
				// empty as unset via os.Getenv == "").
				t.Setenv("WORKER_RUN_IN_PROCESS", "")
			} else {
				t.Setenv("WORKER_RUN_IN_PROCESS", tc.env)
			}
			cfg = &CLIConfig{Config: config.Default()}

			got := loadServeConfig()
			if got.Worker.RunInProcess != tc.want {
				t.Errorf("RunInProcess = %v, want %v", got.Worker.RunInProcess, tc.want)
			}
		})
	}
}
