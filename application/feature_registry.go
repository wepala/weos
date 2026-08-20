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
	"context"
	"fmt"
	"sort"
	"sync"

	"go.uber.org/fx"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
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

// NewFeatureRegistry builds the registry from the code-declared group and
// from configuration. An invalid declaration fails the boot rather than being
// skipped: a malformed key is a programming error, and resolving against a
// registry that silently dropped it would gate nothing while looking like it
// gated something.
//
// Configuration is a third source alongside code and presets, and it exists
// because declarations are deliberately never persisted. Two processes — a
// running server and a `weos feature` invocation — can only agree about which
// features exist by both reading the same declarations, and the environment is
// the only channel they share that does not store anything.
func NewFeatureRegistry(
	cfg config.Config,
	presets *PresetRegistry,
	logger entities.Logger,
	declared []entities.FeatureMeta,
) (*FeatureRegistry, error) {
	if cfg.Features.DeclarationError != nil {
		return nil, cfg.Features.DeclarationError
	}
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
	// Configuration is registered after code, and a clash with either code or
	// a preset is an error rather than an override: two declarations of one
	// key means two things believe they own it, and picking one silently gates
	// the wrong thing. Register alone only sees code declarations, so the
	// preset half is checked here — without it, configuration could quietly
	// rewrite a preset feature's default and nothing would say so.
	for _, m := range cfg.Features.Declared {
		if presets != nil {
			if existing, clash := presets.Features()[m.Key]; clash {
				return nil, fmt.Errorf(
					"FEATURES: feature %q is already declared by a preset (as %q); keys must be unique",
					m.Key, existing.DisplayName)
			}
		}
		if agrees, err := r.reconcileWithCode(m); err != nil {
			return nil, err
		} else if agrees {
			continue
		}
		if err := r.Register(m); err != nil {
			return nil, fmt.Errorf("FEATURES: %w", err)
		}
	}
	return r, nil
}

// reconcileWithCode decides what a FEATURES entry means when code declares the
// same key. The first result reports that the entry is redundant and should be
// skipped.
//
// Core did not declare any feature until it shipped a call site that reads one,
// so an operator who wanted a switch early could only get it by naming the key
// in FEATURES. When core later declares that key itself, a plain duplicate-key
// error stops the whole application at upgrade — not one feature, the instance:
// serve, mcp and the CLI all refuse to start. That is a bad trade for a
// disagreement that does not exist.
//
// So an entry that agrees with the code declaration about what the feature DOES
// is a no-op with a warning naming the remedy. An entry that disagrees is still
// an error, because that is the case where picking one would gate the wrong
// thing — and the message names both sources and what to do about it.
//
// Only the behavior-deciding fields are compared. A description is prose an
// operator reads; code's wins and the difference is worth a line, not a
// failure to boot.
func (r *FeatureRegistry) reconcileWithCode(m entities.FeatureMeta) (bool, error) {
	r.mu.RLock()
	code, declared := r.code[m.Key]
	r.mu.RUnlock()
	if !declared {
		return false, nil
	}
	if code.Default != m.Default || code.Manageable != m.Manageable || code.Grantable != m.Grantable {
		return false, fmt.Errorf(
			"FEATURES declares feature %q as (default=%v manageable=%v grantable=%v), but this build "+
				"declares it in code as (default=%v manageable=%v grantable=%v); "+
				"remove %q from FEATURES to use the built-in declaration, or correct the entry to match it",
			m.Key, m.Default, m.Manageable, m.Grantable,
			code.Default, code.Manageable, code.Grantable, m.Key)
	}
	if r.logger != nil {
		r.logger.Warn(context.Background(),
			"FEATURES redeclares a feature this build already declares in code; the entry has no effect "+
				"and can be removed",
			"feature", m.Key)
	}
	return true, nil
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
		if existing, shadowed := merged[key]; shadowed && r.logger != nil && existing != m {
			// Code wins, deliberately — a binary that declares a key a preset
			// also uses is overriding it on purpose, and cannot edit the
			// preset to say so. But an override nobody announced changes a
			// feature's default, or whether it can be granted, with nothing to
			// read afterwards.
			r.logger.Warn(context.Background(),
				"a code declaration overrides a preset's feature of the same key",
				"feature", key, "preset_default", existing.Default, "code_default", m.Default)
		}
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
