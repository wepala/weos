package entities

import (
	"time"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// TripleCreated is emitted when a relationship triple is established between two resources.
// AccountID carries the owning account (the subject resource's account) so the
// background knowledge-graph projector can route the triple to that account's
// store without a separate history lookup.
type TripleCreated struct {
	domain.BasicTripleEvent
	AccountID string
	Timestamp time.Time
}

func (e TripleCreated) With(subject, predicate, object, accountID string) TripleCreated {
	return TripleCreated{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   subject,
			Predicate: predicate,
			Object:    object,
		},
		AccountID: accountID,
		Timestamp: time.Now(),
	}
}

func (e TripleCreated) EventType() string {
	return "Triple.Created"
}

// TripleDeleted is emitted when a relationship triple is removed.
type TripleDeleted struct {
	domain.BasicTripleEvent
	AccountID string
	Timestamp time.Time
}

func (e TripleDeleted) With(subject, predicate, object, accountID string) TripleDeleted {
	return TripleDeleted{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   subject,
			Predicate: predicate,
			Object:    object,
		},
		AccountID: accountID,
		Timestamp: time.Now(),
	}
}

func (e TripleDeleted) EventType() string {
	return "Triple.Deleted"
}
