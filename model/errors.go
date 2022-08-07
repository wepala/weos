package model

import "errors"

var EntityNotFound = errors.New("entity not found")

//goland:noinspection GoNameStartsWithPackageName
type WeOSError struct {
	message     string
	err         error
	Application string
	AccountID   string
}

func (e *WeOSError) Error() string {
	return e.message
}

func (e *WeOSError) Unwrap() error {
	return e.err
}

type DomainError struct {
	*WeOSError
	EntityID   string
	EntityType string
}

func NewError(message string, err error) *WeOSError {
	return &WeOSError{
		message: message,
		err:     err,
	}
}

func NewDomainError(message string, entityType string, entityID string, err error) *DomainError {
	return &DomainError{
		WeOSError:  NewError(message, err),
		EntityID:   entityID,
		EntityType: entityType,
	}
}
