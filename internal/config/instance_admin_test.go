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

package config

import "testing"

// wm-gu3pm. INSTANCE_ADMIN_ACCOUNT names the account whose owners and admins
// may change the resource types every account shares. Spaces around the id
// are not part of it; unset, and set to spaces only, both leave it empty.
func TestLoadFromEnvironment_InstanceAdminAccount(t *testing.T) {
	cases := map[string]struct {
		env  string
		want string
	}{
		"an account id":           {env: "2Zq7mQb0Xn9YpR4sT1vW8kLcE3dA", want: "2Zq7mQb0Xn9YpR4sT1vW8kLcE3dA"},
		"an id with spaces round": {env: "  2Zq7mQb0Xn9YpR4sT1vW8kLcE3dA \n", want: "2Zq7mQb0Xn9YpR4sT1vW8kLcE3dA"},
		"spaces only":             {env: "   ", want: ""},
		"empty":                   {env: "", want: ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("INSTANCE_ADMIN_ACCOUNT", c.env)
			cfg := Default()
			cfg.LoadFromEnvironment()
			if cfg.InstanceAdminAccountID != c.want {
				t.Fatalf("INSTANCE_ADMIN_ACCOUNT=%q loaded as %q, want %q", c.env, cfg.InstanceAdminAccountID, c.want)
			}
		})
	}
}

func TestDefault_HasNoInstanceAdminAccount(t *testing.T) {
	if got := Default().InstanceAdminAccountID; got != "" {
		t.Fatalf("the default configuration names instance admin account %q, want none", got)
	}
}
