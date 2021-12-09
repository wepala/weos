package projections

import (
	"github.com/wepala/weos"
)

//Projection interface that all projections should implement
type Projection interface {
	weos.Projection
}
