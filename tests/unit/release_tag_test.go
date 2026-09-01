// Package unit holds the unit-test layer required by Article V of the
// constitution.
package unit

import (
	"os"
	"regexp"
	"testing"

	"golang.org/x/mod/semver"
)

// publishedV3Tags is every tag published on the v3 line under the old
// `alphaN` scheme, frozen as of 2026-08-31. They are never renamed or deleted
// — a module proxy caches them and a pinned consumer would break — so any new
// tag has to sort above all of them to be reachable by `go get -u`.
var publishedV3Tags = []string{
	"v3.0.0-alpha",
	"v3.0.1-alpha1", "v3.0.1-alpha2", "v3.0.1-alpha3", "v3.0.1-alpha4",
	"v3.0.1-alpha5", "v3.0.1-alpha6", "v3.0.1-alpha7", "v3.0.1-alpha8",
	"v3.0.1-alpha9", "v3.0.1-alpha10", "v3.0.1-alpha11", "v3.0.1-alpha12",
	"v3.0.1-alpha13", "v3.0.1-alpha14", "v3.0.1-alpha15", "v3.0.1-alpha16",
	"v3.0.1-alpha17", "v3.0.1-alpha18", "v3.0.1-alpha19", "v3.0.1-alpha20",
	"v3.0.1-alpha21",
}

// highestPublishedV3Tag is the tag `go list -m -versions` reported as the
// newest on 2026-08-31, twelve tags behind the real tip. It is named here on
// its own because it is the specific version the bug stalls upgrades at.
const highestPublishedV3Tag = "v3.0.1-alpha9"

var releaseTagPrefixDecl = regexp.MustCompile(`(?m)^RELEASE_TAG_PREFIX\s*:?=[ \t]*(\S+)`)

// releaseTagPrefix reads the declared release tag scheme out of the
// repository Makefile. The Makefile is the single source of truth: its
// `check-release-tag` target refuses anything that does not match, and these
// tests prove that what it declares actually sorts.
func releaseTagPrefix(t *testing.T) string {
	t.Helper()

	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	match := releaseTagPrefixDecl.FindSubmatch(makefile)
	if match == nil {
		t.Fatal("the Makefile declares no RELEASE_TAG_PREFIX; the release tag scheme has no single source of truth")
	}
	return string(match[1])
}

// tag builds the Nth release tag under the declared scheme.
func tag(t *testing.T, n string) string {
	t.Helper()
	return releaseTagPrefix(t) + n
}

func TestDeclaredReleaseTagIsValidSemver(t *testing.T) {
	first := tag(t, "1")
	if !semver.IsValid(first) {
		t.Fatalf("the declared scheme produces %q, which is not valid semver", first)
	}
}

// The bug: every tag from alpha10 to alpha21 sorts below alpha9, so `go get -u`
// never reaches them. A new tag is only useful if it sorts above all of them.
func TestNextReleaseTagSortsAboveEveryPublishedV3Tag(t *testing.T) {
	next := tag(t, "1")

	for _, published := range publishedV3Tags {
		if semver.Compare(next, published) <= 0 {
			t.Errorf("%s does not sort above the published tag %s, so `go get -u` would never reach it", next, published)
		}
	}

	if semver.Compare(next, highestPublishedV3Tag) <= 0 {
		t.Errorf("%s does not sort above %s, the version `go get -u` is stalled at", next, highestPublishedV3Tag)
	}
}

// The scheme also has to keep working as the number grows past 9, which is the
// exact point the old one broke.
func TestReleaseTagNumbersCompareNumerically(t *testing.T) {
	ascending := []string{"1", "2", "9", "10", "21", "100"}

	for i := 1; i < len(ascending); i++ {
		lower, higher := tag(t, ascending[i-1]), tag(t, ascending[i])
		if semver.Compare(higher, lower) <= 0 {
			t.Errorf("%s does not sort above %s; the number is being compared as a string, not numerically", higher, lower)
		}
	}
}

// Both schemes wm-1jkb's own body prescribed were disproven: each sorts BELOW
// the version upgrades are already stalled at, so neither fixes anything. This
// pins that result so the prescription cannot be adopted later by someone
// reading the bug report alone.
func TestSchemesRejectedForThisBugStillSortBelowAlpha9(t *testing.T) {
	rejected := map[string]string{
		"v3.0.1-alpha.22": "a separate numeric identifier makes the FIRST identifier `alpha`, a shorter prefix of `alpha9`, and a shorter prefix sorts first",
		"v3.0.1-alpha022": "zero padding keeps one string identifier, and `alpha0...` still sorts below `alpha9`",
	}

	for candidate, why := range rejected {
		if semver.Compare(candidate, highestPublishedV3Tag) > 0 {
			t.Errorf("%s now sorts above %s, which contradicts the measurement this decision rests on (%s)", candidate, highestPublishedV3Tag, why)
		}
	}
}
