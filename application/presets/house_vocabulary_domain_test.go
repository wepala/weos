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

package presets_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// Issue #520: WeOS-minted vocabulary lives under https://weos.io/vocab/…,
// the domain WeOS owns. weos.org is not the WeOS domain, and a term that
// resolved there pointed at a namespace nobody controls.

// TestNoPresetSourceReferencesWeosOrg is the story's literal criterion: no
// file under application/presets/ references weos.org. It reads the source
// tree rather than the registry because the criterion is about what is
// authored — a test fixture asserting the old IRI counts too.
func TestNoPresetSourceReferencesWeosOrg(t *testing.T) {
	root := "."
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "weos.org") && !strings.Contains(line, "weos.org is not") &&
				!strings.Contains(path, "house_vocabulary_domain_test.go") {
				t.Errorf("%s:%d references weos.org: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestEveryHouseIRIResolvesUnderWeosIO sweeps the whole registry, not the
// three presets the story names: nothing stops a fourth preset from having
// copied the string, and the boot reconcile treats them all alike.
func TestEveryHouseIRIResolvesUnderWeosIO(t *testing.T) {
	for _, preset := range presets.NewDefaultRegistry().List() {
		for _, pt := range preset.Types {
			var raw map[string]any
			if err := json.Unmarshal(pt.Context, &raw); err != nil {
				t.Fatalf("%s/%s: context is not an object: %v", preset.Name, pt.Slug, err)
			}
			vocab, forward := jsonld.ParseContext(pt.Context)
			iris := map[string]string{"@vocab": vocab}
			for term, iri := range forward {
				iris[term] = iri
			}
			if typ, ok := raw["@type"].(string); ok {
				var ctxAny map[string]any
				_ = json.Unmarshal(pt.Context, &ctxAny)
				iris["@type"] = jsonld.ExpandIRI(typ, vocab, ctxAny)
			}
			for term, iri := range iris {
				if strings.Contains(iri, "weos.org") {
					t.Errorf("%s/%s: %q resolves to %s, a domain WeOS does not own", preset.Name, pt.Slug, term, iri)
				}
				if strings.Contains(iri, "weos.io") && !strings.HasPrefix(iri, "https://weos.io/vocab/") {
					t.Errorf("%s/%s: %q resolves to %s, outside https://weos.io/vocab/", preset.Name, pt.Slug, term, iri)
				}
			}
		}
	}
}
