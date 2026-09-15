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
	"sync"
	"testing"
	"time"

	authmodels "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/wepala/weos/v3/domain/repositories"
)

func newDirectoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), gormConfig())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&authmodels.AgentModel{}, &authmodels.CredentialModel{}, &authmodels.AccountMemberModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedMembership(t *testing.T, db *gorm.DB, accountID, agentID, roleID string) {
	t.Helper()
	if err := db.Create(authmodels.AccountMemberModelFrom(accountID, agentID, roleID)).Error; err != nil {
		t.Fatalf("seed membership %s in %s: %v", agentID, accountID, err)
	}
}

// seedPerson stores a person's record and one credential per email, the
// earlier email first.
func seedPerson(t *testing.T, db *gorm.DB, agentID, name, status string, emails ...string) {
	t.Helper()
	now := time.Now()
	if err := db.Create(&authmodels.AgentModel{
		ID: agentID, Name: name, AgentType: "foaf:Person", Status: status, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed person %s: %v", agentID, err)
	}
	for i, email := range emails {
		if err := db.Create(&authmodels.CredentialModel{
			ID: fmt.Sprintf("%s-cred-%d", agentID, i), AgentID: agentID,
			Provider: fmt.Sprintf("provider-%d", i), ProviderUserID: agentID,
			Email: email, Active: true, CreatedAt: now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatalf("seed credential of %s: %v", agentID, err)
		}
	}
}

// wm-govvg. The directory lists the members of the one account asked about,
// each with the role they hold there and the person's own record, and never a
// member of another account.
func TestAccountMemberDirectoryListsOneAccountWithEachPersonsRecord(t *testing.T) {
	ctx := context.Background()
	db := newDirectoryTestDB(t)
	seedPerson(t, db, "agent-ops", "Harbor Operations", "active", "", "ops@harborlegal.example")
	seedPerson(t, db, "agent-clerk", "Lantern Clerk", "suspended", "clerk@lanternhomes.example")
	seedPerson(t, db, "agent-counsel", "Cedar Counsel", "active", "counsel@cedarrealty.example")
	seedMembership(t, db, "acct-harbor", "agent-ops", "owner")
	seedMembership(t, db, "acct-harbor", "agent-clerk", "member")
	seedMembership(t, db, "acct-harbor", "agent-gone", "member")
	seedMembership(t, db, "acct-cedar", "agent-counsel", "owner")
	seedMembership(t, db, "acct-cedar", "agent-clerk", "admin")

	directory := ProvideAccountMemberDirectory(db)
	page, err := directory.ListMembers(ctx, "acct-harbor", "", 0)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	want := []repositories.AccountMember{
		{AgentID: "agent-clerk", RoleID: "member", HasRecord: true, Name: "Lantern Clerk", Status: "suspended", Email: "clerk@lanternhomes.example"},
		{AgentID: "agent-gone", RoleID: "member"},
		{AgentID: "agent-ops", RoleID: "owner", HasRecord: true, Name: "Harbor Operations", Status: "active", Email: "ops@harborlegal.example"},
	}
	if len(page.Members) != len(want) {
		t.Fatalf("ListMembers(acct-harbor) = %+v, want %+v", page.Members, want)
	}
	for i := range want {
		if page.Members[i] != want[i] {
			t.Errorf("member %d = %+v, want %+v", i, page.Members[i], want[i])
		}
	}
	if page.HasMore || page.Cursor != "" {
		t.Errorf("a whole account in one page reported has_more=%v cursor=%q", page.HasMore, page.Cursor)
	}

	for _, account := range []string{"", "acct-nobody"} {
		page, err := directory.ListMembers(ctx, account, "", 0)
		if err != nil {
			t.Fatalf("ListMembers(%q): %v", account, err)
		}
		if len(page.Members) != 0 || page.HasMore {
			t.Fatalf("ListMembers(%q) = %+v, want an empty last page", account, page)
		}
	}
}

// wm-g7284. A client walks a large account a page at a time, each page
// starting after the agent ID the previous one ended at.
func TestAccountMemberDirectoryPagesByAgentID(t *testing.T) {
	ctx := context.Background()
	db := newDirectoryTestDB(t)
	for _, id := range []string{"agent-e", "agent-a", "agent-d", "agent-b", "agent-c"} {
		seedPerson(t, db, id, "Harbor Paralegal "+id, "active", id+"@harborlegal.example")
		seedMembership(t, db, "acct-harbor", id, "member")
	}
	directory := ProvideAccountMemberDirectory(db)

	var seen []string
	cursor := ""
	for pageNo := 1; ; pageNo++ {
		page, err := directory.ListMembers(ctx, "acct-harbor", cursor, 2)
		if err != nil {
			t.Fatalf("page %d: %v", pageNo, err)
		}
		if len(page.Members) > 2 {
			t.Fatalf("page %d held %d members, want at most 2", pageNo, len(page.Members))
		}
		for _, m := range page.Members {
			seen = append(seen, m.AgentID)
		}
		if !page.HasMore {
			if page.Cursor != "" {
				t.Errorf("the last page carried cursor %q", page.Cursor)
			}
			break
		}
		if page.Cursor != page.Members[len(page.Members)-1].AgentID {
			t.Errorf("page %d cursor %q, want the last agent on it", pageNo, page.Cursor)
		}
		cursor = page.Cursor
		if pageNo > 5 {
			t.Fatal("the pages never ended")
		}
	}
	want := []string{"agent-a", "agent-b", "agent-c", "agent-d", "agent-e"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Errorf("walking the pages saw %v, want %v", seen, want)
	}
}

// wm-g7284. No request reads an unbounded account: no limit means the default
// page size, and a larger limit is cut to the maximum.
func TestAccountMemberDirectoryCapsThePageSize(t *testing.T) {
	ctx := context.Background()
	db := newDirectoryTestDB(t)
	rows := make([]*authmodels.AccountMemberModel, 0, repositories.MaxMemberPageSize+1)
	for i := 0; i <= repositories.MaxMemberPageSize; i++ {
		rows = append(rows, authmodels.AccountMemberModelFrom("acct-harbor", fmt.Sprintf("agent-%04d", i), "member"))
	}
	if err := db.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatalf("seed memberships: %v", err)
	}
	directory := ProvideAccountMemberDirectory(db)

	for _, tc := range []struct {
		limit int
		want  int
	}{
		{0, repositories.DefaultMemberPageSize},
		{-3, repositories.DefaultMemberPageSize},
		{repositories.MaxMemberPageSize * 20, repositories.MaxMemberPageSize},
	} {
		page, err := directory.ListMembers(ctx, "acct-harbor", "", tc.limit)
		if err != nil {
			t.Fatalf("limit %d: %v", tc.limit, err)
		}
		if len(page.Members) != tc.want || !page.HasMore {
			t.Errorf("limit %d returned %d members (has_more=%v), want %d and more to come",
				tc.limit, len(page.Members), page.HasMore, tc.want)
		}
	}
}

// statementCounter is a gorm logger that counts the statements run through it.
type statementCounter struct {
	mu sync.Mutex
	n  int
}

func (s *statementCounter) LogMode(gormlogger.LogLevel) gormlogger.Interface { return s }
func (s *statementCounter) Info(context.Context, string, ...any)             {}
func (s *statementCounter) Warn(context.Context, string, ...any)             {}
func (s *statementCounter) Error(context.Context, string, ...any)            {}
func (s *statementCounter) Trace(context.Context, time.Time, func() (string, int64), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
}

func (s *statementCounter) take() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.n
	s.n = 0
	return n
}

