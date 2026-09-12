package gorm

import (
	"context"
	"sort"
	"testing"
)

func TestCredentialEmailQueryMatchesWithoutRegardToCase(t *testing.T) {
	ctx := context.Background()
	db := newFeatureTestDB(t)

	// credentials is pericarp's projection; core reads it without owning the
	// struct, so the test creates it the same way — by table name.
	if err := db.Exec(`CREATE TABLE credentials (
		id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, provider TEXT NOT NULL,
		provider_user_id TEXT NOT NULL, email TEXT, active NUMERIC NOT NULL DEFAULT 1)`).Error; err != nil {
		t.Fatalf("create credentials: %v", err)
	}
	rows := []struct{ id, agent, provider, sub, email string }{
		{"cred-google", "agent-dana", "google", "108234917650023841257", "Dana.Whitfield@HarborLegal.example"},
		{"cred-password", "agent-dana", "password", "dana.whitfield@harborlegal.example", " dana.whitfield@harborlegal.example "},
		{"cred-marcus", "agent-marcus", "apple", "000917.3b6e", "marcus.okafor@harborlegal.example"},
		{"cred-no-email", "agent-quiet", "apple", "000918.4c7f", ""},
	}
	for _, r := range rows {
		if err := db.Exec(`INSERT INTO credentials (id, agent_id, provider, provider_user_id, email) VALUES (?, ?, ?, ?, ?)`,
			r.id, r.agent, r.provider, r.sub, r.email).Error; err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}

	q := ProvideCredentialEmailQuery(db)
	cases := map[string][]string{
		"dana.whitfield@harborlegal.example":   {"agent-dana"},
		"  DANA.WHITFIELD@harborlegal.EXAMPLE": {"agent-dana"},
		"marcus.okafor@harborlegal.example":    {"agent-marcus"},
		"nobody@harborlegal.example":           nil,
		"":                                     nil,
	}
	for email, want := range cases {
		got, err := q.AgentIDsByEmail(ctx, email)
		if err != nil {
			t.Fatalf("AgentIDsByEmail(%q): %v", email, err)
		}
		sort.Strings(got)
		if len(got) != len(want) {
			t.Fatalf("AgentIDsByEmail(%q) = %v, want %v", email, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("AgentIDsByEmail(%q) = %v, want %v", email, got, want)
			}
		}
	}
}
