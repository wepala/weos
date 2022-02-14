package model

import (
	"golang.org/x/net/context"
	"sync"
)

type DefaultEventDisptacher struct {
	handlers        []EventHandler
	handlerPanicked bool
	dispatch        sync.Mutex
}

func (e *DefaultEventDisptacher) Dispatch(ctx context.Context, event Event) []error {
	//mutex helps keep state between routines
	var errors []error

	e.dispatch.Lock()
	defer e.dispatch.Unlock()
	var wg sync.WaitGroup
	for i := 0; i < len(e.handlers); i++ {
		handler := e.handlers[i]
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					e.handlerPanicked = true
				}
				wg.Done()
			}()

			err := handler(ctx, event)
			if err != nil {
				errors = append(errors, err)
			}

		}()
	}
	wg.Wait()
	return errors
}

func (e *DefaultEventDisptacher) AddSubscriber(handler EventHandler) {
	e.handlers = append(e.handlers, handler)
}

func (e *DefaultEventDisptacher) GetSubscribers() []EventHandler {
	return e.handlers
}

type EventHandler func(ctx context.Context, event Event) error
