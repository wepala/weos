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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
)

func TestPrintAssignOwnerReport_DryRunListsCountsPerType(t *testing.T) {
	var out bytes.Buffer
	r := application.AssignOwnerReport{
		AccountID: "acct-1", CreatedBy: "agent-1", DryRun: true,
		Types: map[string]application.AssignOwnerTypeCount{
			"statement":   {Ownerless: 15},
			"transaction": {Ownerless: 412},
		},
	}
	printAssignOwnerTarget(&out, r)
	printAssignOwnerReport(&out, r)
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
	r := application.AssignOwnerReport{
		AccountID: "acct-1", CreatedBy: "agent-1",
		Types: map[string]application.AssignOwnerTypeCount{
			"statement": {Ownerless: 15, Assigned: 14, Skipped: 1},
		},
	}
	printAssignOwnerTarget(&out, r)
	printAssignOwnerReport(&out, r)
	text := out.String()
	for _, want := range []string{
		"assigning owners through the event stream",
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
	for _, flag := range []string{"account", "created-by", "dry-run", "created-from", "created-to", "confirm-account"} {
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

func TestPrintAssignOwnerReport_ListsWhatNeedsAReproject(t *testing.T) {
	var out bytes.Buffer
	printAssignOwnerReport(&out, application.AssignOwnerReport{
		AccountID: "acct-1", CreatedBy: "agent-1",
		Types: map[string]application.AssignOwnerTypeCount{
			"statement": {Ownerless: 3, Assigned: 2, NeedsReproject: 1},
		},
		NeedsReproject: []application.AssignOwnerSkip{{
			ID: "urn:statement:1", TypeSlug: "statement",
			Reason: "the record is at sequence 2 and the event store at 3",
		}},
	})
	text := out.String()
	for _, want := range []string{
		"statement                3 ownerless, assigned 2, 1 left for a reproject",
		"urn:statement:1 (statement): the record is at sequence 2 and the event store at 3",
		"weos worker reproject",
		"total: 2 of 3 ownerless resource(s) assigned, 1 left for a reproject",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
}

func TestAssignOwnerOutcome_ARunThatLeftRowsForAReprojectFails(t *testing.T) {
	if err := assignOwnerOutcome(application.AssignOwnerReport{}, nil); err != nil {
		t.Errorf("a clean run returned %v", err)
	}
	err := assignOwnerOutcome(application.AssignOwnerReport{
		NeedsReproject: []application.AssignOwnerSkip{{ID: "urn:statement:1"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "1 resource(s) left for a reproject") {
		t.Errorf("err = %v, want a failure naming the resources left for a reproject", err)
	}
}

func TestParseCreatedBound(t *testing.T) {
	for _, c := range []struct {
		value string
		end   bool
		want  time.Time
	}{
		{"2026-08-01", false, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-08-10", true, time.Date(2026, 8, 10, 23, 59, 59, 999999999, time.UTC)},
		{"2026-08-10T15:04:05Z", true, time.Date(2026, 8, 10, 15, 4, 5, 0, time.UTC)},
		{"2026-08-10T15:04:05-05:00", false, time.Date(2026, 8, 10, 20, 4, 5, 0, time.UTC)},
	} {
		got, err := parseCreatedBound("created-to", c.value, c.end)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("parse %q (end %v) = (%v, %v), want %v", c.value, c.end, got, err, c.want)
		}
	}
	if got, err := parseCreatedBound("created-from", "", false); err != nil || !got.IsZero() {
		t.Errorf("an empty bound = (%v, %v), want the zero time", got, err)
	}
	if _, err := parseCreatedBound("created-from", "August 1", false); err == nil ||
		!strings.Contains(err.Error(), "--created-from") {
		t.Errorf("a malformed bound: err = %v, want one naming --created-from", err)
	}
}

func TestPrintAssignOwnerTarget_NamesTheAccountAndTheWindow(t *testing.T) {
	var out bytes.Buffer
	printAssignOwnerTarget(&out, application.AssignOwnerReport{
		AccountID: "acct-1", AccountName: "Household", CreatedBy: "agent-1", DryRun: true,
		CreatedFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		CreatedTo:   time.Date(2026, 8, 10, 23, 59, 59, 999999999, time.UTC),
		MemberOf: []application.AssignOwnerAccount{
			{ID: "acct-1", Name: "Household"}, {ID: "acct-2", Name: "Studio"},
		},
	})
	text := out.String()
	for _, want := range []string{
		"DRY RUN",
		"account Household (acct-1), resources created by agent-1",
		"created from 2026-08-01T00:00:00Z to 2026-08-10T23:59:59Z",
		"agent-1 is a member of 2 accounts: Household (acct-1), Studio (acct-2)",
		"--confirm-account acct-1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("target lacks %q:\n%s", want, text)
		}
	}
}

func TestPrintAssignOwnerTarget_ARealRunSaysItCannotBeUndone(t *testing.T) {
	var out bytes.Buffer
	printAssignOwnerTarget(&out, application.AssignOwnerReport{
		AccountID: "acct-1", AccountName: "Household", CreatedBy: "agent-1",
	})
	text := out.String()
	for _, want := range []string{"cannot be undone", "account Household (acct-1)"} {
		if !strings.Contains(text, want) {
			t.Errorf("target lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "created from") {
		t.Errorf("a run with no window prints one:\n%s", text)
	}
}

func TestPrintAssignOwnerReport_DryRunListsEachResource(t *testing.T) {
	var out bytes.Buffer
	printAssignOwnerReport(&out, application.AssignOwnerReport{
		AccountID: "acct-1", CreatedBy: "agent-1", DryRun: true,
		Types: map[string]application.AssignOwnerTypeCount{"statement": {Ownerless: 1}},
		Resources: []application.AssignOwnerResource{{
			ID: "urn:statement:1", TypeSlug: "statement",
			CreatedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
		}},
	})
	if want := "2026-08-05T12:00:00Z  statement                urn:statement:1"; !strings.Contains(out.String(), want) {
		t.Errorf("dry run lacks %q:\n%s", want, out.String())
	}
}

// assignOwnerRunRules are what an operator must know before a real run. The
// command's long help and docs/_reference/cli.md both state each one.
var assignOwnerRunRules = []string{
	// The instance runs this release, and is stopped for a real run.
	"this release",
	"Stop the instance",
	"--dry-run may run while the instance serves",
	// An older binary that served skipped the event in its background groups.
	"weos worker checkpoint reset oxigraph",
	"weos worker checkpoint reset lexical-index",
	// Where the ids come from.
	"SELECT DISTINCT created_by FROM resources",
	"account_members",
	"credentials",
	// The side effect on update time.
	"updated_at",
	// What a stale record needs.
	"weos worker reproject",
}

func TestMaintenanceAssignOwner_HelpStatesTheRunRules(t *testing.T) {
	help := strings.Join(strings.Fields(maintenanceAssignOwnerCmd.Long), " ")
	for _, rule := range assignOwnerRunRules {
		if !strings.Contains(help, rule) {
			t.Errorf("the long help does not state %q", rule)
		}
	}
	if strings.Contains(help, "needs no running server") {
		t.Error("the long help still reads as safe to run while the server runs")
	}
}

func TestMaintenanceAssignOwner_DocsStateTheRunRules(t *testing.T) {
	raw, err := os.ReadFile("../../docs/_reference/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "## `weos maintenance assign-owner`")
	if start < 0 {
		t.Fatal("docs/_reference/cli.md has no assign-owner section")
	}
	section := doc[start:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	// Code marks are presentation; the words are what an operator reads.
	section = strings.Join(strings.Fields(strings.ReplaceAll(section, "`", "")), " ")
	for _, rule := range append(assignOwnerRunRules, "--created-from", "--created-to", "--confirm-account") {
		if !strings.Contains(section, rule) {
			t.Errorf("the cli.md section does not state %q", rule)
		}
	}
	if strings.Contains(section, "needs no running server") {
		t.Error("the cli.md section still reads as safe to run while the server runs")
	}
}
