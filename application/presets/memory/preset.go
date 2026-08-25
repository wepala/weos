// Package memory provides resource types for agent memory: episodic notes,
// plus consolidated facts distilled from them carrying PROV-O provenance and
// supersession, and playbooks with event-sourced outcome counters.
package memory

import (
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// factContext maps the fact type's terms to PROV-O as bare full-IRI strings —
// the storable context sent to the knowledge graph only preserves that form.
// It is shared with the fact behavior (behavior.go), which resolves the
// wasRevisionOf edge against it. rdfs:subClassOf is deliberately not declared:
// projection machinery treats that key as a parent type slug, not an external
// ontology class.
const factContext = `{"@vocab":"https://schema.org/",` +
	`"mem":"` + jsonld.MemoryVocab + `",` +
	`"@type":"mem:Fact",` +
	`"statement":"https://schema.org/text",` +
	`"about":"https://schema.org/about",` +
	`"confidence":"` + jsonld.MemoryVocab + `confidence",` +
	`"attributedTo":"http://www.w3.org/ns/prov#wasAttributedTo",` +
	`"generatedAtTime":"http://www.w3.org/ns/prov#generatedAtTime",` +
	`"wasDerivedFrom":"http://www.w3.org/ns/prov#wasDerivedFrom",` +
	`"wasRevisionOf":"http://www.w3.org/ns/prov#wasRevisionOf",` +
	`"invalidatedAtTime":"http://www.w3.org/ns/prov#invalidatedAtTime"}`

const factSchema = `{"type":"object","properties":{` +
	`"statement":{"type":"string","description":"The fact as one declarative sentence"},` +
	`"about":{"type":"string","description":"URN of the entity this fact concerns"},` +
	`"confidence":{"type":"number","minimum":0,"maximum":1},` +
	`"attributedTo":{"type":"string","description":"Agent or model that consolidated the fact"},` +
	`"generatedAtTime":{"type":"string","format":"date-time"},` +
	`"wasDerivedFrom":{"type":"array","items":{"type":"string"},` +
	`"description":"Source event IDs (urn:event:<id>) this fact was distilled from"},` +
	`"wasRevisionOf":{"type":"string","x-resource-type":"fact","x-display-property":"statement"},` +
	`"invalidatedAtTime":{"type":"string","format":"date-time",` +
	`"description":"Set when a newer fact supersedes this one; superseded facts are excluded from recall"}` +
	`},"required":["statement"]}`

// playbookContext models procedural memory: a learned procedure whose
// success/failure record accrues via events. Counters live in the custom
// memory vocabulary; ranking by them is deferred until signal density
// justifies it (epic #386 boundary).
const playbookContext = `{"@vocab":"https://schema.org/",` +
	`"mem":"` + jsonld.MemoryVocab + `",` +
	`"@type":"mem:Playbook",` +
	`"trigger":"` + jsonld.MemoryVocab + `triggerCondition",` +
	`"steps":"` + jsonld.MemoryVocab + `steps",` +
	`"successCount":"` + jsonld.MemoryVocab + `successCount",` +
	`"failureCount":"` + jsonld.MemoryVocab + `failureCount"}`

const playbookSchema = `{"type":"object","properties":{` +
	`"name":{"type":"string","description":"Short name of the procedure"},` +
	`"description":{"type":"string"},` +
	`"trigger":{"type":"string","description":"The situation in which to reach for this playbook"},` +
	`"steps":{"type":"array","items":{"type":"string"},"description":"Ordered procedure steps"},` +
	`"successCount":{"type":"integer","minimum":0},` +
	`"failureCount":{"type":"integer","minimum":0}` +
	`},"required":["name"]}`

// noteContext models the preset's episodic input: unstructured free-text
// observations (meeting notes, conversation fragments, things an agent was
// told). Notes are the only memory-preset type consolidation reads FROM —
// structured domain types already state their knowledge as typed resources
// and graph triples, so distilling facts from them would only duplicate it.
const noteContext = `{"@vocab":"https://schema.org/",` +
	`"@type":"NoteDigitalDocument",` +
	`"name":"https://schema.org/name",` +
	`"content":"https://schema.org/text",` +
	`"about":"https://schema.org/about"}`

const noteSchema = `{"type":"object","properties":{` +
	`"name":{"type":"string","description":"Optional short title"},` +
	`"content":{"type":"string","description":"Free-text episodic content to distill facts from"},` +
	`"about":{"type":"string","description":"Optional URN of the entity this note concerns"}` +
	`},"required":["content"]}`

// Register adds the memory preset to the registry.
//
// The fact type models CoALA-style semantic memory: each fact is an ordinary
// resource whose provenance and supersession ride PROV-O predicates.
// Supersession is expressed purely via resource events: a superseding fact
// references its predecessor through wasRevisionOf, and the predecessor gains
// invalidatedAtTime in a later update; superseded facts are never deleted, so
// history replays intact.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "memory",
		Description: "Agent memory types: consolidated facts with PROV-O provenance and supersession",
		Types: []application.PresetResourceType{
			application.NewPresetType("Fact", "fact",
				"An agent-consolidated statement distilled from episodic events, with PROV-O provenance",
				factContext, factSchema),
			application.NewPresetType("Playbook", "playbook",
				"A learned procedure with event-sourced success/failure counters",
				playbookContext, playbookSchema),
			application.NewPresetType("Note", "note",
				"A free-text episodic note; the unstructured input memory consolidation distills facts from",
				noteContext, noteSchema),
		},
		// Notes are the preset's own episodic input. Consolidation is
		// allowlist-only: nothing is eligible unless a preset declares it, and
		// the memory types themselves (fact, playbook) can never be.
		Consolidates: []string{"note"},
		Behaviors: map[string]application.BehaviorFactory{
			"fact":     FactBehavior,
			"playbook": PlaybookBehavior,
		},
		BehaviorMeta: map[string]entities.BehaviorMeta{
			"fact": {
				Slug:        "fact",
				DisplayName: "Fact provenance signals",
				Description: "Records Fact.Recorded and Fact.Superseded signal events when facts are committed",
				Default:     true,
				Manageable:  false,
			},
			"playbook": {
				Slug:        "playbook",
				DisplayName: "Playbook outcome signals",
				Description: "Records Playbook.Confirmed and Playbook.Rejected signal events for agent verdicts",
				Default:     true,
				Manageable:  false,
			},
		},
	})
}
