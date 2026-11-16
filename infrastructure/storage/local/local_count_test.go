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

package local_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/infrastructure/storage/local"
)

// The account adoption (wm-c7xehr) refuses a source account with stored
// files, so the backend must count what is under one account's folder and
// nothing of any other.
func TestCountAccountFiles_CountsOnlyThatAccount(t *testing.T) {
	dir := t.TempDir()
	svc := local.New(dir, "/api/uploads/files", nopLogger{})
	ctx := context.Background()
	for account, n := range map[string]int{"acct_1": 2, "acct_10": 3, "acct_2": 1} {
		for range n {
			params := services.UploadParams{Filename: "photo.jpg", ContentType: "image/jpeg", AccountID: account}
			if _, err := svc.Upload(ctx, params, strings.NewReader(account)); err != nil {
				t.Fatalf("Upload(%s) error: %v", account, err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy-flat.jpg"), []byte("flat"), 0o600); err != nil {
		t.Fatal(err)
	}

	counter, ok := svc.(services.AccountFileCounter)
	if !ok {
		t.Fatalf("%T does not count an account's files", svc)
	}
	got, err := counter.CountAccountFiles(ctx, "acct_1")
	if err != nil {
		t.Fatalf("CountAccountFiles() error: %v", err)
	}
	if got != 2 {
		t.Errorf("CountAccountFiles(acct_1) = %d, want 2 (acct_10's and the flat file are not acct_1's)", got)
	}
}

func TestCountAccountFiles_MissingFolderIsZero(t *testing.T) {
	svc := local.New(t.TempDir(), "/api/uploads/files", nopLogger{})
	got, err := svc.(services.AccountFileCounter).CountAccountFiles(context.Background(), "never-uploaded")
	if err != nil || got != 0 {
		t.Fatalf("CountAccountFiles() on a missing folder = %d, %v; want 0, nil", got, err)
	}
}

func TestCountAccountFiles_RefusesUnsafeAccount(t *testing.T) {
	svc := local.New(t.TempDir(), "/api/uploads/files", nopLogger{})
	if _, err := svc.(services.AccountFileCounter).CountAccountFiles(context.Background(), "../etc"); err == nil {
		t.Fatal("CountAccountFiles() accepted an account id that climbs out of the folder")
	}
}
