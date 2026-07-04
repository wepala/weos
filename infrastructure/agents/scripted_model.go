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
}

// scriptRule matches one moment of a conversation. Exactly one trigger:
// WhenMessageContains matches the latest user text; AfterTool matches the
// function response of a just-executed tool; neither set = the default.
// The action is a Call, a Reply, or both (reply after call is invalid —
// the model answers once per turn; a chained call is the way to continue).
type scriptRule struct {
	WhenMessageContains string      `json:"whenMessageContains,omitempty"`
	AfterTool           string      `json:"afterTool,omitempty"`
	Call                *scriptCall `json:"call,omitempty"`
	Reply               string      `json:"reply,omitempty"`
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
		if r.Call == nil && r.Reply == "" {
			return nil, fmt.Errorf("agent script %s: rule %d has neither call nor reply", path, i)
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
	rule := s.match(req)
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(s.respond(req, rule), nil)
	}
}

// match picks the first applicable rule: afterTool rules when the latest
// content is a tool's function response, whenMessageContains rules when it
// is user text, then the first trigger-less rule as the default.
func (s *scriptedModel) match(req *model.LLMRequest) *scriptRule {
	lastTool, lastText := latestMoment(req)
	for i := range s.rules {
		r := &s.rules[i]
		switch {
		case r.AfterTool != "" && lastTool != "" && r.AfterTool == lastTool:
			return r
		case r.WhenMessageContains != "" && lastText != "" &&
			strings.Contains(strings.ToLower(lastText), strings.ToLower(r.WhenMessageContains)):
			return r
		}
	}
	for i := range s.rules {
		if s.rules[i].AfterTool == "" && s.rules[i].WhenMessageContains == "" {
			return &s.rules[i]
		}
	}
	return nil
}

// respond renders the rule as a model response — a minted function call or
// a widget-JSON reply.
func (s *scriptedModel) respond(req *model.LLMRequest, rule *scriptRule) *model.LLMResponse {
	if rule != nil && rule.Call != nil {
		args := rule.Call.Args
		if rule.Call.ArgsFromMessageJSON {
			args = fencedJSONArgs(latestUserText(req))
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
	if rule != nil && rule.Reply != "" {
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

// latestMoment scans the request tail: the name of the tool whose function
// response arrived last (skipping the confirmation protocol's own frames),
// or the latest user text.
func latestMoment(req *model.LLMRequest) (lastTool, lastText string) {
	if req == nil {
		return "", ""
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
				return p.FunctionResponse.Name, ""
			}
			if p.Text != "" && c.Role == genai.RoleUser {
				return "", p.Text
			}
		}
	}
	return "", ""
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
