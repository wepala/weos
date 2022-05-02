package model

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/segmentio/ksuid"
	weosContext "github.com/wepala/weos/context"
	utils "github.com/wepala/weos/utils"
	"golang.org/x/net/context"
	"strings"
	"time"
)

type ContentEntity struct {
	AggregateRoot
	Schema  *openapi3.Schema
	payload map[string]interface{}
}

//IsValid checks if the property is valid using the IsNull function
func (w *ContentEntity) IsValid() bool {
	if w.payload == nil {
		return false
	}
	for _, req := range w.Schema.Required {
		if w.IsNull(req) && !w.Schema.Properties[req].Value.Nullable {
			message := "entity property " + req + " required"
			w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
			return false
		}
	}

	EnumValid := w.IsEnumValid()

	if EnumValid == false {
		return false
	}
	return true
}

//IsEnumValid this loops over the properties of an entity, if the enum is not nil, it will validate if the option the user set is valid
func (w *ContentEntity) IsEnumValid() bool {
	for k, property := range w.Schema.Properties {
		nullFound := false
		//used to indicate if the value passed is part of the enumeration
		enumFound := false

		if property.Value.Enum != nil {
			var value interface{}
			var ok bool

			if value, ok = w.payload[k]; !ok && !property.Value.Nullable {
				message := "this content entity does not contain the field: " + strings.Title(k)
				w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
				return false
			}
			//if the property is nullable and one of the values is "null" then allow nil
			if property.Value.Nullable {
				for _, option := range property.Value.Enum {
					if val, ok := option.(string); ok && val == "null" {
						nullFound = true
						break
					}
				}
			}

			//if it's nullable and the value is nil then we're good
			enumFound = nullFound && value == nil
			if !enumFound {
				switch property.Value.Type {
				case "string":
					if property.Value.Format == "date-time" {
						for _, v := range property.Value.Enum {
							if val, ok := v.(string); ok {
								currTime, _ := time.Parse("2006-01-02T15:04:00Z", val)
								enumFound = value.(time.Time) == currTime
							}
						}
					}
				case "integer":
					for _, v := range property.Value.Enum {
						if val, ok := v.(int); ok {
							enumFound = val == (int(value.(float64)))
						}
					}
				case "number":
					for _, v := range property.Value.Enum {
						if val, ok := v.(float64); ok {
							enumFound = fmt.Sprintf("%.3f", val) == fmt.Sprintf("%.3f", value.(float64))
						}
					}
				}
			}
		}
	}
	return true
}

//IsNull checks if the value of the property is null
func (w *ContentEntity) IsNull(name string) bool {
	if val, ok := w.payload[name]; ok {
		return val == nil
	}
	return true
}

func (w *ContentEntity) Init(ctx context.Context, payload json.RawMessage) (*ContentEntity, error) {
	var err error
	//update default time update values based on routes
	operation, ok := ctx.Value(weosContext.OPERATION_ID).(string)
	if ok {
		payload, err = w.UpdateTime(operation, payload)
		if err != nil {
			return nil, err
		}
	}
	err = w.SetValueFromPayload(ctx, payload)
	if err != nil {
		return nil, err
	}
	err = w.GenerateID(payload)
	if err != nil {
		return nil, err
	}
	eventPayload, err := json.Marshal(w.payload)
	if err != nil {
		return nil, NewDomainError("error marshalling event payload", w.Schema.Title, w.ID, err)
	}
	event := NewEntityEvent(CREATE_EVENT, w, w.ID, eventPayload)
	if err != nil {
		return nil, err
	}
	w.NewChange(event)
	err = w.ApplyEvents([]*Event{event})
	return w, err
}

