package agents

import "testing"

func TestParseFactCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"plain array", `[{"statement":"a"},{"statement":"b","confidence":0.7}]`, 2, false},
		{"fenced json", "```json\n[{\"statement\":\"a\"}]\n```", 1, false},
		{"bare fence", "```\n[]\n```", 0, false},
		{"empty reply", "  \n", 0, false},
		{"empty array", `[]`, 0, false},
		{"invalid json", `{"statement":`, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseFactCandidates(tc.in)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if len(got) != tc.want {
				t.Errorf("candidates = %d, want %d", len(got), tc.want)
			}
		})
	}
}

func TestParseFactCandidates_FieldMapping(t *testing.T) {
	t.Parallel()

	got, err := ParseFactCandidates(
		`[{"statement":"s","about":"urn:x","confidence":0.9,"supersedesId":"urn:fact:1"}]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := got[0]
	if c.Statement != "s" || c.About != "urn:x" || c.Confidence != 0.9 || c.SupersedesID != "urn:fact:1" {
		t.Errorf("candidate = %+v", c)
	}
}
