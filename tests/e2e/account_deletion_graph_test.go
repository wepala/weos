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

//go:build oxigraph_embedded

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wepala/weos/v3/internal/config"

	"github.com/cucumber/godog"
)

// embeddedGraphBuilt reports whether this binary carries the embedded graph
// store. It does, so the @requires-embedded-graph scenarios run.
const embeddedGraphBuilt = true

// registerGraphSteps defines the steps of the two @requires-embedded-graph
// scenarios (wm-ywv8d). They boot the same session-authenticated instance as
// every other scenario, with the embedded store and the background workers
// in-process so the oxigraph group projects — the drain in the erasure waits
// for it, which is what makes the graph state deterministic here.
func (w *deletionWorld) registerGraphSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a per-account WeOS instance where password sign-in is enabled and requests are authenticated by their session$`,
		func() error { return w.bootWithGraph(true) })
	sc.Step(`^a WeOS instance with one shared knowledge-graph store where password sign-in is enabled and requests are authenticated by their session$`,
		func() error { return w.bootWithGraph(false) })
	sc.Step(`^"([^"]*)" has a knowledge-graph store directory of its own$`, w.hasGraphDirectory)
	sc.Step(`^no knowledge-graph store directory exists for "([^"]*)"$`, w.noGraphDirectory)
	sc.Step(`^no knowledge-graph store directory is created for it by a later request$`, w.noGraphDirectoryAfterARequest)
	sc.Step(`^the knowledge graph no longer returns the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`,
		func(_, name, _ string) error { return w.graphHolds(name, false) })
	sc.Step(`^the knowledge graph still returns the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`,
		func(_, name, _ string) error { return w.graphHolds(name, true) })
}

// bootWithGraph boots the deletion instance with the embedded graph: one
// store per account under graphBase, or one shared store.
func (w *deletionWorld) bootWithGraph(perAccount bool) error {
	w.configure = func(cfg *config.Config) {
		cfg.Worker.RunInProcess = true
		if perAccount {
			w.graphBase = filepath.Join(w.tmpDir, "graph")
			cfg.Oxigraph.AccountStorePath = w.graphBase
		} else {
			cfg.Oxigraph.Path = filepath.Join(w.tmpDir, "graph-single")
		}
	}
	return w.bootDeletion(false)
}

func (w *deletionWorld) graphDirectoryOf(name string) (string, error) {
	id, ok := w.accounts[name]
	if !ok {
		return "", fmt.Errorf("no account named %q has been staged", name)
	}
	if w.graphBase == "" {
		return "", fmt.Errorf("the instance was not booted with per-account graph stores")
	}
	return filepath.Join(w.graphBase, id), nil
}

// hasGraphDirectory waits for the oxigraph group to open the account's store,
// which it does when it projects the account's first resource.
func (w *deletionWorld) hasGraphDirectory(name string) error {
	dir, err := w.graphDirectoryOf(name)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no graph store directory appeared for %q at %s", name, dir)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (w *deletionWorld) noGraphDirectory(name string) error {
	dir, err := w.graphDirectoryOf(name)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		return fmt.Errorf("a graph store directory still exists for %q at %s (stat: %v)", name, dir, statErr)
	}
	return nil
}

// noGraphDirectoryAfterARequest makes one more request as the person, with
// the session they held, and checks nothing reopened the store for the
// deleted account: a refused request must not touch the graph.
func (w *deletionWorld) noGraphDirectoryAfterARequest() error {
	p, err := w.current()
	if err != nil {
		return err
	}
	if err := w.request(p, http.MethodGet, projectsPath, ""); err != nil {
		return err
	}
	if p.lastAnswer.status == http.StatusOK {
		return fmt.Errorf("a request with the deleted account's session was served: %s", describe(p.lastAnswer))
	}
	var name string
	for n, id := range w.accounts {
		if id == w.deletedAccountID {
			name = n
		}
	}
	if name == "" {
		return fmt.Errorf("no deleted account is on record")
	}
	return w.noGraphDirectory(name)
}

// graphHolds asks the shared store whether any subject carries the resource's
// name. The name is the one handle the scenario has: the resource row is gone
// once its account is, so its URN cannot be looked up afterwards.
func (w *deletionWorld) graphHolds(name string, want bool) error {
	if w.graphs == nil || !w.graphs.Active() {
		return fmt.Errorf("the instance has no active knowledge-graph store")
	}
	ctx := context.Background()
	store, err := w.graphs.ForAccount(ctx, "")
	if err != nil {
		return fmt.Errorf("could not reach the shared store: %w", err)
	}
	query := fmt.Sprintf("ASK { ?s ?p %q }", name)
	deadline := time.Now().Add(15 * time.Second)
	for {
		result, err := store.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("the graph query failed: %w", err)
		}
		if result.Boolean != nil && *result.Boolean == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the knowledge graph holds %q: %v, want %v", name, result.Boolean != nil && *result.Boolean, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
