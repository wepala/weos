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
	"strings"
	"testing"

	"github.com/wepala/weos/v3/application"
)

func TestPrintAssignOwnerReport_DryRunListsCountsPerType(t *testing.T) {
	var out bytes.Buffer
	printAssignOwnerReport(&out, application.AssignOwnerReport{
		AccountID: "acct-1", CreatedBy: "agent-1", DryRun: true,
		Types: map[string]application.AssignOwnerTypeCount{
			"statement":   {Ownerless: 15},
			"transaction": {Ownerless: 412},
		},
	})
	text := out.String()
	for _, want := range []string{
		"DRY RUN — nothing was written",
		"account acct-1, resources created by agent-1",
		"statement                15 ownerless, would assign 15",
		"transaction              412 ownerless, would assign 412",
		"total: 427 ownerless resource(s) would be assigned",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "statement") > strings.Index(text, "transaction") {
		t.Errorf("types are not in name order:\n%s", text)
	}
}

func TestPrintAssignOwnerReport_RealRunSaysWhatLanded(t *testing.T) {
	var out bytes.Buffer
	printAssignOwnerReport(&out, application.AssignOwnerReport{
		AccountID: "acct-1", CreatedBy: "agent-1",
		Types: map[string]application.AssignOwnerTypeCount{
			"statement": {Ownerless: 15, Assigned: 14, Skipped: 1},
		},
	})
	text := out.String()
	for _, want := range []string{
		"owners assigned through the event stream",
		"statement                15 ownerless, assigned 14, skipped 1 that gained an owner during the run",
		"total: 14 of 15 ownerless resource(s) assigned",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "DRY RUN") {
		t.Errorf("a real run reads as a dry run:\n%s", text)
	}
}

func TestPrintAssignOwnerReport_NothingToAssign(t *testing.T) {
	var out bytes.Buffer
	printAssignOwnerReport(&out, application.AssignOwnerReport{AccountID: "a", CreatedBy: "b", DryRun: true,
		Types: map[string]application.AssignOwnerTypeCount{}})
	if !strings.Contains(out.String(), "nothing to assign") {
		t.Errorf("report lacks the nothing-to-assign line:\n%s", out.String())
	}
}

func TestMaintenanceAssignOwner_IsRegisteredAndNeedsBothIDs(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"maintenance", "assign-owner"})
	if err != nil || found != maintenanceAssignOwnerCmd {
		t.Fatalf("weos maintenance assign-owner is not registered: %v", err)
	}
	for _, flag := range []string{"account", "created-by", "dry-run"} {
		if maintenanceAssignOwnerCmd.Flags().Lookup(flag) == nil {
			t.Errorf("flag --%s is missing", flag)
		}
	}
	// With neither id the command refuses before it opens any store.
	if err := runMaintenanceAssignOwner(maintenanceAssignOwnerCmd, nil); err == nil ||
		!strings.Contains(err.Error(), "--account and --created-by") {
		t.Fatalf("err = %v, want a refusal naming both flags", err)
	}
}
