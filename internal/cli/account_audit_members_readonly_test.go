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

package cli

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authmodels "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/models"
	esinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"github.com/glebarez/sqlite"
	gormlib "gorm.io/gorm"
)

// seedRollbackJournalStore writes a SQLite file in the default rollback journal
// mode, holding the tables the audit reads and one membership, and closes it.
func seedRollbackJournalStore(t *testing.T, dbFile string) {
	t.Helper()
	db, err := gormlib.Open(sqlite.Open(dbFile), &gormlib.Config{})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := db.AutoMigrate(
		&authmodels.AccountModel{}, &authmodels.AgentModel{}, &authmodels.CredentialModel{},
		&authmodels.AccountMemberModel{}, &authmodels.InviteModel{}, &esinfra.GormEventModel{},
	); err != nil {
		t.Fatalf("migrate the store: %v", err)
	}
	if err := db.Create(&authmodels.AccountModel{
		ID: "acct-1harbor", Name: "Harbor Legal", AccountType: "personal", Active: true, CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed the account: %v", err)
	}
	if err := db.Create(&authmodels.AccountMemberModel{
		AccountID: "acct-1harbor", AgentID: "agent-ops", RoleID: authentities.RoleOwner, CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed the membership: %v", err)
	}
	var mode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil || !strings.EqualFold(mode, "delete") {
		t.Fatalf("the seeded store is in journal mode %q (%v); want delete", mode, err)
	}
}

func fileDigest(t *testing.T, name string) [sha256.Size]byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return sha256.Sum256(data)
}

// wm-govvg. The audit only reads, and that includes the database file itself.
// The server's SQLite pragmas switch a file into WAL journal mode, which
// rewrites the file's header, and a read-only DSN refuses that switch. The
// command opens the file read-only, with none of them, so a copy or backup in
// rollback journal mode is byte-for-byte unchanged, and a mode=ro DSN works.
func TestAccountAuditMembersLeavesTheDatabaseFileUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name string
		dsn  func(dbFile string) string
	}{
		{"a plain path", func(dbFile string) string { return dbFile }},
		{"a read-only file URI", func(dbFile string) string { return "file:" + dbFile + "?mode=ro" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbFile := filepath.Join(t.TempDir(), "weos.db")
			seedRollbackJournalStore(t, dbFile)
			before := fileDigest(t, dbFile)

			var out, errOut bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&errOut)
			rootCmd.SetArgs([]string{"account", "audit-members", "--database-dsn", tc.dsn(dbFile)})
			t.Cleanup(func() {
				rootCmd.SetArgs(nil)
				rootCmd.SetOut(nil)
				rootCmd.SetErr(nil)
				databaseDSN = ""
				cfg = nil
			})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("account audit-members failed: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errOut.String())
			}
			if !strings.Contains(out.String(), "agent-ops") {
				t.Errorf("the audit did not list the membership it was given:\n%s", out.String())
			}

			if after := fileDigest(t, dbFile); after != before {
				t.Error("the audit changed the database file")
			}
			for _, sidecar := range []string{dbFile + "-wal", dbFile + "-shm"} {
				if _, err := os.Stat(sidecar); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("the audit left %s beside the database (stat: %v)", filepath.Base(sidecar), err)
				}
			}
		})
	}
}
