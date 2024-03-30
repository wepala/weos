package rest

import (
	"encoding/json"
	"go.uber.org/fx"
	"golang.org/x/net/context"
	"sync"
)

type EventDispatcherParams struct {
	fx.In
	EventConfigs []EventHandlerConfig `group:"eventHandlers"`
}

type EventDispatcherResult struct {
	fx.Out
	Dispatcher EventDispatcher
}

func NewEventDispatcher(p EventDispatcherParams) EventDispatcherResult {
	dispatcher := &DefaultEventDisptacher{
		handlers: make(map[string][]EventHandler),
	}
	for _, config := range p.EventConfigs {
		dispatcher.AddSubscriber(config)
	}
	return EventDispatcherResult{}
}

type EventHandlerConfig struct {
	ResourceType string
	Type         string
	Handler      EventHandler
}

type DefaultEventDisptacher struct {
	handlers        map[string][]EventHandler
	handlerPanicked bool
}

func (e *DefaultEventDisptacher) Dispatch(ctx context.Context, event Event) []error {
	//mutex helps keep state between routines
	var errors []error
	var wg sync.WaitGroup
	if handlers, ok := e.handlers[event.Type]; ok {
		for i := 0; i < len(handlers); i++ {
			//handler := handlers[i]
			wg.Add(1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						e.handlerPanicked = true
					}
					wg.Done()
				}()

				//err := handler(ctx, event)
				//if err != nil {
				//	errors = append(errors, err)
				//}

			}()
		}
		wg.Wait()
	}

	return errors
}

func (e *DefaultEventDisptacher) AddSubscriber(config EventHandlerConfig) {
	if e.handlers == nil {
		e.handlers = map[string][]EventHandler{}
	}
	e.handlers[config.Type] = append(e.handlers[config.Type], config.Handler)
}

func (e *DefaultEventDisptacher) GetSubscribers() map[string][]EventHandler {
	return e.handlers
}

type Event struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	Meta    EventMeta       `json:"meta"`
	Version int             `json:"version"`
	errors  []error
}

type EventMeta struct {
	ResourceID    string `json:"resourceId"`
	ResourceType  string `json:"resourceType"`
	SequenceNo    int64  `json:"sequenceNo"`
	User          string `json:"user"`
	ApplicationID string `json:"applicationId"`
	RootID        string `json:"rootId"`
	AccountID     string `json:"accountId"`
	Created       string `json:"created"`
}
