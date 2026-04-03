package models

import "time"

// Triple stores an RDF triple relationship in the database.
type Triple struct {
	Subject   string    `gorm:"primaryKey;not null;index:idx_triples_sub;index:idx_triples_sp"`
	Predicate string    `gorm:"primaryKey;not null;index:idx_triples_sp;index:idx_triples_po"`
	Object    string    `gorm:"primaryKey;not null;index:idx_triples_obj;index:idx_triples_po"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (Triple) TableName() string {
	return "triples"
}
