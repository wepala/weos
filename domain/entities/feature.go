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

package entities

import (
	"fmt"
	"regexp"
)

// featureKeyRe constrains a flag key to what an operator can type and what
// OpenFeature call sites can embed in a constant: lowercase, hyphenated,
// starting with a letter. Deliberately narrower than a resource-type slug —
// flags are their own namespace and must never be confused with one.
var featureKeyRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// FeatureMeta declares a feature flag. It mirrors BehaviorMeta field-for-field
// (see resource_behavior.go) plus Grantable, because the two are the same kind
// of declaration seen from different ends: BehaviorMeta describes a behavior an
// account may switch, FeatureMeta describes a capability an instance, an
// account, or one person may hold.
//
// The declaration is the whole of what the instance knows about a feature
// before anyone has stored anything. Nothing here is persisted: features are
// declared in code or by a preset, so the registry is rebuilt from the binary
// on every boot and can never drift from what the code can actually gate.
type FeatureMeta struct {
	// Key is what call sites pass to the OpenFeature client, after the
	// "feature." prefix.
	Key string
	// DisplayName and Description are what an operator reads in the CLI
	// (#482) and the admin UI (#486). Neither affects resolution.
	DisplayName string
	Description string
	// Default is the value when no layer has spoken. It is NOT the same as
	// an explicit instance-level off — see ResolveFeature.
	Default bool
	// Manageable allows an account admin to override the feature for their
	// account. When false, a stored account override is ignored rather than
	// rejected: the row may predate a declaration change, and resolution must
	// never depend on a writer having been well-behaved.
	Manageable bool
	// Grantable allows the feature to be granted to one agent or to a role.
	// When false, a stored grant is ignored, for the same reason.
	Grantable bool
}

// Validate reports whether the declaration is usable. A registry rejects an
// invalid declaration at boot rather than resolving against it, because a
// malformed key is a programming error the operator cannot fix at runtime.
func (m FeatureMeta) Validate() error {
	if !featureKeyRe.MatchString(m.Key) {
		return fmt.Errorf("feature key %q must match %s", m.Key, featureKeyRe.String())
	}
	if m.DisplayName == "" {
		return fmt.Errorf("feature %q must declare a display name", m.Key)
	}
	return nil
}

// FeatureState is what one layer says about a feature. The zero value is
// FeatureUnset, so a layer that has stored nothing says nothing — which is what
// makes row-absence the storage representation of "unset" and removes any need
// for a nullable column or a sentinel value.
type FeatureState int

const (
	// FeatureUnset means this layer has no opinion; resolution passes through
	// it untouched.
	FeatureUnset FeatureState = iota
	// FeatureOn means this layer explicitly turned the feature on.
	FeatureOn
	// FeatureOff means this layer explicitly turned the feature off.
	FeatureOff
)

// ResolveFeature answers whether a feature is on for one caller, given what
// each layer says. It is the single definition of precedence in the codebase:
// the resolver, the operator CLI's "explain" output (#482), and the admin
// listing (#486) all call this rather than re-deriving the rules, so the three
// can never disagree.
//
// The layers speak in order — instance, then account, then grant — and the
// rules are:
//
//   - A layer that says nothing passes through untouched. So a feature nobody
//     has touched answers meta.Default.
//   - An explicit OFF is final for every layer below it. An operator who turns
//     a feature off for the instance has turned it off for everyone, and no
//     account override and no grant reaches past that.
//   - An explicit ON is NOT final. An account may still turn off a feature the
//     instance turned on. This asymmetry is deliberate and is the rule most
//     likely to be implemented backwards, because the acceptance contract does
//     not exercise instance-on plus account-off — the truth table beside this
//     function does.
//   - meta.Default is NOT an explicit off. Reading "first off wins" strictly
//     would make a feature declared Default:false unreachable forever, leaving
//     the Default field with one usable value. A declared-off feature can still
//     be turned on by an account or by a grant.
//   - A grant can only turn a feature ON, never off. Grants are the bottom
//     layer, so a grant cannot rescue a feature an upper layer switched off.
//
// Eligibility is re-checked here rather than trusted from the store: an
// account override on a non-manageable feature, and a grant on a non-grantable
// feature, are both ignored. Stored rows can outlive a declaration change, and
// resolution must not depend on whoever wrote the row.
func ResolveFeature(meta FeatureMeta, instance, account FeatureState, granted bool) bool {
	value := meta.Default

	if instance != FeatureUnset {
		if instance == FeatureOff {
			return false
		}
		value = true
	}

	if meta.Manageable && account != FeatureUnset {
		if account == FeatureOff {
			return false
		}
		value = true
	}

	if meta.Grantable && granted {
		value = true
	}

	return value
}
