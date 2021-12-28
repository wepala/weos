package model

import (
	"encoding/json"
	"golang.org/x/net/context"
)

type DomainService struct {
	Repository
	eventRepository EventRepository
}

func (s *DomainService) Create(ctx context.Context, payload json.RawMessage, entityType string) (*AmorphousEntity, error) {
	//TODO take the validation rules from context
	//TODO Must return something that implements aggregrate interface
	return nil, nil
}

func (s *DomainService) CreateBatch(ctx context.Context, payload json.RawMessage, entityType string) ([]*AmorphousEntity, error) {
	//TODO take the validation rules from context
	return nil, nil
}

func NewDomainService(ctx context.Context, eventRepository EventRepository) *DomainService {
	return &DomainService{
		eventRepository: eventRepository,
	}
}
