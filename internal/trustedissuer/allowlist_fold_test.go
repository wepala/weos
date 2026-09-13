package trustedissuer_test

import (
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/trustedissuer"
)

// The allowlist folds exactly as owner binding does — ASCII capitals only, on
// both the entry and the assertion's email — so an address it admits is one
// binding can match, and one that differs from an entry only in a non-ASCII
// capital is refused rather than admitted.
func TestTheAllowlistFoldsOnlyASCIICapitalsAsOwnerBindingDoes(t *testing.T) {
	cases := map[string]struct {
		listed, presented string
		admitted          bool
	}{
		"ASCII capitals":                  {"karl.berg@harborlegal.example", "Karl.Berg@HarborLegal.example", true},
		"the Kelvin sign for a k":         {"karl.berg@harborlegal.example", "Karl.berg@harborlegal.example", false},
		"a listed Kelvin sign for a k":    {"Karl.berg@harborlegal.example", "karl.berg@harborlegal.example", false},
		"the same accented letter":        {"élise.martin@harborlegal.example", "élise.martin@harborlegal.example", true},
		"an accented capital presented":   {"élise.martin@harborlegal.example", "Élise.martin@harborlegal.example", false},
		"an accented capital listed":      {"Élise.martin@harborlegal.example", "élise.martin@harborlegal.example", false},
		"the same accented capital":       {"Élise.martin@harborlegal.example", "Élise.Martin@HarborLegal.example", true},
		"spaces around the listed entry":  {"  karl.berg@harborlegal.example ", "karl.berg@harborlegal.example", true},
		"spaces around the presented one": {"karl.berg@harborlegal.example", " KARL.BERG@harborlegal.example  ", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if binds := repositories.FoldCredentialEmail(c.listed) == repositories.FoldCredentialEmail(c.presented); binds != c.admitted {
				t.Fatalf("the case disagrees with owner binding's fold: binds = %v, admitted = %v", binds, c.admitted)
			}
			f := allowlisted(t, c.listed)
			claims := f.good(t)
			claims["email"] = c.presented
			_, err := f.verify(f.door.sign(currentKey, claims))
			if c.admitted {
				requireAccepted(t, err)
				return
			}
			requireReason(t, err, trustedissuer.ReasonAllowlist)
		})
	}
}
