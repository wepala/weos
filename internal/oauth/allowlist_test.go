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

package oauth

import "testing"

func TestEmailAllowed(t *testing.T) {
	allow := []string{"akeem@example.com", "second@example.com"}
	cases := []struct {
		name  string
		email string
		want  bool
	}{
		{"first entry", "akeem@example.com", true},
		{"second entry", "second@example.com", true},
		{"not listed", "stranger@example.com", false},
		{"empty email", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := emailAllowed(tc.email, allow); got != tc.want {
				t.Fatalf("emailAllowed(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}
