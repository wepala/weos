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

	rec, err := reconcileAdditiveContext(stored, preset, nil)
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

	rec, err := reconcileAdditiveContext(stored, preset, nil)
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

	rec, err := reconcileAdditiveContext(stored, preset, nil)
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

	rec, err := reconcileAdditiveContext(stored, preset, nil)
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

	rec, err := reconcileAdditiveContext(stored, preset, nil)
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
			rec, err := reconcileAdditiveContext(stored, preset, nil)
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

	rec, err := reconcileAdditiveContext(stored, nil, nil)
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

	first, err := reconcileAdditiveContext(stored, preset, nil)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if !first.Changed {
		t.Fatal("expected the first merge to change something")
	}
	second, err := reconcileAdditiveContext(first.Context, preset, nil)
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

	rec, err := reconcileAdditiveContext(stored, preset, nil)
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

	if _, err := reconcileAdditiveContext(json.RawMessage(`["https://schema.org/"]`), preset, nil); err == nil {
		t.Error("expected an error for a stored context that is not an object")
	} else if !strings.Contains(err.Error(), "stored context") {
		t.Errorf("error should name the stored side, got %v", err)
	}
	if _, err := reconcileAdditiveContext(preset, json.RawMessage(`"https://schema.org/"`), nil); err == nil {
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

// TestReconcileAdditiveContext_PrefixFormTerms covers the shape real presets
// actually use: a term whose IRI is written against a prefix defined elsewhere
// in the same context (`"recipeIngredient":"fo:hasIngredient"`). Both the
// prefix and the term are ordinary keys here, so both merge — and the
// detection must expand the prefix before deciding the reference is covered.
func TestReconcileAdditiveContext_PrefixFormTerms(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/",` +
		`"fo":"http://purl.org/foodontology#","ingredient":"fo:hasIngredient"}`)

	rec, err := reconcileAdditiveContext(stored, preset, nil)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if !sameStrings(rec.Added, []string{"fo", "ingredient"}) {
		t.Fatalf("Added = %v, want [fo ingredient] — the prefix must merge alongside the term", rec.Added)
	}

	schema := json.RawMessage(`{"type":"object","properties":{
		"ingredient":{"type":"string","x-resource-type":"ingredient"}}}`)
	if dropped := referencePropertiesWithoutContextEntry(schema, rec.Context); len(dropped) != 0 {
		t.Errorf("a prefix-form term must count as covered, got %v", dropped)
	}
}

// TestReconcileAdditiveContext_HeldPrefixRebindsItsTerms pins a consequence of
// holding: when an operator repointed a PREFIX, a term the preset adds against
// that prefix resolves into the operator's namespace, not the preset's. The
// reference still round-trips — write and read expand through the same stored
// context — so this is documented behavior rather than a defect, but it is the
// one case where an added term does not mean what the preset author wrote.
func TestReconcileAdditiveContext_HeldPrefixRebindsItsTerms(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","fo":"https://example.org/food#"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/",` +
		`"fo":"http://purl.org/foodontology#","ingredient":"fo:hasIngredient"}`)

	rec, err := reconcileAdditiveContext(stored, preset, nil)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if !sameStrings(rec.Conflicts, []string{"fo"}) {
		t.Errorf("Conflicts = %v, want [fo]", rec.Conflicts)
	}
	terms := contextTerms(t, rec.Context)
	if terms["fo"] != "https://example.org/food#" {
		t.Errorf("the operator's prefix was overwritten: %v", terms)
	}
	// The term still resolves, against the operator's prefix — so writes land.
	schema := json.RawMessage(`{"type":"object","properties":{
		"ingredient":{"type":"string","x-resource-type":"ingredient"}}}`)
	if dropped := referencePropertiesWithoutContextEntry(schema, rec.Context); len(dropped) != 0 {
		t.Errorf("a term against a held prefix must still resolve, got %v", dropped)
	}
}

