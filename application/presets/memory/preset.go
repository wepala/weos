// Package memory provides resource types for agent memory: consolidated facts
// distilled from episodic events, carrying PROV-O provenance and supersession.
package memory

import (
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

// factContext maps the fact type's terms to PROV-O as bare full-IRI strings —
// the storable context sent to the knowledge graph only preserves that form.
// It is shared with the fact behavior (behavior.go), which resolves the
// wasRevisionOf edge against it. rdfs:subClassOf is deliberately not declared:
// projection machinery treats that key as a parent type slug, not an external
// ontology class.
const factContext = `{"@vocab":"https://schema.org/",` +
	`"mem":"https://weos.org/vocab/memory#",` +
	`"@type":"mem:Fact",` +
	`"statement":"https://schema.org/text",` +
	`"about":"https://schema.org/about",` +
	`"confidence":"https://weos.org/vocab/memory#confidence",` +
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
		},
		Behaviors: map[string]application.BehaviorFactory{
			"fact": FactBehavior,
		},
		BehaviorMeta: map[string]entities.BehaviorMeta{
			"fact": {
				Slug:        "fact",
				DisplayName: "Fact provenance signals",
				Description: "Records Fact.Recorded and Fact.Superseded signal events when facts are committed",
				Default:     true,
				Manageable:  false,
			},
		},
	})
}
