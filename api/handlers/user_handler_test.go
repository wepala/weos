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

package handlers_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
)

// wm-ii1hz. A users handler built without its member directory would answer
// every list with a nil-pointer panic. It must refuse to be built at all, and
// say which dependency is missing.
func TestNewUserHandlerRefusesToBeBuiltWithoutAMemberDirectory(t *testing.T) {
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handlers.NewUserHandler(handlers.UserHandlerConfig{Logger: nopLogger{}})
	}()
	if recovered == nil {
		t.Fatal("NewUserHandler built a handler with no Members; want it to stop at construction")
	}
	if said := fmt.Sprint(recovered); !strings.Contains(said, "Members") {
		t.Errorf("the construction failure says %q; want it to name the missing Members dependency", said)
	}
}
