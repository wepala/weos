package events

import (
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// EventDispatcherResult holds the event dispatcher result.
type EventDispatcherResult struct {
	fx.Out
	EventDispatcher *domain.EventDispatcher
}

// ProvideEventDispatcher creates a new EventDispatcher instance.
// Event handlers should be subscribed separately using fx.Invoke or lifecycle hooks.
func ProvideEventDispatcher() EventDispatcherResult {
	return EventDispatcherResult{
		EventDispatcher: domain.NewEventDispatcher(),
	}
}