//GORMModel return model
func (w *ContentEntity) GORMModel(ctx context.Context) (interface{}, error) {
	identifiers := w.Schema.Extensions["x-identifier"]
	instance := ds.NewStruct()
	if identifiers == nil {
		name := "ID"
		instance.AddField(name, uint(0), `json:"id"`)
	}
	relations := make(map[string]string)
	for name, p := range w.Schema.Properties {
		name = strings.Title(name)
		if p.Ref != "" {
			relations[name] = strings.TrimPrefix(p.Ref, "#/components/schemas/")
		} else {
			t := p.Value.Type
			if strings.EqualFold(t, "array") {
				t2 := p.Value.Items.Value.Type
				if t2 != "object" {
					if t2 == "string" {
						//format types to be added
						if p.Value.Items.Value.Format == "date-time" {
							instance.AddField(name, time.Now(), `json:"`+utils.SnakeCase(name)+`"`)
						} else {
							instance.AddField(name, []*string{}, `json:"`+utils.SnakeCase(name)+`"`)
						}
					} else if t2 == "number" {
						instance.AddField(name, []*float64{}, `json:"`+utils.SnakeCase(name)+`"`)
					} else if t == "integer" {
						instance.AddField(name, []*int{}, `json:"`+utils.SnakeCase(name)+`"`)
					} else if t == "boolean" {
						instance.AddField(name, []*bool{}, `json:"`+utils.SnakeCase(name)+`"`)
					}
				} else {
					if p.Value.Items.Ref == "" {
						//add as json object
					} else {
						//add reference to the object to the map
						relations[name] = "[]" + strings.TrimPrefix(p.Value.Items.Ref, "#/components/schemas/") + "{}"

					}
				}

			} else if strings.EqualFold(t, "object") {
				//add json object

			} else {
				if t == "string" {
					//format types to be added
					if p.Value.Format == "date-time" {
						var t *time.Time
						instance.AddField(name, t, `json:"`+utils.SnakeCase(name)+`"`)
					} else {
						var strings *string
						instance.AddField(name, strings, `json:"`+utils.SnakeCase(name)+`"`)
					}
				} else if t == "number" {
					var numbers *float32
					instance.AddField(name, numbers, `json:"`+utils.SnakeCase(name)+`"`)
				} else if t == "integer" {
					var integers *int
					instance.AddField(name, integers, `json:"`+utils.SnakeCase(name)+`"`)
				} else if t == "boolean" {
					var boolean *bool
					instance.AddField(name, boolean, `json:"`+utils.SnakeCase(name)+`"`)
				}
			}
		}
	}

	return instance.Build().New(), nil
}

//FromSchema builds properties from the schema
func (w *ContentEntity) FromSchema(ctx context.Context, ref *openapi3.Schema) (*ContentEntity, error) {
	w.User.ID = weosContext.GetUser(ctx)
	w.Schema = ref
	//create map using properties and default values in schema
	if w.payload == nil {
		w.payload = make(map[string]interface{})
	}
	for _, property := range w.Schema.Properties {
		w.payload[property.Value.Title] = nil
		if property.Value.Default != nil {
			w.payload[property.Value.Title] = property.Value.Default
		}
	}
	return w, nil
}

//FromSchemaWithValues builds properties from schema and unmarshall payload into it
func (w *ContentEntity) FromSchemaWithValues(ctx context.Context, schema *openapi3.Schema, payload json.RawMessage) (*ContentEntity, error) {
	w.FromSchema(ctx, schema)

	weosID, err := GetIDfromPayload(payload)
	if err != nil {
		return w, NewDomainError("unexpected error unmarshalling payload", w.Schema.Title, w.ID, err)
	}

	if w.ID == "" {
		w.ID = weosID
	}
	payload, err = ParseToType(payload, schema)
	if err != nil {
		return w, NewDomainError("unexpected error unmarshalling payload", w.Schema.Title, w.ID, err)
	}
	event := NewEntityEvent("create", w, w.ID, payload)
	w.NewChange(event)
	return w, w.ApplyEvents([]*Event{event})
}

func (w *ContentEntity) SetValueFromPayload(ctx context.Context, payload json.RawMessage) error {
	weosID, err := GetIDfromPayload(payload)
	if err != nil {
		return NewDomainError("unexpected error unmarshalling payload", w.Schema.Title, w.ID, err)
	}

	if w.ID == "" {
		w.ID = weosID
	}

	err = json.Unmarshal(payload, &w.payload)
	if err != nil {
		return NewDomainError("unexpected error unmarshalling payload", w.Schema.Title, w.ID, err)
	}

	return nil
}

func (w *ContentEntity) Update(ctx context.Context, payload json.RawMessage) (*ContentEntity, error) {
	event := NewEntityEvent("update", w, w.ID, payload)
	w.NewChange(event)
	return w, w.ApplyEvents([]*Event{event})
}

func (w *ContentEntity) Delete(deletedEntity json.RawMessage) (*ContentEntity, error) {

	event := NewEntityEvent("delete", w, w.ID, deletedEntity)
	w.NewChange(event)
	return w, w.ApplyEvents([]*Event{event})
}

//GetString returns the string property value stored of a given the property name
func (w *ContentEntity) GetString(name string) string {
	if v, ok := w.payload[name]; ok {
		if val, ok := v.(string); ok {
			return val
		}
	}
	return ""
}

//GetInteger returns the integer property value stored of a given the property name
func (w *ContentEntity) GetInteger(name string) int {
	if v, ok := w.payload[name]; ok {
		if val, ok := v.(int); ok {
			return val
		}
	}
	return 0
}

//GetUint returns the unsigned integer property value stored of a given the property name
func (w *ContentEntity) GetUint(name string) uint {
	if v, ok := w.payload[name]; ok {
		if val, ok := v.(uint); ok {
			return val
		}
	}
	return 0
}

//GetBool returns the boolean property value stored of a given the property name
func (w *ContentEntity) GetBool(name string) bool {
	if v, ok := w.payload[name]; ok {
		if val, ok := v.(bool); ok {
			return val
		}
	}
	return false
}

