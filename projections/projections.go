//go:generate moq -out mocks_test.go -pkg projections_test . Projection
package projections

import (
	"github.com/getkin/kin-openapi/openapi3"
	weos "github.com/wepala/weos/model"
	"golang.org/x/net/context"
	"reflect"
	"strings"
)

//Projection interface that all projections should implement
type Projection interface {
	weos.Projection
}

type DefaultProjection struct {
	WeosID     string `json:"weos_id,omitempty" gorm:"unique;<-:create"`
	SequenceNo int64  `json:"sequence_no,omitempty"`
	Table      string `json:"table_alias,omitempty"`
}

//MetaProjection makes it easier to work with multiple projections.
type MetaProjection struct {
	ordinalProjections []Projection
	projections        map[reflect.Value]int
}

//Add appends projection to the meta projection
func (m *MetaProjection) Add(projection Projection) *MetaProjection {
	if m.projections == nil {
		m.projections = make(map[reflect.Value]int)
	}
	//only add projection if it doesn't already exist
	tpoint := reflect.ValueOf(projection)
	if _, ok := m.projections[tpoint]; !ok {
		m.ordinalProjections = append(m.ordinalProjections, projection)
		m.projections[tpoint] = len(m.ordinalProjections)
	}
	return m
}

//Migrate runs migrate on all the projections and captures the errors received as a MetaError
func (m *MetaProjection) Migrate(ctx context.Context, schema *openapi3.Swagger) error {
	migrationErrors := new(MetaError)
	for _, projection := range m.ordinalProjections {
		err := projection.Migrate(ctx, schema)
		if err != nil {
			migrationErrors.Add(err)
		}
	}
	if migrationErrors.HasErrors() {
		return migrationErrors
	}
	return nil
}

//GetEventHandler returns an event handler that will trigger the event handler for all projections
func (m *MetaProjection) GetEventHandler() weos.EventHandler {
	return func(ctx context.Context, event weos.Event) error {
		handlerErrors := new(MetaError)
		for _, projection := range m.ordinalProjections {
			handler := projection.GetEventHandler()
			err := handler(ctx, event)
			if err != nil {
				handlerErrors.Add(err)
			}
		}
		if handlerErrors.HasErrors() {
			return handlerErrors
		}
		return nil
	}
}

//GetContentEntity returns the first not nil Entity
func (m *MetaProjection) GetContentEntity(ctx context.Context, entityFactory weos.EntityFactory, weosID string) (*weos.ContentEntity, error) {
	runErrors := new(MetaError)
	for _, projection := range m.ordinalProjections {
		result, err := projection.GetContentEntity(ctx, entityFactory, weosID)
		if result != nil {
			return result, err
		}
		if err != nil {
			runErrors.Add(err)
		}

	}
	if runErrors.HasErrors() {
		return nil, runErrors
	}
	return nil, nil
}

//GetByKey get entity by identifier
func (m *MetaProjection) GetByKey(ctxt context.Context, entityFactory weos.EntityFactory, identifiers map[string]interface{}) (*weos.ContentEntity, error) {
	runErrors := new(MetaError)
	for _, projection := range m.ordinalProjections {
		result, err := projection.GetByKey(ctxt, entityFactory, identifiers)
		if result != nil {
			return result, err
		}
		if err != nil {
			runErrors.Add(err)
		}

	}
	if runErrors.HasErrors() {
		return nil, runErrors
	}
	return nil, nil
}

//Deprecated: should use GetContentEntity
//GetByEntityID returns entity based on entity id
func (m *MetaProjection) GetByEntityID(ctxt context.Context, entityFactory weos.EntityFactory, id string) (map[string]interface{}, error) {
	runErrors := new(MetaError)
	for _, projection := range m.ordinalProjections {
		result, err := projection.GetByEntityID(ctxt, entityFactory, id)
		if result != nil {
			return result, err
		}
		if err != nil {
			runErrors.Add(err)
		}

	}
	if runErrors.HasErrors() {
		return nil, runErrors
	}
	return nil, nil
}

func (m *MetaProjection) GetContentEntities(ctx context.Context, entityFactory weos.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]map[string]interface{}, int64, error) {
	runErrors := new(MetaError)
	for _, projection := range m.ordinalProjections {
		result, count, err := projection.GetContentEntities(ctx, entityFactory, page, limit, query, sortOptions, filterOptions)
		if result != nil {
			return result, count, err
		}
		if err != nil {
			runErrors.Add(err)
		}

	}
	if runErrors.HasErrors() {
		return nil, 0, runErrors
	}
	return nil, 0, nil
}

func (m *MetaProjection) GetList(ctx context.Context, entityFactory weos.EntityFactory, page int, limit int, query string, sortOptions map[string]string, filterOptions map[string]interface{}) ([]*weos.ContentEntity, int64, error) {
	runErrors := new(MetaError)
	for _, projection := range m.ordinalProjections {
		result, count, err := projection.GetList(ctx, entityFactory, page, limit, query, sortOptions, filterOptions)
		if result != nil {
			return result, count, err
		}
		if err != nil {
			runErrors.Add(err)
		}

	}
	if runErrors.HasErrors() {
		return nil, 0, runErrors
	}
	return nil, 0, nil
}

//GetByProperties get
func (m *MetaProjection) GetByProperties(ctxt context.Context, entityFactory weos.EntityFactory, identifiers map[string]interface{}) ([]*weos.ContentEntity, error) {
	runErrors := new(MetaError)
	for _, projection := range m.ordinalProjections {
		result, err := projection.GetByProperties(ctxt, entityFactory, identifiers)
		if result != nil {
			return result, err
		}
		if err != nil {
			runErrors.Add(err)
		}

	}
	if runErrors.HasErrors() {
		return nil, runErrors
	}
	return nil, nil
}

//MetaError error that contains all the errors returned by the projections within a meta projection
type MetaError struct {
	errorList    []error
	errorStrings []string
}

//Add a new error to the list
func (e *MetaError) Add(err error) {
	e.errorList = append(e.errorList, err)
	e.errorStrings = append(e.errorStrings, err.Error())
}

//HasErrors if there are errors then return the MetaError otherwise returns nil
func (e *MetaError) HasErrors() bool {
	return len(e.errorList) > 0
}

//Error string representation of error
func (e *MetaError) Error() string {
	return strings.Join(e.errorStrings, ",")
}
