package model

type Receiver struct {
	application Service
}

//Initialize sets up the command handlers
func Initialize(application Service) error {
	//TODO Initialize receiver
	//receiver := &Receiver{application: application}
	//TODO add command handlers to the application's command dispatcher
	//application.Dispatcher().AddSubscriber(SomeHandler(context.Background(), SomePayload{}), receiver.SomeHandler)
	return nil
}
