package model

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/net/context"
)

//Command is a common interface that all incoming requests should implement.
type Command struct {
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	Metadata CommandMetadata `json:"metadata"`
}

type CommandMetadata struct {
	EntityID      string
	EntityType    string
	Version       int64
	ExecutionDate *time.Time
	UserID        string
	AccountID     string
}

type DefaultCommandDispatcher struct {
	handlers        map[string][]CommandHandler
	handlerPanicked bool
	dispatch        sync.Mutex
}

func (e *DefaultCommandDispatcher) Dispatch(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error {
	//mutex helps keep state between routines
	e.dispatch.Lock()
	defer e.dispatch.Unlock()
	var wg sync.WaitGroup
	var err error
	if handlers, ok := e.handlers[command.Type]; ok {
		var allHandlers []CommandHandler
		//lets see if there are any global handlers and add those
		if globalHandlers, ok := e.handlers["*"]; ok {
			allHandlers = append(allHandlers, globalHandlers...)
		}
		//now lets add the specific command handlers
		allHandlers = append(allHandlers, handlers...)

		for i := 0; i < len(allHandlers); i++ {
			handler := allHandlers[i]
			wg.Add(1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						e.handlerPanicked = true
						err = fmt.Errorf("handler error '%s'", r)
					}
					wg.Done()
				}()
				err = handler(ctx, command, eventStore, projection, logger)
			}()
		}

		wg.Wait()
	}

	return err
}

func (e *DefaultCommandDispatcher) AddSubscriber(command *Command, handler CommandHandler) map[string][]CommandHandler {
	if e.handlers == nil {
		e.handlers = map[string][]CommandHandler{}
	}
	e.handlers[command.Type] = append(e.handlers[command.Type], handler)

	return e.handlers
}

func (e *DefaultCommandDispatcher) GetSubscribers() map[string][]CommandHandler {
	return e.handlers
}

type CommandHandler func(ctx context.Context, command *Command, eventRepository EventRepository, projection Projection, logger Log) error
