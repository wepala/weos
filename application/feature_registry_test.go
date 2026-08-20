package application

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
)

func testFeature(key, display string) entities.FeatureMeta {
	return entities.FeatureMeta{Key: key, DisplayName: display, Default: true, Manageable: true, Grantable: true}
}

func newTestFeatureRegistry(t *testing.T, declared ...entities.FeatureMeta) (*FeatureRegistry, *PresetRegistry) {
	t.Helper()
	presets := NewPresetRegistry()
	r, err := NewFeatureRegistry(config.Default(), presets, nil, declared)
	if err != nil {
		t.Fatalf("NewFeatureRegistry: %v", err)
	}
	return r, presets
}

func TestFeatureRegistryCodeDeclarations(t *testing.T) {
	r, _ := newTestFeatureRegistry(t,
		testFeature("episodic-recall", "Episodic recall"),
		testFeature("ledger-export", "Ledger export"),
	)

	m, ok := r.Lookup("episodic-recall")
	if !ok {
		t.Fatal("Lookup(episodic-recall) = not found, want found")
	}
	if m.DisplayName != "Episodic recall" {
		t.Fatalf("DisplayName = %q, want %q", m.DisplayName, "Episodic recall")
	}
	if _, ok := r.Lookup("shipping-labels"); ok {
		t.Fatal("Lookup of an undeclared key returned found, want not found")
	}

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d features, want 2", len(all))
	}
	// All() is sorted by key so the CLI and admin listing are stable.
	if all[0].Key != "episodic-recall" || all[1].Key != "ledger-export" {
		t.Fatalf("All() = %q, %q; want sorted by key", all[0].Key, all[1].Key)
	}
}

func TestFeatureRegistryRejectsInvalidDeclaration(t *testing.T) {
	presets := NewPresetRegistry()
	_, err := NewFeatureRegistry(config.Default(), presets, nil,
		[]entities.FeatureMeta{{Key: "Not A Key", DisplayName: "X"}})
	if err == nil {
		t.Fatal("NewFeatureRegistry accepted an invalid key, want a boot failure")
	}
}

func TestFeatureRegistryRejectsDuplicateCodeKey(t *testing.T) {
	r, _ := newTestFeatureRegistry(t, testFeature("ledger-export", "Ledger export"))
	err := r.Register(testFeature("ledger-export", "Ledger export again"))
	if err == nil {
		t.Fatal("Register accepted a duplicate key, want an error")
	}
}

// TestFeatureRegistrySeesPresetInstalledAfterBoot is the registry half of the
// contract scenario "Installing a preset adds the features the preset
// declares". The registry is built BEFORE the preset is added, proving
// declarations are swept on demand rather than copied at construction.
func TestFeatureRegistrySeesPresetInstalledAfterBoot(t *testing.T) {
	r, presets := newTestFeatureRegistry(t, testFeature("episodic-recall", "Episodic recall"))

	if _, ok := r.Lookup("invoice-export"); ok {
		t.Fatal("invoice-export was found before its preset was registered")
	}

	if err := presets.Add(PresetDefinition{
		Name: "billing-demo",
		Features: map[string]entities.FeatureMeta{
			"invoice-export": {Key: "invoice-export", DisplayName: "Invoice export", Default: false},
		},
	}); err != nil {
		t.Fatalf("presets.Add: %v", err)
	}

	m, ok := r.Lookup("invoice-export")
	if !ok {
		t.Fatal("Lookup(invoice-export) = not found after the preset was registered")
	}
	if m.Default {
		t.Fatal("invoice-export resolved Default=true, want the declared false")
	}
	// The code-declared feature must survive the sweep.
	if _, ok := r.Lookup("episodic-recall"); !ok {
		t.Fatal("episodic-recall disappeared once a preset declared features")
	}
	if len(r.All()) != 2 {
		t.Fatalf("All() returned %d features, want 2", len(r.All()))
	}
}

