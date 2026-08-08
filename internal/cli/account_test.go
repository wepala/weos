package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
)

// TestDescribeRegisterOutcome covers the one decision the entrypoint acts on:
// whether the boot carries on. The acceptance scenarios drive the real command
// for the reachable cases; this covers the mapping itself, including the
// password-support branch, which no configuration of this codebase can reach
// (application/module.go always provides a password-credential repository) and
// which therefore cannot honestly be an acceptance scenario.
func TestDescribeRegisterOutcome(t *testing.T) {
	const email = "ops@harborlegal.example"

	tests := []struct {
		name       string
		err        error
		wantFailed bool
		wantSaid   string
	}{
		{
			name:       "a created account carries on",
			err:        nil,
			wantFailed: false,
			wantSaid:   "Created account",
		},
		{
			name:       "an account that already exists carries on",
			err:        authapp.ErrEmailAlreadyTaken,
			wantFailed: false,
			wantSaid:   "already exists",
		},
		{
			name:       "wrapped, an account that already exists still carries on",
			err:        fmt.Errorf("register: %w", authapp.ErrEmailAlreadyTaken),
			wantFailed: false,
			wantSaid:   "already exists",
		},
		{
			name:       "missing password support stops the boot and says so",
			err:        authapp.ErrPasswordSupportNotConfigured,
			wantFailed: true,
			wantSaid:   "password support",
		},
		{
			name:       "an unrecognised failure stops the boot",
			err:        errors.New("disk is on fire"),
			wantFailed: true,
			wantSaid:   "disk is on fire",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report, failed := describeRegisterOutcome(email, "some-password", tc.err)
			if failed != tc.wantFailed {
				t.Errorf("expected failed=%v, got %v (report: %s)", tc.wantFailed, failed, report)
			}
			if !strings.Contains(report, tc.wantSaid) {
				t.Errorf("expected the report to mention %q, got: %s", tc.wantSaid, report)
			}
		})
	}
}

// TestDescribeRegisterOutcomeNeverRepeatsThePassword guards the property the
// story cares about most: an operator reading a failure report must not find
// the password in it. The mapping is not given the password at all, so this
// fails the moment someone changes that.
func TestDescribeRegisterOutcomeNeverRepeatsThePassword(t *testing.T) {
	// Deliberately full of characters %q escapes. A password of plain letters
	// and dashes would let this test pass while the escaped rendering of a
	// realistic password sat in the message untouched: `he "said" hi` formatted
	// with %q is a different string from the password itself, so a substring
	// replace walks straight past it.
	passwords := []string{
		"correct-horse-battery-staple",
		`he "said" hi`,
		`back\slash`,
		"tab\there",
	}

	for _, password := range passwords {
		for _, err := range []error{
			nil,
			authapp.ErrEmailAlreadyTaken,
			authapp.ErrPasswordSupportNotConfigured,
			// The realistic leak: a lower layer that put the password in its
			// own error, which this mapping would otherwise pass through.
			fmt.Errorf("hash password %v: bad cost", password),
			fmt.Errorf("hash password %q: bad cost", password),
		} {
			report, _ := describeRegisterOutcome("ops@harborlegal.example", password, err)
			if strings.Contains(report, password) {
				t.Errorf("the password %q appeared in the report for %v: %s", password, err, report)
			}
			if escaped := strings.Trim(strconv.Quote(password), `"`); escaped != password &&
				strings.Contains(report, escaped) {
				t.Errorf("the escaped password %q appeared in the report for %v: %s", escaped, err, report)
			}
		}
	}
}