// TestReconcileAdditiveContext_HoldsATermThatMovesALivePredicate is issue
// #513's core guard: the stored schema already declares the reference, so data
// has been written under the `@vocab`-derived IRI. A term naming anything else
// is held rather than merged, because merging would leave those edges keyed by
// an IRI nothing reverse-maps and reprojection could not recover them.
func TestReconcileAdditiveContext_HoldsATermThatMovesALivePredicate(t *testing.T) {
	storedSchema := json.RawMessage(`{"type":"object","properties":{
		"supplier":{"type":"string","x-resource-type":"vendor"}}}`)
	stored := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/",` +
		`"supplier":"https://example.org/catalog#supplier"}`)

	rec, err := reconcileAdditiveContext(stored, preset, storedSchema)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if len(rec.Moves) != 1 || rec.Moves[0].Term != "supplier" {
		t.Fatalf("Moves = %+v, want a single hold on supplier", rec.Moves)
	}
	if got := rec.Moves[0].StoredIRI; got != "https://schema.org/supplier" {
		t.Errorf("StoredIRI = %q, want the vocab-derived IRI the data uses", got)
	}
	if rec.Changed {
		t.Error("a held term must not rewrite the stored context")
	}
}

// TestReconcileAdditiveContext_AdoptsAVocabConsistentTerm is the other half:
// when the term names exactly what the property already resolves to, nothing
// moves, so the term merges and the reference becomes readable. This is the
// case issue #510's repair depends on, and every in-tree preset that ships a
// bare schema.org IRI under an @vocab of schema.org takes it.
func TestReconcileAdditiveContext_AdoptsAVocabConsistentTerm(t *testing.T) {
	storedSchema := json.RawMessage(`{"type":"object","properties":{
		"supplier":{"type":"string","x-resource-type":"vendor"}}}`)
	stored := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","supplier":"https://schema.org/supplier"}`)

	rec, err := reconcileAdditiveContext(stored, preset, storedSchema)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if len(rec.Moves) != 0 {
		t.Fatalf("Moves = %+v, want none — the IRI is what the data already uses", rec.Moves)
	}
	if !sameStrings(rec.Added, []string{"supplier"}) {
		t.Errorf("Added = %v, want [supplier]", rec.Added)
	}
}

// TestReconcileAdditiveContext_GuardKeysOffTheStoredSchema pins the boundary
// that keeps issue #510's repair working. A preset adding a reference property
// AND its term in the same build has no data under any IRI for that property,
// so there is nothing to orphan and the term must merge — even though its IRI
// differs from the vocab-derived one. Keying the guard off the MERGED schema
// would hold it and re-break #510.
func TestReconcileAdditiveContext_GuardKeysOffTheStoredSchema(t *testing.T) {
	// The stored schema does NOT declare `supplier` yet.
	storedSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	stored := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/",` +
		`"supplier":"https://example.org/catalog#supplier"}`)

	rec, err := reconcileAdditiveContext(stored, preset, storedSchema)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if len(rec.Moves) != 0 {
		t.Fatalf("Moves = %+v, want none — the property has no stored data to orphan", rec.Moves)
	}
	if !sameStrings(rec.Added, []string{"supplier"}) {
		t.Errorf("Added = %v, want [supplier]", rec.Added)
	}
}

