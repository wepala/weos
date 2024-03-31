package rest

import (
	"encoding/json"
	"fmt"
	"go.uber.org/fx"
	"golang.org/x/net/context"
	"sync"
	"time"
)

const CREATE_COMMAND = "create"
const UPDATE_COMMAND = "update"
const DELETE_COMMAND = "delete"

type CommandDispatcherParams struct {
	fx.In
	CommandConfigs []CommandConfig `group:"commandHandlers"`
	Logger         Log
}

type CommandDispatcherResult struct {
	fx.Out
	Dispatcher CommandDispatcher
}

// NewCommandDispatcher creates a new command dispatcher and registers all the command handlers
func NewCommandDispatcher(p CommandDispatcherParams) CommandDispatcherResult {
	dispatcher := &DefaultCommandDispatcher{
		handlers: make(map[string][]CommandHandler),
	}
	for _, config := range p.CommandConfigs {
		dispatcher.AddSubscriber(config)
	}
	return CommandDispatcherResult{
		Dispatcher: dispatcher,
	}
}

type DefaultCommandDispatcher struct {
	handlers        map[string][]CommandHandler
	handlerPanicked bool
	dispatch        sync.Mutex
}

func (e *DefaultCommandDispatcher) Dispatch(ctx context.Context, command *Command, repository *ResourceRepository, logger Log) (interface{}, error) {
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
					fmt.Println(fmt.Sprintf("%+v", r))
					err = fmt.Errorf("handler error '%v'", r)
					logger.Errorf("handler error '%v'", r)
				}
				wg.Done()
			}()
			result, err = handler(ctx, command, repository, logger)
		}()
	}

	wg.Wait()

	return result, err
}

func (e *DefaultCommandDispatcher) AddSubscriber(command CommandConfig) map[string][]CommandHandler {
	if e.handlers == nil {
		e.handlers = map[string][]CommandHandler{}
	}
	e.handlers[command.Type+command.Resource] = append(e.handlers[command.Type+command.Resource], command.Handler)

	return e.handlers
}

func (e *DefaultCommandDispatcher) GetSubscribers() map[string][]CommandHandler {
	return e.handlers
}

// CommandConfig is a struct that holds the command type and the handler for that command
type CommandConfig struct {
	Type     string
	Resource string
	Handler  CommandHandler
}

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
