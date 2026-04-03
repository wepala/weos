package entities

import (
	"time"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// TripleCreated is emitted when a relationship triple is established between two resources.
type TripleCreated struct {
	domain.BasicTripleEvent
	Timestamp time.Time
}

func (e TripleCreated) With(subject, predicate, object string) TripleCreated {
	return TripleCreated{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   subject,
			Predicate: predicate,
			Object:    object,
		},
		Timestamp: time.Now(),
	}
}

func (e TripleCreated) EventType() string {
	return "Triple.Created"
}

// TripleDeleted is emitted when a relationship triple is removed.
type TripleDeleted struct {
	domain.BasicTripleEvent
	Timestamp time.Time
}

func (e TripleDeleted) With(subject, predicate, object string) TripleDeleted {
	return TripleDeleted{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   subject,
			Predicate: predicate,
			Object:    object,
		},
		Timestamp: time.Now(),
	}
}

func (e TripleDeleted) EventType() string {
	return "Triple.Deleted"
}
