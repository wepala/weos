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
	"encoding/json"
	"strings"
	"testing"
)

// contextTerms decodes a merged context so assertions can talk about terms
// without depending on key order in the marshaled output.
func contextTerms(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	var terms map[string]any
	if err := json.Unmarshal(raw, &terms); err != nil {
		t.Fatalf("context is not a JSON object: %v", err)
	}
	out := map[string]string{}
	for k, v := range terms {
		switch val := v.(type) {
		case string:
			out[k] = val
		default:
			encoded, err := json.Marshal(val)
			if err != nil {
				t.Fatalf("term %q is not encodable: %v", k, err)
			}
			out[k] = string(encoded)
		}
	}
	return out
}

// TestReconcileAdditiveContext_AddsMissingTerm is the defect in one case: the
// preset gained a reference property's term and the stored context lacks it.
func TestReconcileAdditiveContext_AddsMissingTerm(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":"https://schema.org/manufacturer"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/",` +
		`"maker":"https://schema.org/manufacturer","supplier":"https://schema.org/seller"}`)

	rec, err := reconcileAdditiveContext(stored, preset)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if !rec.Changed {
		t.Fatal("expected the merge to report a change")
	}
	if !sameStrings(rec.Added, []string{"supplier"}) {
		t.Errorf("Added = %v, want [supplier]", rec.Added)
	}
	if len(rec.Conflicts) != 0 {
		t.Errorf("Conflicts = %v, want none", rec.Conflicts)
	}
	terms := contextTerms(t, rec.Context)
	if terms["supplier"] != "https://schema.org/seller" {
		t.Errorf("merged context lost the added term: %v", terms)
	}
	if terms["@vocab"] != "https://schema.org/" {
		t.Errorf("merged context lost @vocab: %v", terms)
	}
}

// TestReconcileAdditiveContext_PreservesOperatorTerm pins the mirror of the
// schema merge's property-preservation rule: a term only the operator declared
// survives, because existing edges are already keyed by it.
func TestReconcileAdditiveContext_PreservesOperatorTerm(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","warranty":"https://example.org/vocab/warranty"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","supplier":"https://schema.org/seller"}`)

	rec, err := reconcileAdditiveContext(stored, preset)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	terms := contextTerms(t, rec.Context)
	if terms["warranty"] != "https://example.org/vocab/warranty" {
		t.Errorf("merge dropped the operator's own term: %v", terms)
	}
	if terms["supplier"] != "https://schema.org/seller" {
		t.Errorf("merge failed to add the preset's term: %v", terms)
	}
}

// TestReconcileAdditiveContext_HoldsDivergingTerm is the operator-customisation
// guard: a term the operator repointed stays repointed, and is reported.
// Overwriting it would orphan every edge already written under the stored IRI.
func TestReconcileAdditiveContext_HoldsDivergingTerm(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":"https://example.org/vocab/madeBy"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/",` +
		`"maker":"https://schema.org/manufacturer","supplier":"https://schema.org/seller"}`)

	rec, err := reconcileAdditiveContext(stored, preset)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if !sameStrings(rec.Conflicts, []string{"maker"}) {
		t.Errorf("Conflicts = %v, want [maker]", rec.Conflicts)
	}
	terms := contextTerms(t, rec.Context)
	if terms["maker"] != "https://example.org/vocab/madeBy" {
		t.Errorf("held term was overwritten: %v", terms)
	}
	// Refusal is per-term: the additive term beside it still landed.
	if terms["supplier"] != "https://schema.org/seller" {
		t.Errorf("a held term blocked an additive one: %v", terms)
	}
}

// TestReconcileAdditiveContext_ConflictAloneIsNotAChange: with nothing to add,
// a held term must not rewrite the stored context, or every restart would emit
// a ResourceTypeUpdated for a type it changed nothing about.
func TestReconcileAdditiveContext_ConflictAloneIsNotAChange(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":"https://example.org/vocab/madeBy"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","maker":"https://schema.org/manufacturer"}`)

	rec, err := reconcileAdditiveContext(stored, preset)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if rec.Changed {
		t.Error("a held term alone must not report a change")
	}
	if !sameStrings(rec.Conflicts, []string{"maker"}) {
		t.Errorf("Conflicts = %v, want [maker]", rec.Conflicts)
	}
}

// TestReconcileAdditiveContext_HoldsDivergingKeyword: keywords are merged by
// the same rules as terms. Repointing @vocab would move the predicate of every
// property that resolves through it, so an operator-edited one is held.
func TestReconcileAdditiveContext_HoldsDivergingKeyword(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://example.org/vocab/"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","supplier":"https://schema.org/seller"}`)

	rec, err := reconcileAdditiveContext(stored, preset)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if !sameStrings(rec.Conflicts, []string{"@vocab"}) {
		t.Errorf("Conflicts = %v, want [@vocab]", rec.Conflicts)
	}
	terms := contextTerms(t, rec.Context)
	if terms["@vocab"] != "https://example.org/vocab/" {
		t.Errorf("@vocab was overwritten: %v", terms)
	}
}