// TestReconcileAdditiveContext_HoldsAPrefixThatRepointsAStoredTerm covers the
// indirect move: the prefix itself is new, but a term already stored expands
// through it, so adding it repoints that term's predicate.
func TestReconcileAdditiveContext_HoldsAPrefixThatRepointsAStoredTerm(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","maker":"cat:madeBy"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","cat":"https://example.org/catalog#"}`)

	rec, err := reconcileAdditiveContext(stored, preset, nil)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if len(rec.Moves) != 1 || rec.Moves[0].Term != "cat" {
		t.Fatalf("Moves = %+v, want a single hold on the cat prefix", rec.Moves)
	}
	if rec.Moves[0].Property != "maker" {
		t.Errorf("Property = %q, want the stored term whose resolution moves", rec.Moves[0].Property)
	}
}

// TestReconcileAdditiveContext_HoldsATypeThatMovesTheClass: `@type` decides the
// type's RDF class, so adopting a new one splits resources written before the
// boot from those written after across two classes.
func TestReconcileAdditiveContext_HoldsATypeThatMovesTheClass(t *testing.T) {
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Widget"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","@type":"Product"}`)

	rec, err := reconcileAdditiveContext(stored, preset, nil)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	// @type is present on both sides here, so it is an ordinary conflict; the
	// move guard covers the case where the stored context declares none.
	if len(rec.Conflicts) != 1 || rec.Conflicts[0] != "@type" {
		t.Fatalf("Conflicts = %v, want [@type]", rec.Conflicts)
	}
	if rec.Changed {
		t.Error("holding @type must not rewrite the stored context")
	}
}

// TestReconcileAdditiveContext_ClearedContextIsHeldNotReadopted: an operator
// who empties a type's context leaves its references resolving to their bare
// property names — `maker`, not `https://schema.org/maker` — because there is
// no `@vocab` to prefix them. Edges exist under those names, so adopting the
// preset's terms orphans them like any other repointing. The merge is held and
// reported; `preset install --update` remains the explicit way to re-adopt.
func TestReconcileAdditiveContext_ClearedContextIsHeldNotReadopted(t *testing.T) {
	storedSchema := json.RawMessage(`{"type":"object","properties":{
		"maker":{"type":"string","x-resource-type":"vendor"}}}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/","maker":"https://schema.org/maker"}`)

	for name, stored := range map[string]json.RawMessage{
		"empty object": json.RawMessage(`{}`),
		"json null":    json.RawMessage(`null`),
	} {
		t.Run(name, func(t *testing.T) {
			rec, err := reconcileAdditiveContext(stored, preset, storedSchema)
			if err != nil {
				t.Fatalf("reconcileAdditiveContext: %v", err)
			}
			if len(rec.Moves) == 0 {
				t.Fatalf("expected the merge to be held and reported, got Added=%v", rec.Added)
			}
			if rec.Changed {
				t.Error("a cleared context must not be silently re-adopted")
			}
		})
	}
}

// TestReconcileAdditiveContext_PrefixAndTermAddedTogether is the false positive
// the first version of this guard had. A preset that adds a PREFIX and a term
// using it in the same build resolves that term only once both are present.
// Judging the term against the stored context alone reads its IRI with the
// prefix unexpanded — `http://ex.org/ex:maker` instead of `http://ex.org/maker`
// — and holds a term that moves nothing.
func TestReconcileAdditiveContext_PrefixAndTermAddedTogether(t *testing.T) {
	storedSchema := json.RawMessage(`{"type":"object","properties":{
		"maker":{"type":"string","x-resource-type":"vendor"}}}`)
	stored := json.RawMessage(`{"@vocab":"http://ex.org/","@type":"Thing"}`)
	// `ex` expands to the same namespace as @vocab, so `ex:maker` and the
	// vocab-derived `maker` are the same IRI: nothing moves.
	preset := json.RawMessage(`{"@vocab":"http://ex.org/","@type":"Thing",` +
		`"ex":"http://ex.org/","maker":"ex:maker"}`)

	rec, err := reconcileAdditiveContext(stored, preset, storedSchema)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if len(rec.Moves) != 0 {
		t.Fatalf("Moves = %+v, want none — maker resolves to the same IRI either way", rec.Moves)
	}
	if !sameStrings(rec.Added, []string{"ex", "maker"}) {
		t.Errorf("Added = %v, want [ex maker]", rec.Added)
	}
}

