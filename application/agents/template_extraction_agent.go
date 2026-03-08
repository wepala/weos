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
	"strings"

	"weos/domain/entities"
	infraAgents "weos/infrastructure/agents"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/genai"

	"go.uber.org/fx"
)

// templateExtractionResponseSchema defines the expected JSON shape for the template agent response.
var templateExtractionResponseSchema = &genai.Schema{
	Type:     genai.TypeObject,
	Required: []string{"page_type", "description", "sections"},
	Properties: map[string]*genai.Schema{
		"page_type": {
			Type:        genai.TypeString,
			Description: "Page type: homepage, about, contact, blog, portfolio, services, pricing, faq, product, landing, etc.",
		},
		"description": {
			Type:        genai.TypeString,
			Description: "Human-readable description of what this template is for",
		},
		"sections": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type:     genai.TypeObject,
				Required: []string{"name", "slot", "position"},
				Properties: map[string]*genai.Schema{
					"name": {
						Type:        genai.TypeString,
						Description: "Human-readable section name, e.g. Hero, Services, Testimonials",
					},
					"slot": {
						Type:        genai.TypeString,
						Description: "Slot identifier in kebab-case, e.g. hero, about-us, services",
					},
					"html_selector": {
						Type:        genai.TypeString,
						Description: "CSS selector that targets this section in the HTML",
					},
					"semantic_type": {
						Type:        genai.TypeString,
						Description: "Schema.org type if applicable: FAQPage, Product, Review, Organization, etc.",
					},
					"position": {
						Type:        genai.TypeInteger,
						Description: "Zero-based position of this section in page order",
					},
					"content_slots": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type:     genai.TypeObject,
							Required: []string{"name", "slot", "content_type"},
							Properties: map[string]*genai.Schema{
								"name": {
									Type:        genai.TypeString,
									Description: "Human-readable slot name: Headline, Description, Image",
								},
								"slot": {
									Type:        genai.TypeString,
									Description: "Dot-notation slot path: hero.headline, hero.image",
								},
								"html_selector": {
									Type:        genai.TypeString,
									Description: "CSS selector targeting this content slot",
								},
								"content_type": {
									Type:        genai.TypeString,
									Description: "Content type: text, image, link, rich_text, list, video, icon",
								},
								"required": {
									Type:        genai.TypeBoolean,
									Description: "Whether this content slot is essential to the section",
								},
							},
						},
					},
				},
			},
		},
		"navigation": {
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"items": {
					Type: genai.TypeArray,
					Items: &genai.Schema{
						Type:     genai.TypeObject,
						Required: []string{"label", "href"},
						Properties: map[string]*genai.Schema{
							"label": {Type: genai.TypeString, Description: "Navigation link text"},
							"href":  {Type: genai.TypeString, Description: "Navigation link href"},
						},
					},
				},
			},
		},
	},
}

const templateExtractionInstruction = `You are a template structure analyst for the WeOS website system. Your task is to analyze raw HTML template files and extract their structural metadata.

For each HTML template, identify:

1. **Page Type**: Classify the template (homepage, about, contact, blog, portfolio, services, pricing, faq, product, landing, etc.)

2. **Description**: Write a brief human-readable description of what this template is designed for.

3. **Sections**: Identify the major visual sections of the page in order. For each section:
   - name: Human-readable name (e.g. "Hero", "Services", "Testimonials")
   - slot: Kebab-case identifier (e.g. "hero", "services", "testimonials")
   - html_selector: CSS selector to target the section element
   - semantic_type: Schema.org type if applicable (e.g. "FAQPage", "Product", "Review", "Organization")
   - position: Zero-based position in page order
   - content_slots: Individual editable content areas within the section. For each slot:
     - name: Human-readable name (e.g. "Headline", "Description")
     - slot: Dot-notation path using section.slot format (e.g. "hero.headline", "services.description")
     - html_selector: CSS selector within the section
     - content_type: One of text, image, link, rich_text, list, video, icon
     - required: Whether this content is essential to the section

4. **Navigation**: Extract navigation menu items with label and href.

Guidelines:
- Look for semantic HTML elements (header, nav, main, section, footer, article, aside)
- Use class names and IDs as hints for section purposes
- Identify repeating patterns (cards, grid items) as list-type content slots
- Consider common template patterns: hero sections, feature grids, testimonials, CTAs, pricing tables
- For Schema.org types, only assign when there is a clear semantic match

Return a JSON object with page_type, description, sections, and navigation.`

// NewTemplateExtractionAgent creates the template extraction ADK agent.
func NewTemplateExtractionAgent(
	adkCfg *infraAgents.ADKConfig, logger entities.Logger,
) (agent.Agent, error) {
	ctx := context.Background()

	m, err := adkCfg.CreateGeminiModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model for template extraction: %w", err)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        "template_extraction_agent",
		Model:       m,
		Description: "Analyzes HTML templates and extracts structural metadata",
		Instruction: templateExtractionInstruction,
		OutputKey:   "template_analysis",
		GenerateContentConfig: &genai.GenerateContentConfig{
			ResponseSchema:   templateExtractionResponseSchema,
			ResponseMIMEType: "application/json",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create template extraction agent: %w", err)
	}

	return a, nil
}

