package mcp

import (
	"testing"
)

// TestResolveEnabled_EmptyReturnsAllButOptIn: naming no services enables every
// group except those that must be asked for by name. The exception exists
// because some tools change instance-wide state and the stdio transport is
// trusted without a permission check.
func TestResolveEnabled_EmptyReturnsAllButOptIn(t *testing.T) {
	for _, input := range [][]string{nil, {}} {
		enabled := resolveEnabled(input)
		want := 0
		for _, s := range AllServices {
			if DefaultOffServices[s] {
				if enabled[s] {
					t.Errorf("service %q is opt-in but was enabled for empty input", s)
				}
				continue
			}
			want++
			if !enabled[s] {
				t.Errorf("expected service %q to be enabled for empty input", s)
			}
		}
		if len(enabled) != want {
			t.Errorf("expected %d enabled services, got %d", want, len(enabled))
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
		"knowledge-graph": true, "memory": true,
		"feature": true,
	}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected service name: %s", n)
		}
	}
	// Both directions: a service dropped from AllServices should fail here too,
	// not just an unexpected one added.
	for name := range expected {
		found := false
		for _, n := range names {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("service name missing from AllServices: %s", name)
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

// TestFeatureToolsAreNotEnabledByDefault: the feature tools change
// instance-wide state and `weos mcp` over stdio is trusted without a
// permission check, so an LLM pointed at the graph must not find the
// switchboard in reach unless the operator named it.
func TestFeatureToolsAreNotEnabledByDefault(t *testing.T) {
	if resolveEnabled(nil)[ServiceFeature] {
		t.Fatal("the feature service is enabled when no services are named")
	}
	if !resolveEnabled(nil)[ServiceResource] {
		t.Fatal("naming no services should still enable the ordinary groups")
	}
	if !resolveEnabled([]string{"feature"})[ServiceFeature] {
		t.Fatal("naming the feature service did not enable it")
	}
	// It is still a valid name, so --services feature must not be rejected.
	if err := ValidateServiceNames([]string{"feature"}); err != nil {
		t.Fatalf("ValidateServiceNames rejected a real service: %v", err)
	}
}
