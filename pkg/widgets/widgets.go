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

// Package widgets defines the versioned output contract of the in-app agent:
// a response is an ordered list of typed widgets any client (the admin SPA
// or a third-party renderer) can render consistently. The set is closed —
// agents never emit free-form markup — and validation degrades anything
// malformed to a markdown widget, so a bad payload can never break a
// response. See docs/_reference/agent-widgets.md for the contract with
// examples.
package widgets

import (
	"encoding/json"
	"net/url"
	"strings"
)

// SchemaVersion is the contract version carried on every response. Clients
// should render unknown widget types of future versions as markdown.
const SchemaVersion = 1

// Type enumerates the v1 closed widget set.
type Type string

const (
	TypeMarkdown Type = "markdown"
	TypeTable    Type = "table"
	TypeList     Type = "list"
	TypeCard     Type = "card"
)

// Field is one labeled value on a card.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Widget is one renderable block. Only the fields for its Type are set.
type Widget struct {
	Type Type `json:"type"`

	// Markdown carries the body of a markdown widget.
	Markdown string `json:"markdown,omitempty"`

	// Title captions a table, list, or card.
	Title string `json:"title,omitempty"`

	// Columns and Rows shape a table; every row has one cell per column.
	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`

	// Items are the entries of a list widget.
	Items []string `json:"items,omitempty"`

	// Body, URL, and Fields belong to a card widget.
	Body   string  `json:"body,omitempty"`
	URL    string  `json:"url,omitempty"`
	Fields []Field `json:"fields,omitempty"`
}

// Response is what the in-app agent returns for a turn: widgets in render
// order.
type Response struct {
	SchemaVersion int      `json:"schemaVersion"`
	Widgets       []Widget `json:"widgets"`
}

// FromText wraps plain text in a single markdown widget — the universal
// fallback.
func FromText(text string) Response {
	return Response{
		SchemaVersion: SchemaVersion,
		Widgets:       []Widget{{Type: TypeMarkdown, Markdown: text}},
	}
}

// Parse turns an agent's raw output into a valid Response. It accepts the
// contract JSON (optionally wrapped in a markdown code fence, which models
// sometimes add) and degrades gracefully: output that isn't contract JSON
// becomes one markdown widget, and any individual widget that fails
// validation is replaced by a markdown widget carrying its raw JSON. Parse
// never fails and never returns an empty response for non-empty input.
func Parse(raw string) Response {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if s == "" {
		return Response{SchemaVersion: SchemaVersion}
	}

	// schemaVersion is deliberately not decoded: Parse stamps the version it
	// validated against, and a wrong-typed incoming value must not sink an
	// otherwise-valid payload.
	var payload struct {
		Widgets []json.RawMessage `json:"widgets"`
	}
	if err := json.Unmarshal([]byte(s), &payload); err != nil || len(payload.Widgets) == 0 {
		// Not contract JSON — or contract JSON with nothing to render, which
		// must still render something.
		return FromText(raw)
	}

	out := Response{SchemaVersion: SchemaVersion, Widgets: make([]Widget, 0, len(payload.Widgets))}
	for _, rawWidget := range payload.Widgets {
		out.Widgets = append(out.Widgets, parseWidget(rawWidget))
	}
	return out
}

// parseWidget validates one widget, degrading anything malformed to
// markdown so rendering always succeeds.
func parseWidget(raw json.RawMessage) Widget {
	var w Widget
	if err := json.Unmarshal(raw, &w); err != nil {
		return degrade(raw)
	}
	switch w.Type {
	case TypeMarkdown:
		if w.Markdown == "" {
			return degrade(raw)
		}
		return Widget{Type: TypeMarkdown, Markdown: w.Markdown, Title: w.Title}
	case TypeTable:
		if len(w.Columns) == 0 || !rowsMatchColumns(w.Rows, len(w.Columns)) {
			return degrade(raw)
		}
		return Widget{Type: TypeTable, Title: w.Title, Columns: w.Columns, Rows: w.Rows}
	case TypeList:
		if len(w.Items) == 0 {
			return degrade(raw)
		}
		return Widget{Type: TypeList, Title: w.Title, Items: w.Items}
	case TypeCard:
		if w.Title == "" && w.Body == "" {
			return degrade(raw)
		}
		return Widget{Type: TypeCard, Title: w.Title, Body: w.Body, URL: safeURL(w.URL), Fields: w.Fields}
	default:
		return degrade(raw)
	}
}

// safeURL keeps only http(s) and mailto links. Model output is derived from
// untrusted content, so a javascript:/data: URL here would be script
// execution in the renderer's origin — the URL is dropped, the card stays.
func safeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return u.String()
	default:
		return ""
	}
}

func rowsMatchColumns(rows [][]string, columns int) bool {
	for _, row := range rows {
		if len(row) != columns {
			return false
		}
	}
	return true
}

// degrade wraps an invalid widget's raw JSON in a fenced markdown block so
// the content is preserved for the reader instead of dropped. The fence is
// four backticks so content containing a ``` run cannot close it early.
func degrade(raw json.RawMessage) Widget {
	return Widget{Type: TypeMarkdown, Markdown: "````json\n" + string(raw) + "\n````"}
}
