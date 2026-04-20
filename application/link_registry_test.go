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
	"sync"
	"testing"
)

func TestLinkRegistry_AddValidation(t *testing.T) {
	t.Parallel()
	r := NewLinkRegistry()

	cases := []struct {
		name    string
		def     PresetLinkDefinition
		wantErr bool
	}{
		{"missing source", PresetLinkDefinition{TargetType: "g", PropertyName: "g"}, true},
		{"missing target", PresetLinkDefinition{SourceType: "i", PropertyName: "g"}, true},
		{"missing property", PresetLinkDefinition{SourceType: "i", TargetType: "g"}, true},
		{"valid", PresetLinkDefinition{SourceType: "i", TargetType: "g", PropertyName: "guardian"}, false},
		{"valid kebab source", PresetLinkDefinition{
			SourceType: "invoice-line", TargetType: "guardian", PropertyName: "guardian",
		}, false},
		{"uppercase source rejected", PresetLinkDefinition{
			SourceType: "Invoice", TargetType: "guardian", PropertyName: "guardian",
		}, true},
		{"snake_case source rejected", PresetLinkDefinition{
			SourceType: "invoice_line", TargetType: "guardian", PropertyName: "guardian",
		}, true},
		{"uppercase target rejected", PresetLinkDefinition{
			SourceType: "invoice", TargetType: "Guardian", PropertyName: "guardian",
		}, true},
		{"reserved property rejected", PresetLinkDefinition{
			SourceType: "invoice", TargetType: "guardian", PropertyName: "id",
		}, true},
		{"reserved property typeSlug", PresetLinkDefinition{
			SourceType: "invoice", TargetType: "guardian", PropertyName: "typeSlug",
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.Add(tc.def)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLinkRegistry_DedupesOnSourceAndProperty(t *testing.T) {
	t.Parallel()
	r := NewLinkRegistry()
	first := PresetLinkDefinition{
		Name: "first", SourceType: "invoice", TargetType: "guardian",
		PropertyName: "guardian", DisplayProperty: "name",
	}
	second := PresetLinkDefinition{
		Name: "second", SourceType: "invoice", TargetType: "guardian",
		PropertyName: "guardian", DisplayProperty: "fullName",
	}
	if err := r.Add(first); err != nil {
		t.Fatalf("Add first: %v", err)
	}
	if err := r.Add(second); err != nil {
		t.Fatalf("Add second: %v", err)
	}

	all := r.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d: %+v", len(all), all)
	}
	if all[0].DisplayProperty != "fullName" {
		t.Errorf("expected second def to overwrite first, got %+v", all[0])
	}
}

func TestLinkRegistry_DifferentPropertiesCoexist(t *testing.T) {
	t.Parallel()
	r := NewLinkRegistry()
	_ = r.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "guardian", PropertyName: "guardian",
	})
	_ = r.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "student", PropertyName: "student",
	})
	if got := len(r.All()); got != 2 {
		t.Errorf("expected 2 entries, got %d", got)
	}
}

func TestLinkRegistry_ActiveForRequiresBothEndpoints(t *testing.T) {
	t.Parallel()
	r := NewLinkRegistry()
	_ = r.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "guardian", PropertyName: "guardian",
	})
	_ = r.Add(PresetLinkDefinition{
		SourceType: "task", TargetType: "project", PropertyName: "project",
	})

	// Only finance side installed — no links active.
	active := r.ActiveFor(map[string]bool{"invoice": true})
	if len(active) != 0 {
		t.Errorf("expected 0 active links with only source installed, got %+v", active)
	}

	// Only education side installed — still no links active.
	active = r.ActiveFor(map[string]bool{"guardian": true})
	if len(active) != 0 {
		t.Errorf("expected 0 active links with only target installed, got %+v", active)
	}

	// Both endpoints of one link installed.
	active = r.ActiveFor(map[string]bool{"invoice": true, "guardian": true})
	if len(active) != 1 || active[0].SourceType != "invoice" {
		t.Errorf("expected 1 active link (invoice→guardian), got %+v", active)
	}

	// Both links activatable.
	active = r.ActiveFor(map[string]bool{
		"invoice": true, "guardian": true, "task": true, "project": true,
	})
	if len(active) != 2 {
		t.Errorf("expected 2 active links, got %+v", active)
	}
}

func TestLinkRegistry_BySourceAndByTarget(t *testing.T) {
	t.Parallel()
	r := NewLinkRegistry()
	_ = r.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "guardian", PropertyName: "guardian",
	})
	_ = r.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "student", PropertyName: "student",
	})
	_ = r.Add(PresetLinkDefinition{
		SourceType: "payment", TargetType: "guardian", PropertyName: "guardian",
	})

	if got := len(r.BySource("invoice")); got != 2 {
		t.Errorf("BySource(invoice): expected 2, got %d", got)
	}
	if got := len(r.BySource("payment")); got != 1 {
		t.Errorf("BySource(payment): expected 1, got %d", got)
	}
	if got := len(r.BySource("unknown")); got != 0 {
		t.Errorf("BySource(unknown): expected 0, got %d", got)
	}
	if got := len(r.ByTarget("guardian")); got != 2 {
		t.Errorf("ByTarget(guardian): expected 2, got %d", got)
	}
	if got := len(r.ByTarget("student")); got != 1 {
		t.Errorf("ByTarget(student): expected 1, got %d", got)
	}
}

func TestLinkRegistry_ConcurrentAdd(t *testing.T) {
	t.Parallel()
	r := NewLinkRegistry()

	// Race detector flags any map/slice write without lock protection.
	var wg sync.WaitGroup
	const goroutines = 50
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			_ = r.Add(PresetLinkDefinition{
				SourceType:   "src",
				TargetType:   "tgt",
				PropertyName: propName(i),
			})
		}(i)
	}
	wg.Wait()

	if got := len(r.All()); got != goroutines {
		t.Errorf("expected %d entries, got %d", goroutines, got)
	}
}

func propName(i int) string {
	// Avoid fmt to keep the hot loop allocation-free — any stable unique string works.
	const chars = "abcdefghijklmnopqrstuvwxyz"
	hi, lo := i/len(chars), i%len(chars)
	return string([]byte{chars[hi%len(chars)], chars[lo]})
}

func TestRegisterLink_WritesToDefaultRegistry(t *testing.T) {
	// Intentionally not t.Parallel(): this test mutates package-global state.
	before := len(defaultLinkRegistry.All())
	defer func() {
		// Reset to avoid leaking into other tests. There's no public Remove
		// helper so replace the default registry for the duration of the test
		// process — safe here because nothing else runs in parallel with this.
		defaultLinkRegistry = NewLinkRegistry()
	}()

	def := PresetLinkDefinition{
		Name: "test-link", SourceType: "a", TargetType: "b", PropertyName: "b",
	}
	if err := RegisterLink(def); err != nil {
		t.Fatalf("RegisterLink: %v", err)
	}
	if got := len(defaultLinkRegistry.All()); got != before+1 {
		t.Errorf("expected registry to grow by 1 (was %d, now %d)", before, got)
	}
	if got := DefaultLinkRegistry(); got != defaultLinkRegistry {
		t.Errorf("DefaultLinkRegistry returned a different registry than the package-global")
	}
}