// TestReconcileAdditiveContext_EmptyStoredAdoptsPreset: nothing to preserve,
// nothing that can conflict.
func TestReconcileAdditiveContext_EmptyStoredAdoptsPreset(t *testing.T) {
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","supplier":"https://schema.org/seller"}`)

	for name, stored := range map[string]json.RawMessage{
		"absent": nil,
		"empty":  json.RawMessage(``),
	} {
		t.Run(name, func(t *testing.T) {
			rec, err := reconcileAdditiveContext(stored, preset)
			if err != nil {
				t.Fatalf("reconcileAdditiveContext: %v", err)
			}
			if !rec.Changed {
				t.Fatal("expected the merge to report a change")
			}
			terms := contextTerms(t, rec.Context)
			if terms["supplier"] != "https://schema.org/seller" {
				t.Errorf("adopted context is missing the preset's term: %v", terms)
			}
		})
	}
}

// TestReconcileAdditiveContext_EmptyPresetIsNeverAnInstructionToClear.
func TestReconcileAdditiveContext_EmptyPresetIsNeverAnInstructionToClear(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":"https://schema.org/manufacturer"}`)

	rec, err := reconcileAdditiveContext(stored, nil)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if rec.Changed || rec.Context != nil {
		t.Errorf("an empty preset context must be a no-op, got Changed=%v Context=%s", rec.Changed, rec.Context)
	}
}

// TestReconcileAdditiveContext_Idempotent: re-running against its own output
// reports no change, which is what keeps repeated restarts quiet.
func TestReconcileAdditiveContext_Idempotent(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","supplier":"https://schema.org/seller"}`)

	first, err := reconcileAdditiveContext(stored, preset)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if !first.Changed {
		t.Fatal("expected the first merge to change something")
	}
	second, err := reconcileAdditiveContext(first.Context, preset)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if second.Changed {
		t.Errorf("merge is not idempotent: second pass reported Changed with Added=%v", second.Added)
	}
}

// TestReconcileAdditiveContext_ObjectTermsCompareByValue: a term defined as an
// object round-trips, and an equivalent one written differently is not a
// conflict — otherwise every boot would hold a term over key order.
func TestReconcileAdditiveContext_ObjectTermsCompareByValue(t *testing.T) {
	stored := json.RawMessage(`{"supplier":{"@type":"@id","@id":"https://schema.org/seller"}}`)
	preset := json.RawMessage(`{"supplier":{"@id":"https://schema.org/seller","@type":"@id"}}`)

	rec, err := reconcileAdditiveContext(stored, preset)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if rec.Changed || len(rec.Conflicts) != 0 {
		t.Errorf("equivalent object terms must compare equal, got Changed=%v Conflicts=%v",
			rec.Changed, rec.Conflicts)
	}
}

// TestReconcileAdditiveContext_NonObjectIsAnError: JSON-LD permits an array or
// a remote IRI string as a context. Nothing in this tree uses either, so
// reporting the type rather than guessing at a merge is the honest outcome.
func TestReconcileAdditiveContext_NonObjectIsAnError(t *testing.T) {
	preset := json.RawMessage(`{"@vocab":"https://schema.org/"}`)

	if _, err := reconcileAdditiveContext(json.RawMessage(`["https://schema.org/"]`), preset); err == nil {
		t.Error("expected an error for a stored context that is not an object")
	} else if !strings.Contains(err.Error(), "stored context") {
		t.Errorf("error should name the stored side, got %v", err)
	}
	if _, err := reconcileAdditiveContext(preset, json.RawMessage(`"https://schema.org/"`)); err == nil {
		t.Error("expected an error for a preset context that is not an object")
	}
}

// TestReferencePropertiesWithoutContextEntry pins the detection the reconcile
// reports Failed on: a reference resolving through @vocab has no reverse-map
// entry, so its edge is dropped even though ResolvePredicateIRI yields an IRI.
func TestReferencePropertiesWithoutContextEntry(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{
		"name":{"type":"string"},
		"maker":{"type":"string","x-resource-type":"vendor"},
		"supplier":{"type":"string","x-resource-type":"vendor"}}}`)

	t.Run("names only the uncovered reference", func(t *testing.T) {
		ldContext := json.RawMessage(`{"@vocab":"https://schema.org/","maker":"https://schema.org/manufacturer"}`)
		got := referencePropertiesWithoutContextEntry(schema, ldContext)
		if !sameStrings(got, []string{"supplier"}) {
			t.Errorf("got %v, want [supplier]", got)
		}
	})

	t.Run("a literal never counts as dropped", func(t *testing.T) {
		ldContext := json.RawMessage(`{"@vocab":"https://schema.org/",` +
			`"maker":"https://schema.org/manufacturer","supplier":"https://schema.org/seller"}`)
		if got := referencePropertiesWithoutContextEntry(schema, ldContext); len(got) != 0 {
			t.Errorf("got %v, want none — `name` is a literal and both references are covered", got)
		}
	})

	// Two references mapped to the SAME IRI collide in the reverse map and only
	// one survives, so a per-term existence check would miss this.
	t.Run("colliding terms are reported", func(t *testing.T) {
		ldContext := json.RawMessage(`{"@vocab":"https://schema.org/",` +
			`"maker":"https://schema.org/seller","supplier":"https://schema.org/seller"}`)
		got := referencePropertiesWithoutContextEntry(schema, ldContext)
		if len(got) != 1 {
			t.Errorf("expected exactly one of the colliding references to be reported, got %v", got)
		}
	})
}
