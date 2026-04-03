package application

import (
	"context"
	"fmt"
	"time"

	"weos/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/segmentio/ksuid"
	"go.uber.org/fx"
)

// TripleService manages RDF triple relationships between resources.
type TripleService interface {
	Link(ctx context.Context, subject, predicate, object string) error
	Unlink(ctx context.Context, subject, predicate, object string) error
}

type tripleService struct {
	dispatcher *domain.EventDispatcher
	logger     entities.Logger
}

func ProvideTripleService(params struct {
	fx.In
	Dispatcher *domain.EventDispatcher
	Logger     entities.Logger
}) TripleService {
	return &tripleService{
		dispatcher: params.Dispatcher,
		logger:     params.Logger,
	}
}

//nolint:dupl // Link and Unlink are intentionally parallel
func (s *tripleService) Link(ctx context.Context, subject, predicate, object string) error {
	event := entities.TripleCreated{}.With(subject, predicate, object)
	envelope := domain.EventEnvelope[any]{
		ID:         ksuid.New().String(),
		EventType:  event.EventType(),
		Payload:    event,
		Created:    time.Now(),
		SequenceNo: 0,
	}
	if err := s.dispatcher.Dispatch(ctx, envelope); err != nil {
		return fmt.Errorf("failed to dispatch Triple.Created: %w", err)
	}
	s.logger.Info(ctx, "triple linked",
		"subject", subject, "predicate", predicate, "object", object)
	return nil
}

//nolint:dupl // Link and Unlink are intentionally parallel
func (s *tripleService) Unlink(ctx context.Context, subject, predicate, object string) error {
	event := entities.TripleDeleted{}.With(subject, predicate, object)
	envelope := domain.EventEnvelope[any]{
		ID:         ksuid.New().String(),
		EventType:  event.EventType(),
		Payload:    event,
		Created:    time.Now(),
		SequenceNo: 0,
	}
	if err := s.dispatcher.Dispatch(ctx, envelope); err != nil {
		return fmt.Errorf("failed to dispatch Triple.Deleted: %w", err)
	}
	s.logger.Info(ctx, "triple unlinked",
		"subject", subject, "predicate", predicate, "object", object)
	return nil
}
