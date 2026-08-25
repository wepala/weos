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

package jsonld

// HouseVocabBase is the root every WeOS house vocabulary hangs off. Every
// preset, query, and predicate constant builds its IRIs from these so a move
// of the vocabulary domain (issue #520 moved it from weos.org) is one edit.
// Changing it is a data migration for existing installs, not a rename: the
// boot holds the moved prefix and `adopt-term` + `normalize-edge-keys
// --restamp` carry the records over.
const HouseVocabBase = "https://weos.io/vocab/"

// The house vocabularies, one namespace per domain, each ending in "#" as
// every prefix definition must.
const (
	MealPlanningVocab = HouseVocabBase + "meal-planning#"
	MemoryVocab       = HouseVocabBase + "memory#"
	AgentsVocab       = HouseVocabBase + "agents#"
)
