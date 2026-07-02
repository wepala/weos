package widgets

import (
	"strings"
	"testing"
)

func TestParse_ValidContract(t *testing.T) {
	raw := `{"schemaVersion":1,"widgets":[
		{"type":"markdown","markdown":"Hello"},
		{"type":"table","title":"People","columns":["Name","Email"],"rows":[["Ada","ada@x.io"]]},
		{"type":"list","items":["one","two"]},
		{"type":"card","title":"Ada","body":"Engineer","fields":[{"label":"Email","value":"ada@x.io"}]}
	]}`
	resp := Parse(raw)
	if resp.SchemaVersion != SchemaVersion || len(resp.Widgets) != 4 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	want := []Type{TypeMarkdown, TypeTable, TypeList, TypeCard}
	for i, w := range resp.Widgets {
		if w.Type != want[i] {
			t.Errorf("widget %d type = %q, want %q", i, w.Type, want[i])
		}
	}
}

func TestParse_CodeFencedJSON(t *testing.T) {
	raw := "```json\n{\"schemaVersion\":1,\"widgets\":[{\"type\":\"markdown\",\"markdown\":\"hi\"}]}\n```"
	resp := Parse(raw)
	if len(resp.Widgets) != 1 || resp.Widgets[0].Markdown != "hi" {
		t.Fatalf("fenced JSON not parsed: %+v", resp)
	}
}

func TestParse_PlainTextFallsBackToMarkdown(t *testing.T) {
	resp := Parse("Just a plain answer.")
	if len(resp.Widgets) != 1 || resp.Widgets[0].Type != TypeMarkdown ||
		resp.Widgets[0].Markdown != "Just a plain answer." {
		t.Fatalf("plain text must become one markdown widget: %+v", resp)
	}
}

func TestParse_InvalidWidgetDegradesNotBreaks(t *testing.T) {
	raw := `{"schemaVersion":1,"widgets":[
		{"type":"table","columns":["A"],"rows":[["1","2"]]},
		{"type":"hologram","spin":true},
		{"type":"markdown","markdown":"still here"}
	]}`
	resp := Parse(raw)
	if len(resp.Widgets) != 3 {
		t.Fatalf("expected all 3 widgets (degraded, not dropped): %+v", resp)
	}
	if resp.Widgets[0].Type != TypeMarkdown || !strings.Contains(resp.Widgets[0].Markdown, `"rows"`) {
		t.Errorf("ragged table must degrade to markdown carrying its raw JSON: %+v", resp.Widgets[0])
	}
	if resp.Widgets[1].Type != TypeMarkdown || !strings.Contains(resp.Widgets[1].Markdown, "hologram") {
		t.Errorf("unknown type must degrade to markdown: %+v", resp.Widgets[1])
	}
	if resp.Widgets[2].Markdown != "still here" {
		t.Errorf("valid widget after invalid ones must survive: %+v", resp.Widgets[2])
	}
}

func TestParse_EmptyWidgetArrayStillRenders(t *testing.T) {
	raw := `{"schemaVersion":1,"widgets":[]}`
	resp := Parse(raw)
	if len(resp.Widgets) != 1 || resp.Widgets[0].Type != TypeMarkdown {
		t.Fatalf("empty widget array must fall back to a markdown widget: %+v", resp)
	}
}

func TestParse_WrongTypedSchemaVersionTolerated(t *testing.T) {
	raw := `{"schemaVersion":"one","widgets":[{"type":"markdown","markdown":"ok"}]}`
	resp := Parse(raw)
	if len(resp.Widgets) != 1 || resp.Widgets[0].Markdown != "ok" {
		t.Fatalf("wrong-typed schemaVersion must not sink valid widgets: %+v", resp)
	}
}

func TestParse_EmptyInput(t *testing.T) {
	resp := Parse("   ")
	if len(resp.Widgets) != 0 || resp.SchemaVersion != SchemaVersion {
		t.Fatalf("empty input: %+v", resp)
	}
}

func TestFromText(t *testing.T) {
	resp := FromText("hello")
	if len(resp.Widgets) != 1 || resp.Widgets[0].Type != TypeMarkdown || resp.Widgets[0].Markdown != "hello" {
		t.Fatalf("FromText: %+v", resp)
	}
}