// ProvideTemplateExtractionAgent is the Fx provider. Returns nil when ADK is not configured.
func ProvideTemplateExtractionAgent(params struct {
	fx.In
	ADKConfig *infraAgents.ADKConfig `optional:"true"`
	Logger    entities.Logger
}) (agent.Agent, error) {
	if params.ADKConfig == nil {
		params.Logger.Info(context.Background(),
			"ADK not configured, template extraction agent disabled")
		return nil, nil
	}
	return NewTemplateExtractionAgent(params.ADKConfig, params.Logger)
}

// TemplateAnalysis holds the parsed result from the template extraction agent.
type TemplateAnalysis struct {
	PageType    string            `json:"page_type"`
	Description string            `json:"description"`
	Sections    []SectionAnalysis `json:"sections"`
	Navigation  NavigationAnalysis `json:"navigation"`
}

// SectionAnalysis describes a single section found in the template.
type SectionAnalysis struct {
	Name         string        `json:"name"`
	Slot         string        `json:"slot"`
	HTMLSelector string        `json:"html_selector"`
	SemanticType string        `json:"semantic_type"`
	Position     int           `json:"position"`
	ContentSlots []ContentSlot `json:"content_slots"`
}

// ContentSlot describes an editable content area within a section.
type ContentSlot struct {
	Name         string `json:"name"`
	Slot         string `json:"slot"`
	HTMLSelector string `json:"html_selector"`
	ContentType  string `json:"content_type"`
	Required     bool   `json:"required"`
}

// NavigationAnalysis holds extracted navigation items.
type NavigationAnalysis struct {
	Items []NavItem `json:"items"`
}

// NavItem represents a single navigation link.
type NavItem struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

// TemplateExtractionHelper provides methods for running the template extraction agent.
type TemplateExtractionHelper struct {
	logger entities.Logger
}

// NewTemplateExtractionHelper creates a new helper.
func NewTemplateExtractionHelper(logger entities.Logger) *TemplateExtractionHelper {
	return &TemplateExtractionHelper{logger: logger}
}

const templateExtractionPrompt = "Analyze the following HTML template and extract its structural metadata:\n\n"

// AnalyzeTemplate runs the extraction agent on the given HTML content.
func (h *TemplateExtractionHelper) AnalyzeTemplate(
	ctx context.Context, a agent.Agent, htmlContent string,
) (*TemplateAnalysis, error) {
	prompt := templateExtractionPrompt + htmlContent

	responseText, err := RunAgent(
		ctx, a, DefaultAppName,
		"template-analyzer", "template-extraction",
		prompt, templateExtractionResponseSchema,
	)
	if err != nil {
		return nil, fmt.Errorf("run template extraction agent: %w", err)
	}

	return parseResponse(responseText)
}

// parseResponse parses the agent's JSON response into a TemplateAnalysis.
func parseResponse(response string) (*TemplateAnalysis, error) {
	response = strings.TrimSpace(response)

	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		response = strings.TrimSuffix(response, "```")
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		response = strings.TrimSuffix(response, "```")
	}
	response = strings.TrimSpace(response)

	startIdx := strings.Index(response, "{")
	endIdx := strings.LastIndex(response, "}")
	if startIdx < 0 || endIdx < startIdx {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	response = response[startIdx : endIdx+1]

	var result TemplateAnalysis
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("failed to parse template analysis JSON: %w", err)
	}

	return &result, nil
}

// ToEntitySections converts the analysis sections to domain entity types.
func (a *TemplateAnalysis) ToEntitySections() []entities.TemplateSection {
	sections := make([]entities.TemplateSection, len(a.Sections))
	for i, s := range a.Sections {
		slots := make([]entities.TemplateContentSlot, len(s.ContentSlots))
		for j, cs := range s.ContentSlots {
			slots[j] = entities.TemplateContentSlot{
				Name:         cs.Name,
				Slot:         cs.Slot,
				ContentType:  cs.ContentType,
				HTMLSelector: cs.HTMLSelector,
				Required:     cs.Required,
			}
		}
		sections[i] = entities.TemplateSection{
			Name:         s.Name,
			Slot:         s.Slot,
			HTMLSelector: s.HTMLSelector,
			SemanticType: s.SemanticType,
			Position:     s.Position,
			ContentSlots: slots,
		}
	}
	return sections
}

// ToEntityNavItems converts navigation items to domain entity types.
func (a *TemplateAnalysis) ToEntityNavItems() []entities.TemplateNavItem {
	items := make([]entities.TemplateNavItem, len(a.Navigation.Items))
	for i, n := range a.Navigation.Items {
		items[i] = entities.TemplateNavItem{
			Label: n.Label,
			Href:  n.Href,
		}
	}
	return items
}
