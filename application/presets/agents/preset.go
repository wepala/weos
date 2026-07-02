// Package agents provides the resource types for in-app agents: declarative
// agent-skill definitions that a WeOS app's orchestrator turns into runnable
// ADK sub-agents. Adding a skill is a data change — publish an agent-skill
// resource — never a code change.
package agents

import (
	"encoding/json"

	"github.com/wepala/weos/v3/application"
)

// agentSkillContext types skills in a small agents vocabulary; name and
// description ride schema.org so generic tooling renders them.
const agentSkillContext = `{"@vocab":"https://schema.org/",` +
	`"ag":"https://weos.org/vocab/agents#",` +
	`"@type":"ag:AgentSkill",` +
	`"schemaVersion":"https://schema.org/version",` +
	`"name":"https://schema.org/name",` +
	`"description":"https://schema.org/description",` +
	`"instructions":"https://weos.org/vocab/agents#instructions",` +
	`"tools":"https://weos.org/vocab/agents#tools",` +
	`"mode":"https://weos.org/vocab/agents#mode",` +
	`"widgets":"https://weos.org/vocab/agents#widgets",` +
	`"model":"https://weos.org/vocab/agents#model"}`

// agentSkillSchema is versioned via schemaVersion: v1 describes a single
// agent (instructions + tool allowlist + mode). Composed multi-step skills
// are a future schemaVersion, not a rearchitecture (epic #397 boundary).
const agentSkillSchema = `{"type":"object","properties":{` +
	`"schemaVersion":{"type":"integer","minimum":1,` +
	`"description":"Skill definition version; 1 = single-agent skill"},` +
	`"name":{"type":"string","description":"Unique skill name; doubles as the ADK agent name"},` +
	`"description":{"type":"string","description":"What the skill handles — the orchestrator routes on this"},` +
	`"instructions":{"type":"string","description":"System instructions for the skill agent"},` +
	`"tools":{"type":"array","items":{"type":"string"},` +
	`"description":"Tool-name allowlist; every name must exist on the instance's tool surface"},` +
	`"mode":{"type":"string","enum":["task","single_turn"],` +
	`"description":"ADK delegation mode: task converses to finish the job, single_turn answers once"},` +
	`"widgets":{"type":"array","items":{"type":"string"},` +
	`"description":"Preferred output widgets (markdown, table, list, card)"},` +
	`"model":{"type":"string","description":"Optional model ID override"}` +
	`},"required":["name","description","instructions"]}`

// researcherFixture is the generic example skill every install gets: it
// proves the declarative path end to end and is useful on any WeOS instance
// because it only touches the knowledge graph and memory — capabilities
// every instance has.
const researcherFixture = `{"schemaVersion":1,` +
	`"name":"knowledge_graph_researcher",` +
	`"description":"Answers questions about the people, organizations, and other entities this WeOS instance ` +
	`knows about, by recalling consolidated memory and searching the knowledge graph.",` +
	`"instructions":"You research what this WeOS instance knows. Prefer memory_recall for consolidated facts ` +
	`and memory_search for names and identifiers; walk the graph with kg_search_entities, kg_expand_entity, ` +
	`and kg_describe_class. Reach for kg_sparql_query only when the simpler tools cannot express the ` +
	`question. Ground every answer in what the tools returned and cite entity URNs.",` +
	`"tools":["memory_recall","memory_search","kg_search_entities","kg_expand_entity","kg_describe_class",` +
	`"kg_list_classes","kg_find_path","kg_sparql_query"],` +
	`"mode":"task",` +
	`"widgets":["markdown","table"]}`

// Register adds the agents preset to the registry.
func Register(registry *application.PresetRegistry) {
	skillType := application.NewPresetType("Agent Skill", application.AgentSkillTypeSlug,
		"A declarative agent skill: instructions, tool allowlist, and delegation mode the orchestrator "+
			"builds a sub-agent from",
		agentSkillContext, agentSkillSchema)
	skillType.Fixtures = []json.RawMessage{json.RawMessage(researcherFixture)}

	registry.MustAdd(application.PresetDefinition{
		Name:        "agents",
		Description: "In-app agent types: declarative agent skills routed to by the orchestrator",
		Types:       []application.PresetResourceType{skillType},
	})
}