//GetNumber returns the float64 property value stored of a given the property name
func (w *ContentEntity) GetNumber(name string) float64 {
	if v, ok := w.payload[name]; ok {
		if val, ok := v.(float64); ok {
			return val
		}
	}
	return 0.0
}

//GetTime returns the time.Time property value stored of a given the property name
func (w *ContentEntity) GetTime(name string) time.Time {
	if v, ok := w.payload[name]; ok {
		if val, ok := v.(time.Time); ok {
			return val
		}
	}
	return time.Time{}
}

//FromSchemaWithEvents create content entity using schema and events
func (w *ContentEntity) FromSchemaWithEvents(ctx context.Context, ref *openapi3.Schema, changes []*Event) (*ContentEntity, error) {
	entity, err := w.FromSchema(ctx, ref)
	if err != nil {
		return nil, err
	}
	err = entity.ApplyEvents(changes)
	if err != nil {
		return nil, err
	}
	return entity, err
}

//ApplyEvents apply the new changes from payload to the entity
func (w *ContentEntity) ApplyEvents(changes []*Event) error {
	for _, change := range changes {
		w.SequenceNo = change.Meta.SequenceNo
		w.ID = change.Meta.EntityID
		w.User.BasicEntity.ID = change.Meta.User
		w.User.BasicEntity.ID = change.Meta.User

		if change.Payload != nil {
			switch change.Type {
			case "create":
				err := json.Unmarshal(change.Payload, &w.BasicEntity)
				if err != nil {
					return err
				}
				err = json.Unmarshal(change.Payload, &w.payload)
				if err != nil {
					return err
				}

			case "update":
				err := json.Unmarshal(change.Payload, &w)
				if err != nil {
					return NewDomainError("invalid: error unmarshalling changed payload", change.Meta.EntityType, w.ID, err)
				}
				w.User.BasicEntity.ID = change.Meta.User
			case "delete":
				w = &ContentEntity{}
			}
		}
	}
	return nil
}

//ToMap return entity has a map
func (w *ContentEntity) ToMap() map[string]interface{} {
	return w.payload
}

func (w *ContentEntity) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &w.AggregateRoot)
	err = json.Unmarshal(data, &w.payload)
	return err
}

//GenerateID adds a generated id to the payload based on the schema
func (w *ContentEntity) GenerateID(payload []byte) error {
	tentity := make(map[string]interface{})
	properties := w.Schema.ExtensionProps.Extensions["x-identifier"]
	err := json.Unmarshal(payload, w)
	if err != nil {
		return err
	}
	if properties != nil {
		propArray := []string{}
		err = json.Unmarshal(properties.(json.RawMessage), &propArray)
		if err != nil {
			return fmt.Errorf("unexpected error unmarshalling identifiers: %s", err)
		}
		if len(propArray) == 1 { // if there is only one x-identifier specified then it should auto generate the identifier
			property := propArray[0]
			if w.Schema.Properties[property].Value.Type == "string" && w.GetString(property) == "" {
				if w.Schema.Properties[property].Value.Format != "" { //if the format is specified
					switch w.Schema.Properties[property].Value.Format {
					case "ksuid":
						tentity[property] = ksuid.New().String()
					case "uuid":
						tentity[property] = uuid.NewString()
					default:
						errr := "unexpected error: fail to generate identifier " + property + " since the format " + w.Schema.Properties[property].Value.Format + " is not supported"
						return NewDomainError(errr, w.Schema.Title, "", nil)
					}
				} else { //if the format is not specified
					errr := "unexpected error: fail to generate identifier " + property + " since the format was not specified"
					return NewDomainError(errr, w.Schema.Title, "", nil)
				}
			} else if w.Schema.Properties[property].Value.Type == "integer" {
				reader := ds.NewReader(w.payload)
				if w.Schema.Properties[property].Value.Format == "" && reader.GetField(strings.Title(property)).PointerInt() == nil {
					errr := "unexpected error: fail to generate identifier " + property + " since the format was not specified"
					return NewDomainError(errr, w.Schema.Title, "", nil)

				}
			}

		}
	}
	generatedIdentifier, err := json.Marshal(tentity)
	if err != nil {
		return err
	}

	return json.Unmarshal(generatedIdentifier, w)
}

//UpdateTime updates auto update time values on the payload
func (w *ContentEntity) UpdateTime(operationID string, data []byte) ([]byte, error) {
	payload := map[string]interface{}{}
	json.Unmarshal(data, &payload)
	for key, p := range w.Schema.Properties {
		routes := []string{}
		routeBytes, _ := json.Marshal(p.Value.Extensions["x-update"])
		json.Unmarshal(routeBytes, &routes)
		for _, r := range routes {
			if r == operationID {
				if p.Value.Format == "date-time" {
					payload[key] = time.Now()
				}
			}
		}
	}
	newPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return newPayload, nil
}
