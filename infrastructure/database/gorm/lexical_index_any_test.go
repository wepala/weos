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

package gorm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
)

// The MatchAny search (story wm-u1xp4w), run against the real SQL of both
// engines. Each case seeds its own ids, so the cases share one index.

func anySearch(t *testing.T, idx repositories.LexicalIndex, q repositories.LexicalQuery,
	scope *repositories.VisibilityScope,
) []repositories.LexicalHit {
	t.Helper()
	q.Match = repositories.MatchAny
	if q.Limit == 0 {
		q.Limit = 20
	}
	hits, err := idx.Search(context.Background(), q, scope)
	if err != nil {
		t.Fatalf("Search(%q, any): %v", q.Text, err)
	}
	return hits
}

func orderedIDs(hits []repositories.LexicalHit) []string {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	return ids
}

func runAnyMatchCases(t *testing.T, idx repositories.LexicalIndex) {
	t.Helper()
	ctx := context.Background()
	none := repositories.LexicalOwner{}

	// Ranking: coverage, then relevance, then id.
	mustIndex(t, idx, "urn:task:rank-4", "task", "Chase the Harbor invoice. Harbor Legal invoice for the June lease", none)
	mustIndex(t, idx, "urn:task:rank-2", "task", "Harbor Legal onboarding. Welcome pack for the new Legal client", none)
	mustIndex(t, idx, "urn:task:rank-1", "task", "Lease renewal for the Wepala studio", none)
	mustIndex(t, idx, "urn:task:rank-rep", "task",
		"Lease lease lease lease lease lease lease lease lease lease, lease lease", none)
	hits := anySearch(t, idx, repositories.LexicalQuery{Text: "the invoice for the Harbor Legal lease"}, nil)
	got := strings.Join(orderedIDs(hits), ",")
	if !strings.HasPrefix(got, "urn:task:rank-4,urn:task:rank-2,") {
		t.Errorf("order = %s, want the four-word then the two-word resource first", got)
	}
	if !strings.Contains(got, "urn:task:rank-1") || !strings.Contains(got, "urn:task:rank-rep") {
		t.Errorf("order = %s, want the one-word resources returned too", got)
	}
	for _, h := range hits {
		if h.Snippet == "" || h.TypeSlug != "task" {
			t.Errorf("hit %+v, want a snippet and the type", h)
		}
	}

	// Equal coverage and equal relevance fall back to the id.
	mustIndex(t, idx, "urn:note:tie-b", "note", "zanzibar quokka", none)
	mustIndex(t, idx, "urn:note:tie-a", "note", "zanzibar quokka", none)
	hits = anySearch(t, idx, repositories.LexicalQuery{Text: "zanzibar quokka"}, nil)
	if got := strings.Join(orderedIDs(hits), ","); got != "urn:note:tie-a,urn:note:tie-b" {
		t.Errorf("tie order = %s, want by id", got)
	}

	// Nothing to match: no hits, no error, never everything.
	for _, q := range []string{"what is it about", "can't", "I don't, you're, we'd", "& | ! :* <-> ( )", "", "   "} {
		if hits := anySearch(t, idx, repositories.LexicalQuery{Text: q}, nil); len(hits) != 0 {
			t.Errorf("Search(%q, any) = %v, want no hits", q, orderedIDs(hits))
		}
	}

	// Operator syntax is matched as words.
	for _, q := range []string{"Harbor & onboarding", "Harbor | onboarding", "onboarding:*", "!onboarding",
		`"onboarding`, "onboarding NEAR", "onboarding OR", "(onboarding)", "onboarding <-> pack", `\onboarding`} {
		hits := anySearch(t, idx, repositories.LexicalQuery{Text: q}, nil)
		if len(hits) == 0 || hits[0].ID != "urn:task:rank-2" {
			t.Errorf("Search(%q, any) = %v, want the onboarding resource first", q, orderedIDs(hits))
		}
	}

	// A word with punctuation inside it matches the resource that holds it.
	mustIndex(t, idx, "urn:task:punct", "task",
		"Follow-up e-mail to Dana's office at dana@example.com via docs.example.org", none)
	for _, q := range []string{"follow-up", "e-mail", "Dana's", "dana@example.com", "docs.example.org"} {
		hits := anySearch(t, idx, repositories.LexicalQuery{Text: q}, nil)
		if len(hits) != 1 || hits[0].ID != "urn:task:punct" {
			t.Errorf("Search(%q, any) = %v, want the punctuated resource alone", q, orderedIDs(hits))
		}
	}

	// MinMatch is a floor, clamped at the query's content-word count.
	hits = anySearch(t, idx, repositories.LexicalQuery{Text: "Harbor Legal invoice", MinMatch: 2}, nil)
	if got := strings.Join(hitIDs(hits), ","); got != "urn:task:rank-2,urn:task:rank-4" {
		t.Errorf("MinMatch 2 = %s, want the resources holding two or more words", got)
	}
	hits = anySearch(t, idx, repositories.LexicalQuery{Text: "Harbor invoice", MinMatch: 9}, nil)
	if got := strings.Join(orderedIDs(hits), ","); got != "urn:task:rank-4" {
		t.Errorf("MinMatch 9 over two words = %s, want the resource holding both", got)
	}

	// The type filter narrows the match.
	hits = anySearch(t, idx, repositories.LexicalQuery{Text: "zanzibar Harbor", TypeSlugs: []string{"note"}}, nil)
	if got := strings.Join(hitIDs(hits), ","); got != "urn:note:tie-a,urn:note:tie-b" {
		t.Errorf("narrowed to note = %s, want the notes alone", got)
	}

	// The scope and the type filter apply before the limit: another
	// account's resources holding every word cannot spend it.
	harbor := repositories.LexicalOwner{CreatedBy: "agent-harbor", AccountID: "acct-harbor"}
	cedar := repositories.LexicalOwner{CreatedBy: "agent-cedar", AccountID: "acct-cedar"}
	for i := 0; i < 30; i++ {
		mustIndex(t, idx, fmt.Sprintf("urn:task:crowd-%02d", i), "task", "dunmore settlement balance", harbor)
	}
	mustIndex(t, idx, "urn:task:cedar", "task", "Dunmore Road showing", cedar)
	hits = anySearch(t, idx, repositories.LexicalQuery{Text: "the Dunmore settlement balance", Limit: 5},
		scopeOf(cedar))
	if got := strings.Join(orderedIDs(hits), ","); got != "urn:task:cedar" {
		t.Errorf("cedar's search = %s, want its own resource alone", got)
	}
	hits = anySearch(t, idx, repositories.LexicalQuery{Text: "the Dunmore settlement balance", Limit: 5}, nil)
	if len(hits) != 5 || strings.Contains(strings.Join(orderedIDs(hits), ","), "urn:task:cedar") {
		t.Errorf("system search = %v, want five three-word resources ahead of the one-word one", orderedIDs(hits))
	}

	// The all mode is unchanged: every word, stop words included.
	all, err := idx.Search(ctx, repositories.LexicalQuery{Text: "Harbor Legal invoice", Limit: 20}, nil)
	if err != nil {
		t.Fatalf("Search(all): %v", err)
	}
	if got := strings.Join(hitIDs(all), ","); got != "urn:task:rank-4" {
		t.Errorf("all mode = %s, want only the resource holding every word", got)
	}
	all, err = idx.Search(ctx, repositories.LexicalQuery{Text: "what is it about", Limit: 20}, nil)
	if err != nil || len(all) != 0 {
		t.Errorf("all mode over stop words = %v, %v, want no hits and no error", orderedIDs(all), err)
	}
}

func TestLexicalIndex_AnyMatch_SQLite(t *testing.T) {
	t.Parallel()
	runAnyMatchCases(t, ProvideLexicalIndex(sqliteScopeDB(t), config.Default(), &testLogger{}))
}

func TestLexicalIndex_AnyMatch_Postgres(t *testing.T) {
	t.Parallel()
	db, cfg := postgresScopeDB(t)
	runAnyMatchCases(t, ProvideLexicalIndex(db, cfg, &testLogger{}))
}

func TestFts5AnyMatch(t *testing.T) {
	t.Parallel()
	if got := fts5AnyMatch([]string{"harbor", "follow-up"}); got != `"harbor" OR "follow-up"` {
		t.Errorf("fts5AnyMatch = %s", got)
	}
}
