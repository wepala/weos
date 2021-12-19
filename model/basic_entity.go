package model

type BasicEntity struct {
	entityErrors []error
	ID           string `json:"id,omitempty"`
}

func (b *BasicEntity) IsValid() bool {
	return len(b.entityErrors) == 0
}

func (b *BasicEntity) AddError(err error) {
	b.entityErrors = append(b.entityErrors, err)
}

func (b *BasicEntity) GetID() string {
	return b.ID
}

func (b *BasicEntity) GetErrors() []error {
	return b.entityErrors
}
