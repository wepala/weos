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

package gcs_test

import (
	"context"
	"testing"

	"github.com/wepala/weos/v3/domain/services"
	"github.com/wepala/weos/v3/infrastructure/storage/gcs"
)

// The account adoption (wm-c7xehr) refuses a source account with stored
// files; the count follows every page of the listing under the account
// prefix and deletes nothing.
func TestCountAccountFiles_CountsEveryPageUnderThePrefix(t *testing.T) {
	fake := &fakeBucket{t: t, pageSize: 2, objects: []string{
		"accounts/acct_1/uploads/a-one.jpg",
		"accounts/acct_1/uploads/b-two.jpg",
		"accounts/acct_1/uploads/c-three.jpg",
		"accounts/acct_10/uploads/f-other.jpg",
		"uploads/h-legacy-flat.jpg",
	}}
	svc := gcs.New(newClient(t, fake), testBucket, nopLogger{})

	counter, ok := svc.(services.AccountFileCounter)
	if !ok {
		t.Fatalf("%T does not count an account's files", svc)
	}
	got, err := counter.CountAccountFiles(context.Background(), "acct_1")
	if err != nil {
		t.Fatalf("CountAccountFiles() error: %v", err)
	}
	if got != 3 {
		t.Errorf("CountAccountFiles(acct_1) = %d, want 3", got)
	}
	for _, prefix := range fake.lists {
		if prefix != "accounts/acct_1/" {
			t.Errorf("listed with prefix %q, want accounts/acct_1/", prefix)
		}
	}
	if len(fake.deleted) != 0 {
		t.Errorf("a count deleted %v", fake.deleted)
	}
}
