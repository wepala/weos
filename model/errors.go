package model

import (
	"errors"
	"net/http"
)

var EntityNotFound = errors.New("entity not found")

//goland:noinspection GoNameStartsWithPackageName
type WeOSError struct {
	Message     string `json:"message"`
	Code        int    `json:"code"`
	err         error
	Application string `json:"application"`
	AccountID   string `json:"accountId"`
}

func (e *WeOSError) Error() string {
	return e.Message
}

func (e *WeOSError) Unwrap() error {
	return e.err
}

type DomainError struct {
	*WeOSError
	EntityID   string `json:"entityId"`
	EntityType string `json:"entityType"`
}

func NewError(message string, err error) *WeOSError {
	return &WeOSError{
		Message: message,
		err:     err,
	}
}

func NewDomainError(message string, entityType string, entityID string, err error) *DomainError {
	terror := &DomainError{
		WeOSError:  NewError(message, err),
		EntityID:   entityID,
		EntityType: entityType,
	}
	terror.Code = http.StatusBadRequest
	return terror
}
