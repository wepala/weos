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

package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

const testScript = `{
  "rules": [
    {"whenMessageContains": "import the statement",
     "call": {"tool": "import_statement", "argsFromMessageJSON": true}},
    {"afterTool": "import_statement", "reply": "Imported."},
    {"reply": "Nothing scripted for that."}
  ]
}`

func scriptedFromString(t *testing.T, script string) model.LLM {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script.json")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewScriptedModel(path)
	if err != nil {
		t.Fatalf("NewScriptedModel: %v", err)
	}
	return m
}

func oneResponse(t *testing.T, m model.LLM, req *model.LLMRequest) *model.LLMResponse {
	t.Helper()
	for resp, err := range m.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("GenerateContent: %v", err)
		}
		return resp
	}
	t.Fatal("no response yielded")
	return nil
}

func userMessage(text string) *model.LLMRequest {
	return &model.LLMRequest{Contents: []*genai.Content{
		{Role: genai.RoleUser, Parts: []*genai.Part{{Text: text}}},
	}}
}

func TestScriptedModel_MessageRuleEmitsToolCallWithFencedArgs(t *testing.T) {
	m := scriptedFromString(t, testScript)

	resp := oneResponse(t, m, userMessage(
		"Please import the statement.\n```json\n{\"fileName\":\"march.csv\",\"openingBalance\":\"100.00\"}\n```"))
	call := resp.Content.Parts[0].FunctionCall
	if call == nil || call.Name != "import_statement" {
		t.Fatalf("expected an import_statement call, got %+v", resp.Content.Parts[0])
	}
	if call.ID == "" {
		t.Error("the scripted model must mint a function-call id")
	}
	if call.Args["fileName"] != "march.csv" {
		t.Errorf("args = %v, want the fenced JSON block's contents", call.Args)
	}
}

func TestScriptedModel_AfterToolRuleReplies(t *testing.T) {
	m := scriptedFromString(t, testScript)

	resp := oneResponse(t, m, &model.LLMRequest{Contents: []*genai.Content{
		{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "import the statement"}}},
		{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
			ID: "scripted-call-1", Name: "import_statement", Args: map[string]any{}}}}},
		{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			ID: "scripted-call-1", Name: "import_statement",
			Response: map[string]any{"output": "ok"}}}}},
	}})
	text := resp.Content.Parts[0].Text
	if !strings.Contains(text, "Imported.") || !strings.Contains(text, "schemaVersion") {
		t.Errorf("after-tool reply = %q, want the scripted reply wrapped as widget JSON", text)
	}
}

func TestScriptedModel_DefaultRuleAndFallback(t *testing.T) {
	m := scriptedFromString(t, testScript)
	resp := oneResponse(t, m, userMessage("something unscripted"))
	if !strings.Contains(resp.Content.Parts[0].Text, "Nothing scripted") {
		t.Errorf("default reply = %q", resp.Content.Parts[0].Text)
	}

	// No default rule in the script → built-in fallback, never a hang.
	bare := scriptedFromString(t, `{"rules": [{"whenMessageContains": "x", "reply": "y"}]}`)
	resp = oneResponse(t, bare, userMessage("something else"))
	if !strings.Contains(resp.Content.Parts[0].Text, "no rule") {
		t.Errorf("fallback reply = %q", resp.Content.Parts[0].Text)
	}
}

func TestScriptedModel_ConfirmationFrameIsNotAToolOutcome(t *testing.T) {
	m := scriptedFromString(t, testScript)

	// A confirmation response frame must not match afterTool rules — the
	// real tool outcome arrives in a later frame.
	resp := oneResponse(t, m, &model.LLMRequest{Contents: []*genai.Content{
		{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			ID: "call-9", Name: "adk_request_confirmation",
			Response: map[string]any{"confirmed": true}}}}},
	}})
	if strings.Contains(resp.Content.Parts[0].Text, "Imported.") {
		t.Error("the confirmation protocol frame matched an afterTool rule")
	}
}

