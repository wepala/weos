package projections

import (
	weos "github.com/wepala/weos-content-service/model"
)

//Projection interface that all projections should implement
type Projection interface {
	weos.Projection
}