// TestFeatureRegistryCodeWinsOverPreset pins the collision rule: a downstream
// binary that declares a key a preset also uses is overriding it deliberately
// and cannot edit the preset to say so.
func TestFeatureRegistryCodeWinsOverPreset(t *testing.T) {
	r, presets := newTestFeatureRegistry(t,
		entities.FeatureMeta{Key: "invoice-export", DisplayName: "From code", Default: true},
	)
	if err := presets.Add(PresetDefinition{
		Name: "billing-demo",
		Features: map[string]entities.FeatureMeta{
			"invoice-export": {Key: "invoice-export", DisplayName: "From preset", Default: false},
		},
	}); err != nil {
		t.Fatalf("presets.Add: %v", err)
	}

	m, ok := r.Lookup("invoice-export")
	if !ok {
		t.Fatal("Lookup(invoice-export) = not found")
	}
	if m.DisplayName != "From code" || !m.Default {
		t.Fatalf("Lookup returned %+v, want the code declaration to win", m)
	}

	all := r.All()
	if len(all) != 1 {
		t.Fatalf("All() returned %d features, want 1 merged entry", len(all))
	}
	if all[0].DisplayName != "From code" {
		t.Fatalf("All() returned %q, want the code declaration to win", all[0].DisplayName)
	}
}

// TestPresetRegistryFeaturesMergeLastWins mirrors BehaviorsMeta's documented
// merge semantics: presets are walked alphabetically and the last declaration
// of a key wins.
func TestPresetRegistryFeaturesMergeLastWins(t *testing.T) {
	presets := NewPresetRegistry()
	for _, name := range []string{"alpha", "zulu"} {
		if err := presets.Add(PresetDefinition{
			Name: name,
			Features: map[string]entities.FeatureMeta{
				"shared": {Key: "shared", DisplayName: name},
			},
		}); err != nil {
			t.Fatalf("presets.Add(%s): %v", name, err)
		}
	}
	if got := presets.Features()["shared"].DisplayName; got != "zulu" {
		t.Fatalf("merged DisplayName = %q, want %q (alphabetical, last wins)", got, "zulu")
	}
}

// TestPresetDefinitionCloneCopiesFeatures guards the deep-copy contract on
// Get/List: a caller mutating the returned definition must not reach into the
// registry's own copy.
func TestPresetDefinitionCloneCopiesFeatures(t *testing.T) {
	presets := NewPresetRegistry()
	if err := presets.Add(PresetDefinition{
		Name:     "billing-demo",
		Features: map[string]entities.FeatureMeta{"invoice-export": {Key: "invoice-export", DisplayName: "Invoice export"}},
	}); err != nil {
		t.Fatalf("presets.Add: %v", err)
	}

	def, ok := presets.Get("billing-demo")
	if !ok {
		t.Fatal("Get(billing-demo) = not found")
	}
	def.Features["invoice-export"] = entities.FeatureMeta{Key: "invoice-export", DisplayName: "mutated"}
	def.Features["injected"] = entities.FeatureMeta{Key: "injected", DisplayName: "injected"}

	fresh := presets.Features()
	if fresh["invoice-export"].DisplayName != "Invoice export" {
		t.Fatal("mutating a cloned definition changed the registry's declaration")
	}
	if _, ok := fresh["injected"]; ok {
		t.Fatal("mutating a cloned definition injected a feature into the registry")
	}
}

