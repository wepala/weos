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

package s3_test

import (
	"context"
	"testing"

	"github.com/wepala/weos/v3/domain/services"
	weoss3 "github.com/wepala/weos/v3/infrastructure/storage/s3"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// The account adoption (wm-c7xehr) refuses a source account with stored
// files; the count walks every page of the listing under the account prefix.
func TestCountAccountFiles_CountsEveryPageUnderThePrefix(t *testing.T) {
	fake := &fakeListing{pageSize: 1000}
	fake.keys = append(fake.keys, accountKeys("acct_1", 2500)...)
	fake.keys = append(fake.keys, accountKeys("acct_10", 3)...)
	fake.keys = append(fake.keys, "uploads/legacy-flat.jpg")
	svc := weoss3.New(fake.client(), testBucket, testRegion, nopLogger{})

	counter, ok := svc.(services.AccountFileCounter)
	if !ok {
		t.Fatalf("%T does not count an account's files", svc)
	}
	got, err := counter.CountAccountFiles(context.Background(), "acct_1")
	if err != nil {
		t.Fatalf("CountAccountFiles() error: %v", err)
	}
	if got != 2500 {
		t.Errorf("CountAccountFiles(acct_1) = %d, want 2500", got)
	}
	for _, list := range fake.lists {
		if aws.ToString(list.Prefix) != "accounts/acct_1/" {
			t.Errorf("listed with prefix %q, want accounts/acct_1/", aws.ToString(list.Prefix))
		}
	}
	if len(fake.deletes) != 0 {
		t.Errorf("a count deleted %d batch(es)", len(fake.deletes))
	}
}
