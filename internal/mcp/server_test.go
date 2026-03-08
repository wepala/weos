package mcp

import (
	"testing"
)

func TestResolveEnabled_EmptyReturnsAll(t *testing.T) {
	for _, input := range [][]string{nil, {}} {
		enabled := resolveEnabled(input)
		for _, s := range AllServices {
			if !enabled[s] {
				t.Errorf("expected service %q to be enabled for empty input", s)
			}
		}
		if len(enabled) != len(AllServices) {
			t.Errorf("expected %d enabled services, got %d", len(AllServices), len(enabled))
		}
	}
}

func TestResolveEnabled_Subset(t *testing.T) {
	enabled := resolveEnabled([]string{"person", "organization"})
	if !enabled[ServicePerson] {
		t.Error("expected person to be enabled")
	}
	if !enabled[ServiceOrganization] {
		t.Error("expected organization to be enabled")
	}
	if enabled[ServiceResourceType] {
		t.Error("expected resource-type to be disabled")
	}
	if len(enabled) != 2 {
		t.Errorf("expected 2 enabled services, got %d", len(enabled))
	}
}

func TestValidateServiceNames_Valid(t *testing.T) {
	if err := ValidateServiceNames([]string{"person", "organization", "resource-type"}); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateServiceNames_Invalid(t *testing.T) {
	err := ValidateServiceNames([]string{"person", "bogus", "fake"})
	if err == nil {
		t.Fatal("expected error for invalid service names")
	}
	msg := err.Error()
	if !contains(msg, "bogus") || !contains(msg, "fake") {
		t.Errorf("expected error to list invalid names, got: %s", msg)
	}
}

func TestValidServiceNames_ReturnsAll(t *testing.T) {
	names := ValidServiceNames()
	if len(names) != len(AllServices) {
		t.Errorf("expected %d names, got %d", len(AllServices), len(names))
	}
	expected := map[string]bool{
		"person": true, "organization": true,
		"resource-type": true, "resource": true,
	}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected service name: %s", n)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
