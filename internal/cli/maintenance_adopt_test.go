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
	"github.com/wepala/weos/v3/domain/repositories"
)

func adoptReport(dryRun bool) application.AdoptAccountReport {
	return application.AdoptAccountReport{
		FromAccountID: "acct-old", FromAccountName: "Old local twin",
		AccountID: "acct-new", AccountName: "Akeem", AgentID: "agent-1", AgentRole: "owner", DryRun: dryRun,
		Types: map[string]application.AdoptAccountTypeCount{},
	}
}

func TestPrintAdoptAccount_APreviewNamesBothAccountsTheMemberCheckAndCountsPerType(t *testing.T) {
	var out bytes.Buffer
	r := adoptReport(true)
	r.Types["task"] = application.AdoptAccountTypeCount{ToMove: 4260}
	r.Types["project"] = application.AdoptAccountTypeCount{ToMove: 2130}
	printAdoptAccountTarget(&out, r)
	printAdoptAccountReport(&out, r)
	text := out.String()
	for _, want := range []string{
		"DRY RUN — nothing was written. Re-run with --write to apply.",
		"from Old local twin (acct-old): 0 members",
		"to   Akeem (acct-new): agent-1 is owner",
		"project                  2130 to move",
		"task                     4260 to move",
		"total: 6390 resource(s) would be moved (one Resource.AccountAdopted event each)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("preview lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "project") > strings.Index(text, "task") {
		t.Errorf("types are not in name order:\n%s", text)
	}
}

// wm-m0ci05: the preview shows the pending invitations and who created the
// source's resources — whether each can still sign in, how many it wrote and
// when it last wrote one — and warns when one of them can still sign in.
func TestPrintAdoptAccount_APreviewShowsInvitesAndWhoCreatedTheResources(t *testing.T) {
	var out bytes.Buffer
	r := adoptReport(true)
	newest := time.Date(2026, 6, 30, 18, 4, 0, 0, time.UTC)
	r.FromCreators = []repositories.AccountCreator{
		{AgentID: "agent-old", Resources: 7077, NewestCreatedAt: newest},
		{AgentID: "agent-live", Resources: 1, HasCredential: true, NewestCreatedAt: newest.Add(time.Hour)},
	}
	printAdoptAccountTarget(&out, r)
	text := out.String()
	for _, want := range []string{
		"from Old local twin (acct-old): 0 members, 0 pending invitations",
		"created by agent-old: 7077 resource(s), newest 2026-06-30T18:04:00Z, no credential",
		"created by agent-live: 1 resource(s), newest 2026-06-30T19:04:00Z, HAS A CREDENTIAL",
		"WARNING: 1 creator(s) can still sign in",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("preview lacks %q:\n%s", want, text)
		}
	}
}

func TestPrintAdoptAccount_ARealRunSaysWhatLanded(t *testing.T) {
	var out bytes.Buffer
	r := adoptReport(false)
	r.Types["task"] = application.AdoptAccountTypeCount{ToMove: 3, Moved: 2, Skipped: 1, RowsToMove: 1, RowsMoved: 1}
	printAdoptAccountTarget(&out, r)
	printAdoptAccountReport(&out, r)
	text := out.String()
	for _, want := range []string{
		"moving resources through the event stream. A move cannot be undone.",
		"task                     3 to move, moved 2; 1 row(s) left in the source, moved 1, " +
			"skipped 1 that left the source account during the run",
		"total: 2 of 3 resource(s) moved",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "DRY RUN") {
		t.Errorf("a real run reads as a preview:\n%s", text)
	}
}

func TestPrintAdoptAccount_NothingToMove(t *testing.T) {
	var out bytes.Buffer
	printAdoptAccountReport(&out, adoptReport(true))
	if !strings.Contains(out.String(), "nothing to move") {
		t.Errorf("report lacks the nothing-to-move line:\n%s", out.String())
	}
}

func TestPrintAdoptAccount_ListsRowsOfAThirdAccountAndRowsForAReproject(t *testing.T) {
	var out bytes.Buffer
	r := adoptReport(false)
	r.Types["task"] = application.AdoptAccountTypeCount{ToMove: 2, Moved: 0, NeedsReproject: 1, ForeignRows: 1}
	r.NeedsReproject = []application.AssignOwnerSkip{{ID: "urn:task:a", TypeSlug: "task", Reason: "behind"}}
	r.ForeignRows = []application.AssignOwnerForeignRow{{ID: "urn:task:b", TypeSlug: "task", Reason: "third"}}
	printAdoptAccountReport(&out, r)
	text := out.String()
	for _, want := range []string{"urn:task:a (task): behind", "weos worker reproject",
		"urn:task:b (task): third", "never moved"} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	if err := adoptAccountOutcome(r, nil); err == nil {
		t.Error("a run that left resources behind exits 0")
	}
	if err := adoptAccountOutcome(adoptReport(false), nil); err != nil {
		t.Errorf("a clean run fails: %v", err)
	}
}

func TestMaintenanceAdoptAccount_IsRegisteredAndNeedsEveryID(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"maintenance", "adopt-account"})
	if err != nil || found != maintenanceAdoptAccountCmd {
		t.Fatalf("weos maintenance adopt-account is not registered: %v", err)
	}
	for _, flag := range []string{"from", "account", "agent", "write"} {
		if maintenanceAdoptAccountCmd.Flags().Lookup(flag) == nil {
			t.Errorf("flag --%s is missing", flag)
		}
	}
	if f := maintenanceAdoptAccountCmd.Flags().Lookup("write"); f != nil && f.DefValue != "false" {
		t.Errorf("--write defaults to %s, want false", f.DefValue)
	}
	if err := runMaintenanceAdoptAccount(maintenanceAdoptAccountCmd, nil); err == nil ||
		!strings.Contains(err.Error(), "--from, --account and --agent") {
		t.Fatalf("err = %v, want a refusal naming the three flags", err)
	}
}

// The rules an operator must read before a real run, in the help and in the
// reference docs. Compared without case: the help shouts the first one.
var adoptAccountRunRules = []string{
	"PREVIEW BY DEFAULT",
	"Stop the instance for the real run",
	"no members",
	"owner or admin",
	"never deleted",
	"weos worker reproject",
	"weos worker checkpoint reset oxigraph",
	"weos worker checkpoint reset lexical-index",
	// wm-myzy4t: an older binary's erasure of the source deletes what moved.
	"Erase it only with this release or later",
	// wm-m0ci05.
	"pending invitation",
	"still has a credential",
}

func TestMaintenanceAdoptAccount_HelpStatesTheRunRules(t *testing.T) {
	help := strings.ToLower(strings.Join(strings.Fields(maintenanceAdoptAccountCmd.Long), " "))
	for _, rule := range adoptAccountRunRules {
		if !strings.Contains(help, strings.ToLower(rule)) {
			t.Errorf("the long help does not state %q", rule)
		}
	}
}

func TestMaintenanceAdoptAccount_DocsStateTheRunRules(t *testing.T) {
	raw, err := os.ReadFile("../../docs/_reference/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "## `weos maintenance adopt-account`")
	if start < 0 {
		t.Fatal("docs/_reference/cli.md has no adopt-account section")
	}
	section := doc[start:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	section = strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(section, "`", "")), " "))
	for _, rule := range append(adoptAccountRunRules, "--from", "--account", "--agent", "--write") {
		if !strings.Contains(section, strings.ToLower(rule)) {
			t.Errorf("the cli.md section does not state %q", rule)
		}
	}
}