// TestPresetRegistryRejectsInvalidFeatureDeclarations holds preset
// declarations to the same standard as code declarations. Without this, the
// registry's "an invalid declaration fails the boot" contract only held for
// code, and a preset could ship a key nothing can gate.
func TestPresetRegistryRejectsInvalidFeatureDeclarations(t *testing.T) {
	cases := []struct {
		name     string
		features map[string]entities.FeatureMeta
		want     string
	}{
		{
			"key disagrees with the declaration",
			map[string]entities.FeatureMeta{"a": {Key: "b", DisplayName: "B"}},
			"names itself",
		},
		{
			"invalid key",
			map[string]entities.FeatureMeta{"Not A Key": {Key: "Not A Key", DisplayName: "X"}},
			"must match",
		},
		{
			"missing display name",
			map[string]entities.FeatureMeta{"ledger-export": {Key: "ledger-export"}},
			"display name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewPresetRegistry().Add(PresetDefinition{Name: "broken", Features: tc.features})
			if err == nil {
				t.Fatal("Add accepted an invalid feature declaration")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestMismatchedPresetKeyWouldStrandResolution documents WHY the key must
// match: a declaration found by one key but folded under another can never be
// resolved, so it would silently ignore every stored override.
func TestMismatchedPresetKeyWouldStrandResolution(t *testing.T) {
	r, presets := newTestFeatureRegistry(t)
	// Bypass Add's validation to build the shape the guard now prevents.
	presets.MustAdd(PresetDefinition{Name: "ok", Features: map[string]entities.FeatureMeta{
		"invoice-export": {Key: "invoice-export", DisplayName: "Invoice export"},
	}})
	if _, ok := r.Lookup("invoice-export"); !ok {
		t.Fatal("a well-formed preset declaration was not found")
	}
}

// TestFeatureRegistryReadsConfigDeclarations covers the third declaration
// source. It exists because declarations are never persisted, so a running
// server and a `weos feature` invocation — separate processes — can only agree
// about which features exist by both reading the same environment.
func TestFeatureRegistryReadsConfigDeclarations(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Declared = []entities.FeatureMeta{
		{Key: "ledger-export", DisplayName: "Ledger export", Default: false, Manageable: true},
	}
	r, err := NewFeatureRegistry(cfg, NewPresetRegistry(), nil, nil)
	if err != nil {
		t.Fatalf("NewFeatureRegistry: %v", err)
	}
	m, ok := r.Lookup("ledger-export")
	if !ok {
		t.Fatal("a configured declaration was not registered")
	}
	if !m.Manageable || m.Default {
		t.Fatalf("the configured declaration lost its fields: %+v", m)
	}
}

func TestFeatureRegistryRefusesMalformedConfigDeclarations(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Declared = []entities.FeatureMeta{{Key: "Not A Key", DisplayName: "X"}}
	if _, err := NewFeatureRegistry(cfg, NewPresetRegistry(), nil, nil); err == nil {
		t.Fatal("an invalid configured declaration was accepted, want a boot failure")
	}
}

// TestFeatureRegistryRefusesAConfigKeyCodeAlreadyOwns keeps configuration from
// silently redefining something the binary declares — two owners of one key
// means one of them is gating the wrong thing.
func TestFeatureRegistryRefusesAConfigKeyCodeAlreadyOwns(t *testing.T) {
	cfg := config.Default()
	cfg.Features.Declared = []entities.FeatureMeta{{Key: "ledger-export", DisplayName: "From config"}}
	_, err := NewFeatureRegistry(cfg, NewPresetRegistry(), nil,
		[]entities.FeatureMeta{{Key: "ledger-export", DisplayName: "From code"}})
	if err == nil {
		t.Fatal("configuration silently redeclared a key code already owns")
	}
	if !strings.Contains(err.Error(), "FEATURES") {
		t.Fatalf("the error does not say the clash came from configuration: %v", err)
	}
}

// TestFeatureRegistryRefusesToBootOnAMalformedFeaturesValue: a malformed
// FEATURES value must stop the boot rather than declare nothing. Declaring
// nothing looks exactly like a working instance with every feature off, which
// is the hardest failure to notice and the hardest to explain.
func TestFeatureRegistryRefusesToBootOnAMalformedFeaturesValue(t *testing.T) {
	cfg := config.Default()
	cfg.Features.DeclarationError = fmt.Errorf("FEATURES must be a JSON array")
	if _, err := NewFeatureRegistry(cfg, NewPresetRegistry(), nil, nil); err == nil {
		t.Fatal("a malformed FEATURES value was ignored, want a boot failure")
	}
}
