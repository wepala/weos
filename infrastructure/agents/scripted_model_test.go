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

func TestNewScriptedModel_RejectsEmptyRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"rules":[{"whenMessageContains":"x"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewScriptedModel(path); err == nil {
		t.Fatal("a rule with neither call nor reply must be rejected at load")
	}
}
