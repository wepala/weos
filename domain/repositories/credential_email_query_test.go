package repositories

import "testing"

func TestFoldCredentialEmailFoldsOnlyASCIICapitalsAndSpaces(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"ASCII capitals and surrounding spaces", "  Dana.Whitfield@HarborLegal.example ", "dana.whitfield@harborlegal.example"},
		{"a non-ASCII capital is kept", "ÉLISE.MARTIN@harborlegal.example", "Élise.martin@harborlegal.example"},
		{"a non-ASCII lower case is kept", "élise.martin@harborlegal.example", "élise.martin@harborlegal.example"},
		// U+212A KELVIN SIGN lower-cases to "k" under a Unicode fold.
		{"the Kelvin sign is not a k", "Karl.berg@harborlegal.example", "Karl.berg@harborlegal.example"},
		{"only spaces are trimmed", "\tpat.lee@harborlegal.example", "\tpat.lee@harborlegal.example"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := FoldCredentialEmail(c.in); got != c.want {
			t.Errorf("%s: FoldCredentialEmail(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
