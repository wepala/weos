package model

import "github.com/wepala/weos"

type Receiver struct {
	application weos.Application
}

//Initialize sets up the command handlers
func Initialize(application weos.Application) error {
	//TODO Initialize receiver
	//receiver := &Receiver{application: application}
	//TODO add command handlers to the application's command dispatcher
	//application.Dispatcher().AddSubscriber(SomeHandler(context.Background(), SomePayload{}), receiver.SomeHandler)
	return nil
}