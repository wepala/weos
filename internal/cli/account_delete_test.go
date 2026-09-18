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
	"context"
	"path/filepath"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/internal/config"

	"go.uber.org/fx"
)

// bankLinkRemover stands for the step an embedding service registers: it
// unlinks the account from something outside this instance, and only that
// binary knows how.
type bankLinkRemover struct{}

func (bankLinkRemover) Name() string { return "bank-links" }

func (bankLinkRemover) BeforeAccountErased(context.Context, application.ErasingAccount) error {
	return nil
}

// registerTestParticipant registers a participant for one test and takes it
// back afterwards, so the process-global list does not leak between tests.
func registerTestParticipant(t *testing.T) {
	t.Helper()
	erasureBefore, serveBefore := customErasureFxOptions, customFxOptions
	t.Cleanup(func() { customErasureFxOptions, customFxOptions = erasureBefore, serveBefore })
	RegisterErasureFxOptions(
		fx.Provide(application.AsAccountErasureParticipant(func() *bankLinkRemover { return &bankLinkRemover{} })),
	)
}

// erasureTestConfig is a configuration that opens a fresh store in a
// temporary directory.
func erasureTestConfig(t *testing.T) config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DatabaseDSN = filepath.Join(dir, "weos.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")
	cfg.LogLevel = "error"
	return cfg
}

// The operator command is the documented remedy for a deletion that failed
// part-way, so it has to run the same steps the API deletion runs. A graph
// built without the binary's registered options erases with an empty
// participant group — and an empty group looks exactly like an instance that
// registered none, so the account goes with the external link stranded and
// nothing left to find it by.
func TestAccountDelete_TheCommandsGraphCarriesTheRegisteredParticipants(t *testing.T) {
	registerTestParticipant(t)

	erasure, app, err := buildErasure(erasureTestConfig(t))
	if err != nil {
		t.Fatalf("build the erasure: %v", err)
	}
	stopApp(t, app)

	names := erasure.ParticipantNames()
	if len(names) != 1 || names[0] != "bank-links" {
		t.Fatalf("the command would run participants %v, want the registered bank-links step", names)
	}
}

// The same registration reaches the server's graph, so one list serves both
// the deletion a person asks for and the one an operator finishes.
func TestAccountDelete_ServeRunsTheSameRegisteredParticipants(t *testing.T) {
	registerTestParticipant(t)

	var erasure *application.AccountErasureService
	extra := append(serveFxOptions(), fx.Populate(&erasure))
	_, app, err := buildServer(erasureTestConfig(t), extra...)
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	stopApp(t, app)

	names := erasure.ParticipantNames()
	if len(names) != 1 || names[0] != "bank-links" {
		t.Fatalf("serve would run participants %v, want the registered bank-links step", names)
	}
}

func stopApp(t *testing.T, app *fx.App) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		if err := app.Stop(ctx); err != nil {
			t.Logf("stop the application: %v", err)
		}
	})
}
