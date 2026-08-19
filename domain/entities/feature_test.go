package entities

import (
	"strings"
	"testing"
)

// grantable is the declaration used by most cases below: an account may change
// it and it may be granted, so every layer is live and the table exercises
// precedence rather than eligibility.
func grantable(def bool) FeatureMeta {
	return FeatureMeta{
		Key: "ledger-export", DisplayName: "Ledger export",
		Default: def, Manageable: true, Grantable: true,
	}
}

// TestResolveFeature is the exhaustive truth table over every layer
// combination. It exists because the acceptance contract cannot reach one of
// these rows: no scenario stages an instance-level ON together with an account
// OFF, so the fold could be written with ON treated as final and all 33
// scenarios would still pass. The rows marked "contract gap" below are the
// ones only this test proves.
func TestResolveFeature(t *testing.T) {
	cases := []struct {
		name     string
		meta     FeatureMeta
		instance FeatureState
		account  FeatureState
		granted  bool
		want     bool
	}{
		// Nobody has spoken: the declared default stands, and it is not an
		// explicit off — a declared-off feature must remain reachable.
		{"default on, nothing stored", grantable(true), FeatureUnset, FeatureUnset, false, true},
		{"default off, nothing stored", grantable(false), FeatureUnset, FeatureUnset, false, false},

		// A declared-off feature can still be turned on from below. This is
		// the whole reason Default and an explicit instance off are different
		// states; read "first off wins" strictly and every row here is false.
		{"default off, account on", grantable(false), FeatureUnset, FeatureOn, false, true},
		{"default off, granted", grantable(false), FeatureUnset, FeatureUnset, true, true},
		{"default off, instance on", grantable(false), FeatureOn, FeatureUnset, false, true},

		// An explicit off is final for every layer below it.
		{"instance off beats account on", grantable(true), FeatureOff, FeatureOn, false, false},
		{"instance off beats grant", grantable(true), FeatureOff, FeatureUnset, true, false},
		{"instance off beats account on and grant", grantable(true), FeatureOff, FeatureOn, true, false},
		{"account off beats grant", grantable(true), FeatureUnset, FeatureOff, true, false},

		// contract gap: an explicit ON is NOT final. An account may still turn
		// off what the instance turned on. No .feature scenario covers these.
		{"instance on does not beat account off", grantable(true), FeatureOn, FeatureOff, false, false},
		{"instance on, account off, granted", grantable(false), FeatureOn, FeatureOff, true, false},

		// A grant only ever turns a feature on; it can never turn one off.
		{"grant cannot lower account on", grantable(true), FeatureUnset, FeatureOn, false, true},
		{"grant raises a default-off feature", grantable(false), FeatureUnset, FeatureUnset, true, true},

		// Eligibility is re-checked at resolution, because a stored row can
		// outlive a declaration change and resolution must not depend on the
		// writer having been well behaved.
		{
			"account override ignored when not manageable",
			FeatureMeta{Key: "audit-trail", DisplayName: "Audit trail", Default: true},
			FeatureUnset, FeatureOff, false, true,
		},
		{
			"grant ignored when not grantable",
			FeatureMeta{Key: "audit-trail", DisplayName: "Audit trail", Default: false},
			FeatureUnset, FeatureUnset, true, false,
		},
		{
			"non-manageable still obeys the instance layer",
			FeatureMeta{Key: "audit-trail", DisplayName: "Audit trail", Default: true},
			FeatureOff, FeatureUnset, false, false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveFeature(tc.meta, tc.instance, tc.account, tc.granted)
			if got != tc.want {
				t.Fatalf("ResolveFeature(default=%v manageable=%v grantable=%v, instance=%v, account=%v, granted=%v) = %v, want %v",
					tc.meta.Default, tc.meta.Manageable, tc.meta.Grantable,
					tc.instance, tc.account, tc.granted, got, tc.want)
			}
		})
	}
}

// TestFeatureStateZeroValueIsUnset pins the property that makes row-absence the
// storage representation of "unset": a FeatureState nobody assigned must say
// nothing, so a missing row needs no nullable column and no sentinel.
func TestFeatureStateZeroValueIsUnset(t *testing.T) {
	var s FeatureState
	if s != FeatureUnset {
		t.Fatalf("zero FeatureState = %v, want FeatureUnset", s)
	}
}

func TestFeatureMetaValidate(t *testing.T) {
	cases := []struct {
		name    string
		meta    FeatureMeta
		wantErr string
	}{
		{"valid", FeatureMeta{Key: "ledger-export", DisplayName: "Ledger export"}, ""},
		{"single letter key", FeatureMeta{Key: "a", DisplayName: "A"}, ""},
		{"digits and hyphens", FeatureMeta{Key: "sea-math-2027", DisplayName: "SEA math"}, ""},
		{"empty key", FeatureMeta{DisplayName: "X"}, "must match"},
		{"uppercase rejected", FeatureMeta{Key: "LedgerExport", DisplayName: "X"}, "must match"},
		{"underscore rejected", FeatureMeta{Key: "ledger_export", DisplayName: "X"}, "must match"},
		{"leading digit rejected", FeatureMeta{Key: "2027-sea", DisplayName: "X"}, "must match"},
		{"namespace prefix rejected", FeatureMeta{Key: "feature.ledger", DisplayName: "X"}, "must match"},
		{"missing display name", FeatureMeta{Key: "ledger-export"}, "display name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.meta.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
