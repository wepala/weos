package projections

import (
	weos "github.com/wepala/weos/model"
)

//Projection interface that all projections should implement
type Projection interface {
	weos.Projection
}

type DefaultProjection struct {
	WEOSID     string `json:"weos_id,omitempty" gorm:"unique;<-:create"`
	SequenceNo int64  `json:"sequence_no,omitempty"`
	Table      string `json:"table_alias"`
}
