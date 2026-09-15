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

package repositories_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

// v3MemberQuery is an AccountMemberQuery written the way an embedder wrote one
// against the published v3 interface: the two methods it has always had, and
// nothing else (wm-9wslp). If this file stops compiling, the interface grew and
// every external implementation breaks on the upgrade.
type v3MemberQuery struct{}

func (v3MemberQuery) ListMemberIDsByRole(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (v3MemberQuery) CountMembers(context.Context, string) (int, error) { return 0, nil }

var _ repositories.AccountMemberQuery = v3MemberQuery{}

// The users routes list an account's people through their own port, so the
// published AccountMemberQuery keeps exactly the methods it shipped with.
func TestAccountMemberQueryKeepsTheMethodsItShippedWith(t *testing.T) {
	iface := reflect.TypeOf((*repositories.AccountMemberQuery)(nil)).Elem()
	want := map[string]bool{"ListMemberIDsByRole": true, "CountMembers": true}
	if iface.NumMethod() != len(want) {
		t.Errorf("AccountMemberQuery has %d methods, want %d", iface.NumMethod(), len(want))
	}
	for i := 0; i < iface.NumMethod(); i++ {
		if name := iface.Method(i).Name; !want[name] {
			t.Errorf("AccountMemberQuery gained %s; add it to a separate interface so existing implementations keep compiling", name)
		}
	}
}
