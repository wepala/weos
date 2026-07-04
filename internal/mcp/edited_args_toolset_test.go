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

package mcp

import (
	"reflect"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

// confirmationContext is an agent context carrying a canned confirmation,
// the state the ADK flow sets when it re-runs a confirmed call.
type confirmationContext struct {
	agent.ContextMock
	conf *toolconfirmation.ToolConfirmation
}

func (c *confirmationContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return c.conf }

// recordingTool captures the args Run receives, standing in for a wrapped
// mcptoolset tool.
type recordingTool struct {
	gotArgs   any
	ranTimes  int
	processed bool
}

func (r *recordingTool) Name() string        { return "save_interpretation" }
func (r *recordingTool) Description() string { return "saves" }
func (r *recordingTool) IsLongRunning() bool { return false }
func (r *recordingTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{Name: r.Name()}
}
func (r *recordingTool) ProcessRequest(agent.Context, *model.LLMRequest) error {
	r.processed = true
	return nil
}
func (r *recordingTool) Run(_ agent.Context, args any) (map[string]any, error) {
	r.ranTimes++
	r.gotArgs = args
	return map[string]any{"output": "ok"}, nil
}

// plainTool implements only tool.Tool — the wrapper must pass it through.
type plainTool struct{}

func (plainTool) Name() string        { return "plain" }
func (plainTool) Description() string { return "not runnable" }
func (plainTool) IsLongRunning() bool { return false }

// stubToolset serves a fixed tool list.
type stubToolset struct{ tools []tool.Tool }

func (stubToolset) Name() string                                       { return "stub" }
func (s stubToolset) Tools(agent.ReadonlyContext) ([]tool.Tool, error) { return s.tools, nil }

func wrappedRecordingTool(t *testing.T) (functionTool, *recordingTool) {
	t.Helper()
	inner := &recordingTool{}
	tools, err := EditedArgsToolset(stubToolset{tools: []tool.Tool{inner}}).Tools(&testAgentContext{})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	ft, ok := tools[0].(functionTool)
	if !ok {
		t.Fatalf("wrapped tool is not runnable: %T", tools[0])
	}
	return ft, inner
}

func TestEditedArgsToolset_ApprovalWithPayloadSubstitutesArgs(t *testing.T) {
	wrapped, inner := wrappedRecordingTool(t)
	edited := map[string]any{"interpretations": []any{"user-edited"}}

	_, err := wrapped.Run(&confirmationContext{
		conf: &toolconfirmation.ToolConfirmation{Confirmed: true, Payload: edited},
	}, map[string]any{"interpretations": []any{"model-proposed"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(inner.gotArgs, edited) {
		t.Errorf("tool ran with %v, want the user-edited payload %v", inner.gotArgs, edited)
	}
}

func TestEditedArgsToolset_PlainApprovalRunsOriginalArgs(t *testing.T) {
	wrapped, inner := wrappedRecordingTool(t)
	original := map[string]any{"interpretations": []any{"model-proposed"}}

	_, err := wrapped.Run(&confirmationContext{
		conf: &toolconfirmation.ToolConfirmation{Confirmed: true},
	}, original)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(inner.gotArgs, original) {
		t.Errorf("tool ran with %v, want the original args %v", inner.gotArgs, original)
	}
}

func TestEditedArgsToolset_NoConfirmationPassesThrough(t *testing.T) {
	wrapped, inner := wrappedRecordingTool(t)
	original := map[string]any{"key": "value"}

	if _, err := wrapped.Run(&confirmationContext{}, original); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(inner.gotArgs, original) {
		t.Errorf("tool ran with %v, want %v", inner.gotArgs, original)
	}
}

func TestEditedArgsToolset_RejectionIgnoresPayload(t *testing.T) {
	wrapped, inner := wrappedRecordingTool(t)
	original := map[string]any{"interpretations": []any{"model-proposed"}}

	// The wrapped tool owns rejection semantics (it fails the call); the
	// wrapper must not substitute on a rejection.
	if _, err := wrapped.Run(&confirmationContext{
		conf: &toolconfirmation.ToolConfirmation{Confirmed: false, Payload: map[string]any{"x": 1}},
	}, original); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(inner.gotArgs, original) {
		t.Errorf("tool ran with %v, want the original args %v", inner.gotArgs, original)
	}
}

func TestEditedArgsToolset_NonObjectPayloadFailsClosed(t *testing.T) {
	wrapped, inner := wrappedRecordingTool(t)

	_, err := wrapped.Run(&confirmationContext{
		conf: &toolconfirmation.ToolConfirmation{Confirmed: true, Payload: []any{"not", "an", "object"}},
	}, map[string]any{"interpretations": []any{"model-proposed"}})
	if err == nil {
		t.Fatal("a non-object payload must fail the call, not fall back to the model-proposed args")
	}
	if inner.ranTimes != 0 {
		t.Errorf("tool executed %d times despite a malformed payload", inner.ranTimes)
	}
}

func TestEditedArgsToolset_DelegatesDeclarationAndProcessRequest(t *testing.T) {
	wrapped, inner := wrappedRecordingTool(t)

	if got := wrapped.Declaration().Name; got != "save_interpretation" {
		t.Errorf("Declaration().Name = %q", got)
	}
	rp, ok := wrapped.(requestProcessor)
	if !ok {
		t.Fatal("wrapper must keep the tool packable into LLM requests")
	}
	if err := rp.ProcessRequest(&confirmationContext{}, nil); err != nil {
		t.Fatalf("ProcessRequest: %v", err)
	}
	if !inner.processed {
		t.Error("ProcessRequest did not reach the wrapped tool")
	}
}

func TestEditedArgsToolset_NonRunnableToolsPassThrough(t *testing.T) {
	tools, err := EditedArgsToolset(stubToolset{tools: []tool.Tool{plainTool{}}}).Tools(&testAgentContext{})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if _, isWrapped := tools[0].(*editedArgsTool); isWrapped {
		t.Error("a tool without Run/ProcessRequest must pass through unwrapped")
	}
	if tools[0].Name() != "plain" {
		t.Errorf("tool = %q", tools[0].Name())
	}
}
