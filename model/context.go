package model

import (
	weoscontext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

//Get entity if it's in the context
func GetEntity(ctx context.Context) *ContentEntity {
	if value, ok := ctx.Value(weoscontext.ENTITY).(*ContentEntity); ok {
		return value
	}
	return nil
}
