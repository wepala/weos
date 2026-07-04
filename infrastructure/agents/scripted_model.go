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
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"regexp"
	"strings"
	"sync/atomic"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Scripted agent model (test harness, #420): WEOS_AGENT_SCRIPT points at a
// JSON script and the orchestrator runs deterministic turns without an LLM
// — no key, no network, no spend. Everything except the model's judgment is
// the real pipeline: tool calls execute over MCP, mutating tools pause for
// confirmation, approve-with-edits substitutes payloads. Test-only, the
// same convention as a downstream app's fixture knobs; never set it in
// production.

// AgentScriptEnv names the env var holding the script path.
const AgentScriptEnv = "WEOS_AGENT_SCRIPT"

// scriptCall is a tool call a rule emits. Args are either fixed or
// extracted from a ```json fenced block in the user's message — how a test
// binding smuggles exact tool arguments through the conversation.
type scriptCall struct {
	Tool                string         `json:"tool"`
	Args                map[string]any `json:"args,omitempty"`
	ArgsFromMessageJSON bool           `json:"argsFromMessageJSON,omitempty"`
	// ArgsFromToolOutputField takes the named field of the PREVIOUS tool's
	// output object as this call's arguments — how a scripted chain passes a
	// tool-composed object (e.g. run_interpretation's proposal) to the next
	// call the way a live model would.
	ArgsFromToolOutputField string `json:"argsFromToolOutputField,omitempty"`
}

// scriptRule matches one moment of a conversation. Exactly one trigger:
// WhenMessageContains matches the latest user text; AfterTool matches the
// function response of a just-executed tool; neither set = the default.
// The action is a Call or a Reply; ReplyFromToolOutput renders the matched
// tool's real response instead of canned text, so a scenario can assert
// the genuine outcome (e.g. "already imported") through the scripted rail.
type scriptRule struct {
	WhenMessageContains string `json:"whenMessageContains,omitempty"`
	AfterTool           string `json:"afterTool,omitempty"`
	// AfterToolFailed matches a tool's FAILED function response (e.g. a
	// rejected confirmation) — checked before AfterTool so a script can
	// stop a chain instead of marching on after a rejection.
	AfterToolFailed     string      `json:"afterToolFailed,omitempty"`
	Call                *scriptCall `json:"call,omitempty"`
	Reply               string      `json:"reply,omitempty"`
	ReplyFromToolOutput bool        `json:"replyFromToolOutput,omitempty"`
}

// scriptedModel is a model.LLM driven by ordered rules.
type scriptedModel struct {
	rules  []scriptRule
	callID atomic.Int64
}

// NewScriptedModel loads a script file into a deterministic model.
func NewScriptedModel(path string) (model.LLM, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent script: %w", err)
	}
	var file struct {
		Rules []scriptRule `json:"rules"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse agent script %s: %w", path, err)
	}
	for i, r := range file.Rules {
		if r.Call == nil && r.Reply == "" && !r.ReplyFromToolOutput {
			return nil, fmt.Errorf("agent script %s: rule %d has neither call nor reply", path, i)
		}
		if r.ReplyFromToolOutput && r.AfterTool == "" && r.AfterToolFailed == "" {
			return nil, fmt.Errorf("agent script %s: rule %d: replyFromToolOutput needs afterTool", path, i)
		}
	}
	return &scriptedModel{rules: file.Rules}, nil
}

func (s *scriptedModel) Name() string { return "scripted" }

// GenerateContent matches the request's latest moment against the rules and
// emits the scripted action. It never errors on a non-match: the default
// rule (or a built-in fallback reply) answers, so a scripted run degrades
// to a visible no-op turn instead of a hung one.
func (s *scriptedModel) GenerateContent(
	_ context.Context, req *model.LLMRequest, _ bool,
) iter.Seq2[*model.LLMResponse, error] {
	rule, toolOutput := s.match(req)
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(s.respond(req, rule, toolOutput), nil)
	}
}

// match picks the first applicable rule: afterTool rules when the latest
// content is a tool's function response, whenMessageContains rules when it
// is user text, then the first trigger-less rule as the default. It also
// returns the matched tool's response map for replyFromToolOutput.
func (s *scriptedModel) match(req *model.LLMRequest) (*scriptRule, map[string]any) {
	lastTool, lastText, toolOutput := latestMoment(req)
	failed := toolOutput != nil && toolOutput["error"] != nil
	for i := range s.rules {
		r := &s.rules[i]
		switch {
		case r.AfterToolFailed != "" && lastTool != "" && r.AfterToolFailed == lastTool && failed:
			return r, toolOutput
		case r.AfterTool != "" && lastTool != "" && r.AfterTool == lastTool && !failed:
			return r, toolOutput
		case r.WhenMessageContains != "" && lastText != "" &&
			strings.Contains(strings.ToLower(lastText), strings.ToLower(r.WhenMessageContains)):
			return r, nil
		}
	}
	for i := range s.rules {
		if s.rules[i].AfterTool == "" && s.rules[i].WhenMessageContains == "" {
			return &s.rules[i], nil
		}
	}
	return nil, nil
}

// respond renders the rule as a model response — a minted function call or
// a widget-JSON reply.
func (s *scriptedModel) respond(req *model.LLMRequest, rule *scriptRule, toolOutput map[string]any) *model.LLMResponse {
	if rule != nil && rule.Call != nil {
		args := rule.Call.Args
		if rule.Call.ArgsFromMessageJSON {
			args = fencedJSONArgs(latestUserText(req))
		}
		if f := rule.Call.ArgsFromToolOutputField; f != "" && toolOutput != nil {
			// MCP structured outputs arrive wrapped under "output".
			source := toolOutput
			if inner, ok := toolOutput["output"].(map[string]any); ok {
				source = inner
			}
			if picked, ok := source[f].(map[string]any); ok {
				args = picked
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		return &model.LLMResponse{Content: &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
				ID:   fmt.Sprintf("scripted-call-%d", s.callID.Add(1)),
				Name: rule.Call.Tool,
				Args: args,
			}}},
		}}
	}
	reply := "The scripted agent has no rule for this turn."
	switch {
	case rule != nil && rule.ReplyFromToolOutput && toolOutput != nil:
		// The tool's real response, verbatim — so a scenario can assert the
		// genuine outcome through the scripted rail.
		raw, err := json.Marshal(toolOutput)
		if err == nil {
			reply = string(raw)
		}
		if rule.Reply != "" {
			reply = rule.Reply + "\n" + reply
		}
	case rule != nil && rule.Reply != "":
		reply = rule.Reply
	}
	widget, _ := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"widgets":       []map[string]any{{"type": "markdown", "markdown": reply}},
	})
	return &model.LLMResponse{Content: &genai.Content{
		Role:  genai.RoleModel,
		Parts: []*genai.Part{{Text: string(widget)}},
	}}
}

// latestMoment scans the request tail: the name (and response map) of the
// tool whose function response arrived last (skipping the confirmation
// protocol's own frames), or the latest user text.
func latestMoment(req *model.LLMRequest) (lastTool, lastText string, toolOutput map[string]any) {
	if req == nil {
		return "", "", nil
	}
	for i := len(req.Contents) - 1; i >= 0; i-- {
		c := req.Contents[i]
		if c == nil {
			continue
		}
		for j := len(c.Parts) - 1; j >= 0; j-- {
			p := c.Parts[j]
			if p == nil {
				continue
			}
			if p.FunctionResponse != nil {
				if p.FunctionResponse.Name == "adk_request_confirmation" {
					continue // the protocol frame, not a tool outcome
				}
				if isAwaitingConfirmation(p.FunctionResponse.Response) {
					// The pause artifact ("requires confirmation") the flow
					// records when a mutating call first pauses — not a tool
					// outcome; a fresh user message may legitimately follow.
					continue
				}
				return p.FunctionResponse.Name, "", p.FunctionResponse.Response
			}
			if p.Text != "" && c.Role == genai.RoleUser {
				return "", p.Text, nil
			}
		}
	}
	return "", "", nil
}

// isAwaitingConfirmation reports whether a function response is the pause
// artifact ADK records when a mutating call awaits approval — distinct
// from a real failure (e.g. a rejection), which IS an outcome.
func isAwaitingConfirmation(response map[string]any) bool {
	if response == nil {
		return false
	}
	errText, _ := response["error"].(string)
	return strings.Contains(errText, "requires confirmation")
}

// latestUserText returns the most recent user-authored text content.
func latestUserText(req *model.LLMRequest) string {
	if req == nil {
		return ""
	}
	for i := len(req.Contents) - 1; i >= 0; i-- {
		c := req.Contents[i]
		if c == nil || c.Role != genai.RoleUser {
			continue
		}
		for j := len(c.Parts) - 1; j >= 0; j-- {
			if p := c.Parts[j]; p != nil && p.Text != "" {
				return p.Text
			}
		}
	}
	return ""
}

var fencedJSON = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// fencedJSONArgs extracts the first ```json fenced object from a message.
// A missing or unparseable block yields nil (the caller substitutes {}),
// which surfaces as a tool validation error — visible, not hung.
func fencedJSONArgs(message string) map[string]any {
	m := fencedJSON.FindStringSubmatch(message)
	if len(m) < 2 {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(m[1]), &args); err != nil {
		return nil
	}
	return args
}
