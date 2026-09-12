package gorm

import (
	"context"
	"reflect"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
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
	rows := []struct {
		id, agent, provider, sub, email string
		active                          bool
	}{
		{"cred-google", "agent-dana", "google", "108234917650023841257", "Dana.Whitfield@HarborLegal.example", true},
		{"cred-password", "agent-dana", "password", "dana.whitfield@harborlegal.example", " dana.whitfield@harborlegal.example ", true},
		{"cred-marcus", "agent-marcus", "apple", "000917.3b6e", "marcus.okafor@harborlegal.example", true},
		{"cred-marcus-off", "agent-marcus-old", "google", "117590246813570924368", "Marcus.Okafor@harborlegal.example", false},
		{"cred-no-email", "agent-quiet", "apple", "000918.4c7f", "", true},
		{"cred-elise", "agent-elise", "google", "108234917650023841999", "ÉLISE.MARTIN@harborlegal.example", true},
		{"cred-karl", "agent-karl", "apple", "000919.5d8a", "karl.berg@harborlegal.example", true},
		{"cred-pat", "agent-pat", "google", "108234917650023842000", "\tpat.lee@harborlegal.example", true},
	}
	for _, r := range rows {
		if err := db.Exec(`INSERT INTO credentials (id, agent_id, provider, provider_user_id, email, active) VALUES (?, ?, ?, ?, ?, ?)`,
			r.id, r.agent, r.provider, r.sub, r.email, r.active).Error; err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}

	q := ProvideCredentialEmailQuery(db)
	dana := []repositories.CredentialEmailMatch{
		{AgentID: "agent-dana", Provider: "google", Active: true},
		{AgentID: "agent-dana", Provider: "password", Active: true},
	}
	elise := []repositories.CredentialEmailMatch{{AgentID: "agent-elise", Provider: "google", Active: true}}
	karl := []repositories.CredentialEmailMatch{{AgentID: "agent-karl", Provider: "apple", Active: true}}
	cases := map[string][]repositories.CredentialEmailMatch{
		"dana.whitfield@harborlegal.example":   dana,
		"  DANA.WHITFIELD@harborlegal.EXAMPLE": dana,
		// An inactive credential is returned, and says so: whether it counts
		// is the caller's decision.
		"marcus.okafor@harborlegal.example": {
			{AgentID: "agent-marcus", Provider: "apple", Active: true},
			{AgentID: "agent-marcus-old", Provider: "google", Active: false},
		},
		"nobody@harborlegal.example": nil,
		"":                           nil,
		// ASCII capitals fold; a non-ASCII capital is kept, in Go and in SQL.
		"Élise.Martin@harborlegal.example": elise,
		"élise.martin@harborlegal.example": nil,
		// The Kelvin sign lower-cases to k under a Unicode fold; not here.
		"KARL.BERG@harborlegal.example": karl,
		"Karl.berg@harborlegal.example": nil,
		// Only spaces are trimmed, so a stored tab is part of the address.
		"pat.lee@harborlegal.example": nil,
	}
	for email, want := range cases {
		got, err := q.CredentialsByEmail(ctx, email)
		if err != nil {
			t.Fatalf("CredentialsByEmail(%q): %v", email, err)
		}
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("CredentialsByEmail(%q) = %+v, want %+v", email, got, want)
		}
	}
}