func TestNewScriptedModel_RejectsLooseRules(t *testing.T) {
	for name, script := range map[string]string{
		"neither call nor reply": `{"rules":[{"whenMessageContains":"x"}]}`,
		"multiple triggers":      `{"rules":[{"whenMessageContains":"x","afterTool":"t","reply":"y"}]}`,
		"call plus reply":        `{"rules":[{"whenMessageContains":"x","call":{"tool":"t"},"reply":"y"}]}`,
		"call plus replyFromToolOutput": `{"rules":[` +
			`{"afterTool":"t","call":{"tool":"u"},"replyFromToolOutput":true}]}`,
		"replyFromToolOutput without a tool trigger": `{"rules":[{"replyFromToolOutput":true}]}`,
	} {
		path := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewScriptedModel(path); err == nil {
			t.Errorf("%s must be rejected at load", name)
		}
	}

	// replyFromToolOutput triggered by a failure is a legitimate shape.
	path := filepath.Join(t.TempDir(), "good.json")
	script := `{"rules":[{"afterToolFailed":"t","replyFromToolOutput":true}]}`
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewScriptedModel(path); err != nil {
		t.Errorf("afterToolFailed with replyFromToolOutput must load: %v", err)
	}
}

func TestFencedJSONArgs_NestedObjects(t *testing.T) {
	// The closing fence anchors the regex, so backtracking must carry the
	// capture past nested closing braces to the object's real end.
	args := fencedJSONArgs("import this\n```json\n" +
		`{"statement": {"fileName": "march.csv"}, "interpretations": [{"line": 1, "meta": {"x": 2}}]}` +
		"\n```")
	if args == nil {
		t.Fatal("a fenced object with nested objects must parse")
	}
	statement, ok := args["statement"].(map[string]any)
	if !ok || statement["fileName"] != "march.csv" {
		t.Errorf("nested object truncated: %v", args)
	}
	if _, ok := args["interpretations"]; !ok {
		t.Errorf("trailing keys after the nested object were lost: %v", args)
	}
}

// After a coordinator transfer, ADK narrates the parent's tool activity to
// the sub-agent as USER-role text ("For context:" + call/result echoes) —
// there is no FunctionResponse on the sub-agent's side. The scripted model
// must treat the result echo as that tool's moment (so afterTool rules can
// continue a hub handoff) and must NOT let the echoes shadow the real user
// message for argsFromMessageJSON.
func TestScriptedModel_HubTransferEchoIsAToolMoment(t *testing.T) {
	m := scriptedFromString(t, `{
	  "rules": [
	    {"whenMessageContains": "import the statement",
	     "call": {"tool": "transfer_to_agent", "args": {"agent_name": "statement_import"}}},
	    {"afterTool": "transfer_to_agent",
	     "call": {"tool": "check_duplicate_import", "argsFromMessageJSON": true}},
	    {"reply": "Nothing scripted for that."}
	  ]
	}`)

	// The sub-agent's request, exactly as the runner shapes it post-transfer.
	req := &model.LLMRequest{Contents: []*genai.Content{
		{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "Please import the statement I'm handing you.\n```json\n{\"fileName\":\"march.csv\"}\n```"}}},
		{Role: genai.RoleUser, Parts: []*genai.Part{
			{Text: "For context:"},
			{Text: "[weos_coordinator] called tool `transfer_to_agent` with parameters: {\"agent_name\":\"statement_import\"}"},
		}},
		{Role: genai.RoleUser, Parts: []*genai.Part{
			{Text: "For context:"},
			{Text: "[weos_coordinator] `transfer_to_agent` tool returned result: {}"},
		}},
	}}

	resp := oneResponse(t, m, req)
	call := resp.Content.Parts[0].FunctionCall
	if call == nil || call.Name != "check_duplicate_import" {
		t.Fatalf("expected the afterTool transfer rule to fire check_duplicate_import, got %+v", resp.Content.Parts[0])
	}
	if call.Args["fileName"] != "march.csv" {
		t.Errorf("args = %v, want the ORIGINAL user message's fenced JSON (echoes must not shadow it)", call.Args)
	}
}

// The result echo carries the tool's outcome; a narrated error must count as
// a failed moment so afterToolFailed rules (not afterTool) match it.
func TestScriptedModel_HubTransferEchoErrorIsAFailedMoment(t *testing.T) {
	m := scriptedFromString(t, `{
	  "rules": [
	    {"afterTool": "transfer_to_agent", "reply": "transferred"},
	    {"afterToolFailed": "transfer_to_agent", "reply": "transfer failed"},
	    {"reply": "Nothing scripted for that."}
	  ]
	}`)

	req := &model.LLMRequest{Contents: []*genai.Content{
		{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "hi"}}},
		{Role: genai.RoleUser, Parts: []*genai.Part{
			{Text: "[weos_coordinator] `transfer_to_agent` tool returned result: {\"error\":\"tool 'transfer_to_agent' not found.\"}"},
		}},
	}}

	resp := oneResponse(t, m, req)
	if text := resp.Content.Parts[0].Text; !strings.Contains(text, "transfer failed") {
		t.Errorf("reply = %q, want the afterToolFailed branch", text)
	}
}
