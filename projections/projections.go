package projections

import (
	weos "github.com/wepala/weos-service/model"
)

//Projection interface that all projections should implement
type Projection interface {
	weos.Projection
}

type DefaultProjection struct {
	Table string `json:"table_alias"`
}
