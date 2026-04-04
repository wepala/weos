package repositories

import (
	"context"
	"time"
)

// Triple represents an RDF triple (subject-predicate-object) relationship.
type Triple struct {
	Subject   string
	Predicate string
	Object    string
	CreatedAt time.Time
}

// TripleRepository manages RDF triple relationships between resources and entities.
type TripleRepository interface {
	SaveTriple(ctx context.Context, subject, predicate, object string) error
	DeleteTriple(ctx context.Context, subject, predicate, object string) error
	DeleteBySubject(ctx context.Context, subject string) error
	DeleteBySubjectAndPredicate(ctx context.Context, subject, predicate string) error
	FindBySubject(ctx context.Context, subject string) ([]Triple, error)
	FindByObject(ctx context.Context, object string) ([]Triple, error)
	FindBySubjectAndPredicate(ctx context.Context, subject, predicate string) ([]Triple, error)
	FindByPredicateAndObject(ctx context.Context, predicate, object string) ([]Triple, error)
}
