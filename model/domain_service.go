package model

import (
	"encoding/json"
	weosContext "github.com/wepala/weos-service/context"
	"golang.org/x/net/context"
)

type DomainService struct {
	Repository
	eventRepository EventRepository
}

func (s *DomainService) Create(ctx context.Context, payload json.RawMessage, entityType string) (ContentAggregateInterface, error) {

	contentType := weosContext.GetContentType(ctx)
	entity, err := new(ContentAggregateRoot).FromSchema(ctx, contentType.Schema)
	if err != nil {
		return nil, NewDomainError("unexpected error creating entity", entityType, "", err)
	}
	if entity == nil {
		return nil, NewDomainError("expected entity to be created but got none", entityType, "", nil)
	}

	return nil, nil
}

func (s *DomainService) CreateBatch(ctx context.Context, payload json.RawMessage, entityType string) ([]ContentAggregateInterface, error) {
	//TODO take the validation rules from context
	return nil, nil
}

func NewDomainService(ctx context.Context, eventRepository EventRepository) *DomainService {
	return &DomainService{
		eventRepository: eventRepository,
	}
}