// TestReconcileAdditiveContext_PrefixedTermThatDoesMove is the other side of
// that case: once the prefix resolves, the term still names a different
// namespace from the one the data uses, so it is held — and the reported IRI is
// the resolved one, not a half-expanded string.
func TestReconcileAdditiveContext_PrefixedTermThatDoesMove(t *testing.T) {
	storedSchema := json.RawMessage(`{"type":"object","properties":{
		"recipeIngredient":{"type":"string","x-resource-type":"ingredient"}}}`)
	stored := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/",` +
		`"fo":"http://purl.org/foodontology#","recipeIngredient":"fo:hasIngredient"}`)

	rec, err := reconcileAdditiveContext(stored, preset, storedSchema)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if len(rec.Moves) != 1 {
		t.Fatalf("Moves = %+v, want the recipeIngredient term held", rec.Moves)
	}
	got := rec.Moves[0]
	if got.StoredIRI != "https://schema.org/recipeIngredient" {
		t.Errorf("StoredIRI = %q, want the vocab-derived IRI the data uses", got.StoredIRI)
	}
	if got.PresetIRI != "http://purl.org/foodontology#hasIngredient" {
		t.Errorf("PresetIRI = %q, want the fully expanded IRI — a half-expanded "+
			"`fo:hasIngredient` would tell an operator nothing actionable", got.PresetIRI)
	}
}

// TestReconcileAdditiveContext_AddingVocabMovesEveryUntermedReference: a stored
// context with no `@vocab` resolves an untermed reference to its bare name, so
// adding one moves every such reference at once. Nothing names `@vocab` in
// those definitions, so without an explicit fallback the move is detected, no
// term is blamed, nothing is held, and it is allowed through.
func TestReconcileAdditiveContext_AddingVocabMovesEveryUntermedReference(t *testing.T) {
	storedSchema := json.RawMessage(`{"type":"object","properties":{
		"maker":{"type":"string","x-resource-type":"vendor"}}}`)
	stored := json.RawMessage(`{"@type":"Widget"}`)
	preset := json.RawMessage(`{"@type":"Widget","@vocab":"https://schema.org/"}`)

	rec, err := reconcileAdditiveContext(stored, preset, storedSchema)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if len(rec.Moves) != 1 || rec.Moves[0].Term != "@vocab" {
		t.Fatalf("Moves = %+v, want @vocab held", rec.Moves)
	}
	if rec.Changed {
		t.Error("holding @vocab must leave the stored context alone")
	}
}

// TestReconcileAdditiveContext_DropsATermWhoseHeldPrefixIsGone: holding a
// prefix must also drop the terms written against it. Left merged, such a term
// resolves through `@vocab` to a fabricated IRI that reads and writes then
// agree on — so nothing is dropped, the type is reported Updated, and the
// triple store carries a predicate nobody meant.
func TestReconcileAdditiveContext_DropsATermWhoseHeldPrefixIsGone(t *testing.T) {
	// `hasIngredient` is stored against an UNDEFINED `fo`, so it currently
	// resolves through @vocab. Defining `fo` would move it, so `fo` is held —
	// and `recipeIngredient`, written against that same `fo`, must not survive.
	stored := json.RawMessage(`{"@vocab":"https://schema.org/","hasIngredient":"fo:hasIngredient"}`)
	preset := json.RawMessage(`{"@vocab":"https://schema.org/",` +
		`"fo":"http://purl.org/foodontology#","recipeIngredient":"fo:hasIngredient2"}`)

	rec, err := reconcileAdditiveContext(stored, preset, nil)
	if err != nil {
		t.Fatalf("reconcileAdditiveContext: %v", err)
	}
	if rec.Context != nil {
		if terms := contextTerms(t, rec.Context); terms["recipeIngredient"] != "" {
			t.Errorf("recipeIngredient survived with no `fo` to expand through — it resolves to "+
				"https://schema.org/fo:hasIngredient2, an IRI nobody declared (context: %v)", terms)
		}
	}
	for _, m := range rec.Moves {
		if m.Term == "recipeIngredient" {
			return
		}
	}
	t.Errorf("recipeIngredient was not reported; Moves = %+v", rec.Moves)
}
