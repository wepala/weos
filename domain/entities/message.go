package entities

import "context"

// Message represents a non-fatal notification that can be surfaced to the UI.
// Services accumulate messages on the request context via AddMessage; handlers
// extract them with GetMessages and include them in the response envelope.
type Message struct {
	Type  string `json:"type"`            // "error", "warning", "info", "success"
	Text  string `json:"text"`
	Field string `json:"field,omitempty"` // optional: ties message to a specific field
	Code  string `json:"code,omitempty"`  // optional: machine-readable code for the UI
}

// messagesKeyType is an unexported type used as the context key for messages.
type messagesKeyType struct{}

// messagesKey is the context key. Using a private struct prevents collisions.
var messagesKey = &messagesKeyType{}

// ContextWithMessages returns a new context that carries a message accumulator.
// Call this once per request (typically in middleware) before any service code runs.
func ContextWithMessages(ctx context.Context) context.Context {
	msgs := make([]Message, 0)
	return context.WithValue(ctx, messagesKey, &msgs)
}

// AddMessage appends a message to the context's accumulator.
// It is a no-op if the context does not carry a message accumulator.
func AddMessage(ctx context.Context, msg Message) {
	if ptr, ok := ctx.Value(messagesKey).(*[]Message); ok && ptr != nil {
		*ptr = append(*ptr, msg)
	}
}

// GetMessages returns the messages accumulated on the context, or nil if none.
func GetMessages(ctx context.Context) []Message {
	ptr, ok := ctx.Value(messagesKey).(*[]Message)
	if !ok || ptr == nil || len(*ptr) == 0 {
		return nil
	}
	return *ptr
}
