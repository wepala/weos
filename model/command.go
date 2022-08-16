package model

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/net/context"
)

const CREATE_COMMAND = "create"
const UPDATE_COMMAND = "update"
const DELETE_COMMAND = "delete"

//Command is a common interface that all incoming requests should implement.
type Command struct {
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	Metadata CommandMetadata `json:"metadata"`
}

type CommandMetadata struct {
	EntityID      string
	SequenceNo    int
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

func (e *DefaultCommandDispatcher) Dispatch(ctx context.Context, command *Command, container Container, repository EntityRepository, logger Log) (interface{}, error) {
	//mutex helps keep state between routines
	e.dispatch.Lock()
	defer e.dispatch.Unlock()
	var wg sync.WaitGroup
	var err error
	var result interface{}
	var allHandlers []CommandHandler
	//first preference is handlers for specific command type and entity type
	if handlers, ok := e.handlers[command.Type+command.Metadata.EntityType]; ok {
		allHandlers = append(allHandlers, handlers...)
	}
	//if there are no handler then let's fall back to checking just handlers for the command type.
	if len(allHandlers) == 0 {
		if handlers, ok := e.handlers[command.Type]; ok {
			allHandlers = append(allHandlers, handlers...)
		}
	}
	//lets see if there are any global handlers and add those
	if globalHandlers, ok := e.handlers["*"]; ok {
		allHandlers = append(allHandlers, globalHandlers...)
	}

	for i := 0; i < len(allHandlers); i++ {
		handler := allHandlers[i]
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					e.handlerPanicked = true
					err = fmt.Errorf("handler error '%v'", r)
				}
				wg.Done()
			}()
			result, err = handler(ctx, command, container, repository, logger)
		}()
	}

	wg.Wait()

	return result, err
}

func (e *DefaultCommandDispatcher) AddSubscriber(command *Command, handler CommandHandler) map[string][]CommandHandler {
	if e.handlers == nil {
		e.handlers = map[string][]CommandHandler{}
	}
	e.handlers[command.Type+command.Metadata.EntityType] = append(e.handlers[command.Type+command.Metadata.EntityType], handler)

	return e.handlers
}

func (e *DefaultCommandDispatcher) GetSubscribers() map[string][]CommandHandler {
	return e.handlers
}

type CommandHandler func(ctx context.Context, command *Command, container Container, repository EntityRepository, logger Log) (interface{}, error)
