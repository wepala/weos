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

package application

import (
	"fmt"
	"sort"
	"sync"

	"go.uber.org/fx"

	"github.com/wepala/weos/v3/domain/entities"
)

// FeatureDeclarations is an fx value group of feature declarations contributed
// by code rather than by a preset. A downstream binary — including one built
// from a private presets repository core must never depend on — adds its own
// flags by providing into this group:
//
//	fx.Provide(application.AsFeatureDeclarations(func() []entities.FeatureMeta {
//	    return []entities.FeatureMeta{{Key: "invoice-export", ...}}
//	}))
//
// This is the whole seam for out-of-tree declarations. Nothing in core imports
// anything to support it.
type FeatureDeclarations struct {
	fx.Out
	Declarations []entities.FeatureMeta `group:"feature_declarations,flatten"`
}

// AsFeatureDeclarations annotates a constructor into the feature_declarations
// value group, mirroring AsSubscriberGroups in worker_providers.go.
func AsFeatureDeclarations(f any) any {
	return fx.Annotate(f, fx.ResultTags(`group:"feature_declarations,flatten"`))
}

// FeatureRegistry answers what features exist. There are two sources and
// neither is persisted: declarations contributed in code (the fx value group
// above, plus anything Register adds during wiring) and declarations owned by
// an installed preset.
//
// Preset declarations are swept from the PresetRegistry on every lookup rather
// than copied in once. That costs a map build per call — trivially cheap
// against the database read it precedes, and only on a cache miss — and buys
// two things: installing a preset makes its features known to the very next
// resolution without a hook in InstallPreset, and there is no second copy of
// the declarations that could drift from the preset that owns them.
//
// A code declaration wins a key collision with a preset. Code is the more
// specific statement: a downstream binary that declares a key a preset also
// uses is overriding it deliberately, and cannot edit the preset to say so.
type FeatureRegistry struct {
	mu      sync.RWMutex
	code    map[string]entities.FeatureMeta
	presets *PresetRegistry
	logger  entities.Logger
}

// NewFeatureRegistry builds the registry from the code-declared group. An
// invalid declaration fails the boot rather than being skipped: a malformed
// key is a programming error, and resolving against a registry that silently
// dropped it would gate nothing while looking like it gated something.
func NewFeatureRegistry(
	presets *PresetRegistry,
	logger entities.Logger,
	declared []entities.FeatureMeta,
) (*FeatureRegistry, error) {
	r := &FeatureRegistry{
		code:    make(map[string]entities.FeatureMeta, len(declared)),
		presets: presets,
		logger:  logger,
	}
	for _, m := range declared {
		if err := r.Register(m); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds a code-declared feature. Duplicate keys are an error rather
// than last-wins: two code declarations of one key means two subsystems each
// believe they own it, and picking one silently would gate the wrong thing.
func (r *FeatureRegistry) Register(m entities.FeatureMeta) error {
	if err := m.Validate(); err != nil {
		return fmt.Errorf("invalid feature declaration: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.code[m.Key]; ok {
		return fmt.Errorf(
			"feature %q is already declared in code (as %q); keys must be unique",
			m.Key, existing.DisplayName)
	}
	r.code[m.Key] = m
	return nil
}

// Lookup returns the declaration for a key. The second result is false when
// nothing declares the key — which the resolver treats as registry drift: the
// caller gets its own default and the instance logs it once.
func (r *FeatureRegistry) Lookup(key string) (entities.FeatureMeta, bool) {
	r.mu.RLock()
	code, ok := r.code[key]
	r.mu.RUnlock()
	if ok {
		return code, true
	}
	if r.presets == nil {
		return entities.FeatureMeta{}, false
	}
	m, ok := r.presets.Features()[key]
	return m, ok
}

// All returns every declared feature, sorted by key, with code declarations
// overriding preset ones. This is what the operator CLI (#482) and the admin
// listing (#486) enumerate, and what the resolver folds over to build a
// caller's whole set in one pass.
func (r *FeatureRegistry) All() []entities.FeatureMeta {
	merged := map[string]entities.FeatureMeta{}
	if r.presets != nil {
		for key, m := range r.presets.Features() {
			merged[key] = m
		}
	}
	r.mu.RLock()
	for key, m := range r.code {
		merged[key] = m
	}
	r.mu.RUnlock()

	out := make([]entities.FeatureMeta, 0, len(merged))
	for _, m := range merged {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