// wm-g7284. A page costs the same number of statements whether it holds three
// people or thirty: the records are loaded for the page, not per member.
func TestAccountMemberDirectoryReadsAPageInAFixedNumberOfStatements(t *testing.T) {
	ctx := context.Background()
	db := newDirectoryTestDB(t)
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("cedar-%02d", i)
		seedPerson(t, db, id, "Cedar Agent "+id, "active", id+"@cedarrealty.example")
		seedMembership(t, db, "acct-cedar", id, "member")
	}
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("harbor-%02d", i)
		seedPerson(t, db, id, "Harbor Agent "+id, "active", id+"@harborlegal.example", "second-"+id+"@harborlegal.example")
		seedMembership(t, db, "acct-harbor", id, "member")
	}
	counter := &statementCounter{}
	directory := ProvideAccountMemberDirectory(db.Session(&gorm.Session{Logger: counter}))

	statements := map[string]int{}
	for _, account := range []string{"acct-cedar", "acct-harbor"} {
		counter.take()
		page, err := directory.ListMembers(ctx, account, "", 0)
		if err != nil {
			t.Fatalf("ListMembers(%s): %v", account, err)
		}
		for _, m := range page.Members {
			if m.Email == "" || m.Name == "" {
				t.Errorf("member %s of %s came back without their record: %+v", m.AgentID, account, m)
			}
		}
		statements[account] = counter.take()
	}
	if statements["acct-cedar"] != statements["acct-harbor"] || statements["acct-harbor"] > 2 {
		t.Errorf("a page of 3 members cost %d statements and a page of 30 cost %d; want the same, at most 2",
			statements["acct-cedar"], statements["acct-harbor"])
	}
}
