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

package agents

import (
	"testing"
	"time"

	"google.golang.org/adk/v2/runner"
)

// SetRunnerPlugins installs the process-wide plugin config RunAgent applies to
// every run; the zero value clears it.
func TestSetRunnerPlugins(t *testing.T) {
	t.Cleanup(func() { SetRunnerPlugins(runner.PluginConfig{}) })

	if runnerPlugins.CloseTimeout != 0 || runnerPlugins.Plugins != nil {
		t.Fatalf("expected no plugins by default, got %+v", runnerPlugins)
	}
	SetRunnerPlugins(runner.PluginConfig{CloseTimeout: 5 * time.Second})
	if runnerPlugins.CloseTimeout != 5*time.Second {
		t.Errorf("SetRunnerPlugins did not install the config: %+v", runnerPlugins)
	}
	SetRunnerPlugins(runner.PluginConfig{})
	if runnerPlugins.CloseTimeout != 0 {
		t.Errorf("the zero value should clear the plugins, got %+v", runnerPlugins)
	}
}
