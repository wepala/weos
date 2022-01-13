package model

import (
	"context"
	"encoding/json"

	weoscontext "github.com/wepala/weos-service/context"
)

func Create(ctx context.Context, payload json.RawMessage, entityType string, entityID string) *Command {

	command := &Command{
		Type:    "create",
		Payload: payload,
		Metadata: CommandMetadata{
			Version:    1,
			UserID:     weoscontext.GetUser(ctx),
			AccountID:  weoscontext.GetAccount(ctx),
			EntityType: entityType,
			EntityID:   entityID,
		},
	}
	return command
}

func CreateBatch(ctx context.Context, payload json.RawMessage, entityType string) *Command {

	command := &Command{
		Type:    "create_batch",
		Payload: payload,
		Metadata: CommandMetadata{
			Version:    1,
			UserID:     weoscontext.GetUser(ctx),
			AccountID:  weoscontext.GetAccount(ctx),
			EntityType: entityType,
		},
	}
	return command
}

func Update(ctx context.Context, payload json.RawMessage, entityType string) *Command {

	command := &Command{
		Type:    "update",
		Payload: payload,
		Metadata: CommandMetadata{
			Version:    1,
			UserID:     weoscontext.GetUser(ctx),
			AccountID:  weoscontext.GetAccount(ctx),
			EntityType: entityType,
		},
	}
	return command
}
