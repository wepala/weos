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

package graph

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/fx"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"
	"github.com/wepala/weos/v3/internal/config"
)

// ProvideKnowledgeGraphStores selects the knowledge-graph store resolver for the
// container. It is the single-tenant/per-account fork:
//
//   - PER-ACCOUNT (OXIGRAPH_ACCOUNT_STORE_PATH set): a resolver that lazily opens
//     one embedded store per account under the base directory, so kg_* queries
//     and projections are isolated to the caller's account.
//   - SINGLE-TENANT (Path / URL / nop): the existing single store, wrapped so the
//     account is ignored — behavior is byte-for-byte unchanged.
//
// It reuses ProvideKnowledgeGraphStore for the single-tenant branch so that
// path's selection, degradation, and shutdown semantics stay in one place.
func ProvideKnowledgeGraphStores(
	cfg config.Config, logger entities.Logger, lc fx.Lifecycle,
) repositories.KnowledgeGraphStores {
	if cfg.Oxigraph.PerAccount() {
		return newPerAccountStores(cfg.Oxigraph.AccountStorePath, logger, lc)
	}
	return repositories.NewSingleKnowledgeGraphStores(ProvideKnowledgeGraphStore(cfg, logger, lc))
}

// perAccountStores lazily opens and caches one embedded KnowledgeGraphStore per
// account beneath a base directory (<base>/<accountID>). Growth is bounded by
// the number of accounts actually queried — not by request or event volume —
// and every open store is closed on shutdown so its RocksDB directory lock is
// released and the process can restart cleanly. (A hard cap with LRU eviction is
// a deliberate non-goal for now: the acceptance contract exercises many accounts
// plus a clean restart, and precise file-handle bounds belong in a unit test —
// see the #431 discussion.)
type perAccountStores struct {
	base   string
	logger entities.Logger

	mu   sync.Mutex
	open map[string]repositories.KnowledgeGraphStore
}

// newPerAccountStores builds the per-account resolver. If the binary lacks the
// oxigraph_embedded tag (per-account uses the embedded backend) or the base
// directory can't be created, it logs one ERROR and degrades to nop — the same
// soft-dependency contract the single-store provider uses for an unopenable
// path, so the twin still boots with the graph simply off.
func newPerAccountStores(
	base string, logger entities.Logger, lc fx.Lifecycle,
) repositories.KnowledgeGraphStores {
	if !oxigraph.EmbeddedAvailable() {
		logger.Error(context.Background(),
			"knowledge graph: per-account store path is set but this binary was not built with "+
				"the 'oxigraph_embedded' tag, using nop store", "path", base)
		return repositories.NewSingleKnowledgeGraphStores(NewNopStore())
	}
	// Refuse a base that could make Truncate destructive (root or the cwd): a
	// rebuild removes account-store dirs under the base, and pointing it at "/"
	// or "." would put unrelated data in blast range. Degrade to nop instead.
	if clean := filepath.Clean(base); clean == "/" || clean == "." || clean == "" {
		logger.Error(context.Background(),
			"knowledge graph: per-account store base is unsafe (root or cwd), using nop store", "path", base)
		return repositories.NewSingleKnowledgeGraphStores(NewNopStore())
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		logger.Error(context.Background(),
			"knowledge graph: failed to create per-account store base directory, falling back to nop",
			"path", base, "error", err)
		return repositories.NewSingleKnowledgeGraphStores(NewNopStore())
	}
	f := &perAccountStores{
		base:   base,
		logger: logger,
		open:   make(map[string]repositories.KnowledgeGraphStore),
	}
	// Close every open account store on shutdown so directory locks are dropped
	// and the same base can be reopened (a restart, or the next test).
	lc.Append(fx.Hook{OnStop: func(context.Context) error { return f.Close() }})
	logger.Info(context.Background(),
		"knowledge graph: per-account embedded stores enabled", "path", base)
	return f
}

func (f *perAccountStores) Active() bool { return true }

func (f *perAccountStores) PerAccount() bool { return true }

// ForAccount returns the embedded store for accountID, opening it on first use.
// An empty accountID is ErrNoAccount: callers resolve the account (and choose
// fail-closed vs. the local graph) before asking, so an empty id here is a bug.
func (f *perAccountStores) ForAccount(
	_ context.Context, accountID string,
) (repositories.KnowledgeGraphStore, error) {
	if accountID == "" {
		return nil, repositories.ErrNoAccount
	}
	dir, err := f.accountDir(accountID)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if st, ok := f.open[accountID]; ok {
		return st, nil
	}
	st, err := oxigraph.NewEmbeddedStore(dir, f.logger)
	if err != nil {
		return nil, fmt.Errorf("knowledge graph: open account store %q: %w", accountID, err)
	}
	f.open[accountID] = st
	return st, nil
}

// Truncate closes every open account store and removes each account's graph
// directory, so a checkpoint-reset replay reopens each store empty and
// re-projects its account's events. The base directory itself is left in place.
func (f *perAccountStores) Truncate(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Best-effort close so directory locks release before removal; a close error
	// shouldn't stop the truncate (removing the dir drops the lock anyway).
	_ = f.closeAllLocked()
	entries, err := os.ReadDir(f.base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("knowledge graph: read per-account base for truncate: %w", err)
	}
	for _, e := range entries {
		// Only reap directories that could be account stores (a safe account id).
		// Never touch other files/dirs an operator may keep under the base, so a
		// misconfigured base can't delete unrelated data on a rebuild.
		if !e.IsDir() || !isSafeAccountID(e.Name()) {
			continue
		}
		if rmErr := os.RemoveAll(filepath.Join(f.base, e.Name())); rmErr != nil {
			return fmt.Errorf("knowledge graph: remove account graph %q: %w", e.Name(), rmErr)
		}
	}
	return nil
}

// Close flushes and unlocks every open account store. Idempotent — the embedded
// store's own Close is a no-op after the first call.
func (f *perAccountStores) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeAllLocked()
}

// closeAllLocked closes every open store and empties the map. Caller holds f.mu.
// It closes all stores even if one errors, returning the first error seen.
func (f *perAccountStores) closeAllLocked() error {
	var firstErr error
	for id, st := range f.open {
		closer, ok := st.(io.Closer)
		if !ok {
			// Every account store is an embedded *EmbeddedStore (an io.Closer);
			// a non-closer here would strand its directory lock and break the
			// next restart, so surface it loudly rather than dropping silently.
			f.logger.Error(context.Background(),
				"knowledge graph: account store is not closable, its lock may be stranded", "accountID", id)
			delete(f.open, id)
			continue
		}
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("knowledge graph: close account store %q: %w", id, err)
		}
		delete(f.open, id)
	}
	return firstErr
}

// accountDir maps an account id to its store directory, rejecting anything that
// isn't a plain id token. Account ids are KSUIDs (base62) and the reserved
// "local" sentinel, so restricting to [A-Za-z0-9_-] both matches every real id
// and closes the path-traversal hole a crafted id ("../other") would otherwise
// open when joined onto the base.
func (f *perAccountStores) accountDir(accountID string) (string, error) {
	if !isSafeAccountID(accountID) {
		return "", fmt.Errorf("knowledge graph: unsafe account id %q", accountID)
	}
	return filepath.Join(f.base, accountID), nil
}

func isSafeAccountID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
