package models

// EventReference is one edge of the event-reference projection: the event
// identified by EventID references the resource identified by ResourceURN.
// Rows are derived from the event log by the "event-references" subscriber
// and are only ever inserted or truncated (for rebuild) — never updated, so
// replay is idempotent by construction.
type EventReference struct {
	EventID     string `gorm:"primaryKey;not null;column:event_id"`
	ResourceURN string `gorm:"primaryKey;not null;column:resource_urn;index:idx_event_refs_resource"`
}

func (EventReference) TableName() string {
	return "event_references"
}
