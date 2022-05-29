package model

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/segmentio/ksuid"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
	"math"
	"strings"
	"time"
)

type ContentEntity struct {
	AggregateRoot
	Schema  *openapi3.Schema `json:"-"`
	payload map[string]interface{}
	builder ds.Builder
}

//IsValid checks if the property is valid using the IsNull function
func (w *ContentEntity) IsValid() bool {
	if w.payload == nil {
		return false
	}

	//if there is not schema to valid against then it's valid
	if w.Schema == nil {
		return true
	}

	//check if the value being passed in is valid
	for key, value := range w.payload {
		if property, ok := w.Schema.Properties[key]; ok {
			if property.Value.Enum == nil {
				if value == nil {
					if InList(w.Schema.Required, key) {
						message := "entity property " + key + " required"
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					}

					//linked objects are allowed to be null
					if !w.Schema.Properties[key].Value.Nullable && property.Value.Type != "object" {
						message := "entity property " + key + " is not nullable"
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					}

					properties := w.Schema.ExtensionProps.Extensions["x-identifier"]
					if properties != nil {
						propArray := []string{}
						err := json.Unmarshal(properties.(json.RawMessage), &propArray)
						if err == nil {
							if InList(propArray, key) {
								message := "entity property " + key + " is part if the identifier and cannot be null"
								w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
							}
						}
					}
					continue
				}

				switch property.Value.Type {
				case "string":
					switch property.Value.Format {
					case "date-time":
						//check if the date is in the expected format
						if _, ok := value.(*Time); !ok {
							w.AddError(NewDomainError(fmt.Sprintf("invalid type specified for '%s' expected date time", key), "ContentEntity", w.ID, nil))
						}
					default:
						if _, ok := value.(string); !ok {
							w.AddError(NewDomainError(fmt.Sprintf("invalid type specified for '%s' expected string", key), "ContentEntity", w.ID, nil))
						}
					}
				case "integer":
					if _, ok := value.(int); !ok {
						w.AddError(NewDomainError(fmt.Sprintf("invalid type specified for '%s' expected integer", key), "ContentEntity", w.ID, nil))
					}
				case "boolean":
					if _, ok := value.(bool); !ok {
						w.AddError(NewDomainError(fmt.Sprintf("invalid type specified for '%s' expected boolean", key), "ContentEntity", w.ID, nil))
					}
				case "number":
					_, integerOk := value.(int)
					_, floatOk := value.(float32)
					_, float64Ok := value.(float64)
					if !integerOk && !float64Ok && !floatOk {
						w.AddError(NewDomainError(fmt.Sprintf("invalid type specified for '%s' expected number", key), "ContentEntity", w.ID, nil))
					}
				}
			} else {
				w.IsEnumValid(key, property, value)
			}
		}
	}

	return len(w.entityErrors) == 0
}

//IsEnumValid this loops over the properties of an entity, if the enum is not nil, it will validate if the option the user set is valid
func (w *ContentEntity) IsEnumValid(propertyName string, property *openapi3.SchemaRef, value interface{}) bool {

	nullFound := false
	//used to indicate if the value passed is part of the enumeration
	enumFound := false

	if property.Value.Enum != nil {
		if !property.Value.Nullable {
			message := "this content entity does not contain the field: " + strings.Title(propertyName)
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
				if nullFound && value == "" {
					enumFound = true
				} else if property.Value.Format == "date-time" {
					for _, v := range property.Value.Enum {
						if val, ok := v.(string); ok {
							currTime, _ := time.Parse("2006-01-02T15:04:05Z", val)
							currentTime := NewTime(currTime)
							enumFound = enumFound || value.(*Time).String() == currentTime.String()
						}
					}
				} else {
					for _, v := range property.Value.Enum {
						if val, ok := v.(string); ok {
							if tv, ok := value.(string); ok {
								if tv != "null" { //we don't allow the user to literally send "null"
									enumFound = enumFound || tv == val
								}

							}
						}
					}
				}
			case "integer":
				for _, v := range property.Value.Enum {
					if val, ok := v.(float64); ok {
						enumFound = enumFound || int(math.Round(val)) == value
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

		if !enumFound {
			message := "invalid value set for " + propertyName
			w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
			return enumFound
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
		err = w.UpdateTime(operation)
		if err != nil {
			return nil, err
		}
	}
	//this is done so that if a payload has MORE info that what was needed by the entity only what was applicable
	//would be in the event payload
	err = w.SetValueFromPayload(ctx, payload)
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
	return w, err
}

//GORMModel return model
func (w *ContentEntity) GORMModel(ctx context.Context) (interface{}, error) {
	identifiers := w.Schema.Extensions["x-identifier"]
	//ideally the builder would be set by the entity factory which should accommodate the recursive structs
	if w.builder == nil {
		w.builder = ds.NewStruct()
		if identifiers == nil {
			name := "ID"
			w.builder.AddField(name, uint(0), `json:"id"`)
		}
		relations := make(map[string]string)
		for name, p := range w.Schema.Properties {
			exportedPropertyName := strings.Title(name)
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
								w.builder.AddField(exportedPropertyName, time.Now(), `json:"`+name+`"`)
							} else {
								w.builder.AddField(exportedPropertyName, []*string{}, `json:"`+name+`"`)
							}
						} else if t2 == "number" {
							w.builder.AddField(exportedPropertyName, []*float64{}, `json:"`+name+`"`)
						} else if t == "integer" {
							w.builder.AddField(exportedPropertyName, []*int{}, `json:"`+name+`"`)
						} else if t == "boolean" {
							w.builder.AddField(exportedPropertyName, []*bool{}, `json:"`+name+`"`)
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
							w.builder.AddField(exportedPropertyName, t, `json:"`+name+`"`)
						} else {
							var s *string
							w.builder.AddField(exportedPropertyName, s, `json:"`+name+`"`)
						}
					} else if t == "number" {
						var numbers *float32
						w.builder.AddField(exportedPropertyName, numbers, `json:"`+name+`"`)
					} else if t == "integer" {
						var integers *int
						w.builder.AddField(exportedPropertyName, integers, `json:"`+name+`"`)
					} else if t == "boolean" {
						var boolean *bool
						w.builder.AddField(exportedPropertyName, boolean, `json:"`+name+`"`)
					}
				}
			}
		}

		//setup basic weos properties
		w.builder.AddField("Weos_id", w.ID, `json:"weos_id"`)
		w.builder.AddField("Sequence_no", w.SequenceNo, `json:"sequence_no"`)
	}

	model := w.builder.Build().New()
	//if there is a payload let's serialize that
	if w.payload != nil {
		tpayload := w.payload
		tpayload["weos_id"] = w.ID
		tpayload["sequence_no"] = w.SequenceNo
		tbytes, err := json.Marshal(tpayload)
		if err != nil {
			return nil, NewDomainError("error prepping entity for gorm", "ContentEntity", w.ID, err)
		}
		err = json.Unmarshal(tbytes, &model)
		if err != nil {
			return nil, NewDomainError(fmt.Sprintf("error prepping entity for gorm '%s'", err), "ContentEntity", w.ID, err)
		}
	}

	return model, nil
}

//FromSchema builds properties from the schema
func (w *ContentEntity) FromSchema(ctx context.Context, ref *openapi3.Schema) (*ContentEntity, error) {
	w.User.ID = weosContext.GetUser(ctx)
	w.Schema = ref
	//create map using properties and default values in schema
	if w.payload == nil {
		w.payload = make(map[string]interface{})
	}
	for name, property := range w.Schema.Properties {
		w.payload[name] = nil
		if property.Value.Default != nil {
			w.payload[name] = property.Value.Default
		}
	}
	return w, nil
}

//FromSchemaAndBuilder builds properties from the schema and uses the builder generated on startup. This helps generate
//complex gorm models that reference other models (if we only use the schema for the current entity then we won't be able to do that)
func (w *ContentEntity) FromSchemaAndBuilder(ctx context.Context, ref *openapi3.Schema, builder ds.Builder) (*ContentEntity, error) {
	_, err := w.FromSchema(ctx, ref)
	if err != nil {
		return nil, err
	}
	w.builder = builder
	return w, nil
}

//FromSchemaWithValues builds properties from schema and unmarshall payload into it
func (w *ContentEntity) FromSchemaWithValues(ctx context.Context, schema *openapi3.Schema, payload json.RawMessage) (*ContentEntity, error) {
	_, err := w.FromSchema(ctx, schema)
	if err != nil {
		return nil, err
	}
	return w, w.SetValueFromPayload(ctx, payload)
}

func (w *ContentEntity) SetValueFromPayload(ctx context.Context, payload json.RawMessage) error {
	weosID, err := GetIDfromPayload(payload)
	if err != nil {
		return NewDomainError("unexpected error unmarshalling payload", w.Schema.Title, w.ID, err)
	}

	if w.ID == "" {
		if weosID != "" {
			w.ID = weosID
		} else {
			w.ID = ksuid.New().String()
		}
	}

	err = json.Unmarshal(payload, &w.payload)
	if err != nil {
		return NewDomainError("unexpected error unmarshalling payload", w.Schema.Title, w.ID, err)
	}

	//go serializes integers to float64
	if w.Schema != nil {
		var tpayload map[string]interface{}
		tpayload, err = w.SetValue(w.Schema, w.payload)
		if err == nil {
			w.payload = tpayload
		}
	}

	return err
}

//SetValue use to recursively setup the payload
func (w *ContentEntity) SetValue(schema *openapi3.Schema, data map[string]interface{}) (map[string]interface{}, error) {
	var err error
	for k, property := range schema.Properties {
		if property.Value != nil {
			switch property.Value.Type {
			case "integer":
				if t, ok := data[k].(float64); ok {
					data[k] = int(math.Round(t))
				}
			case "string":
				switch property.Value.Format {
				case "date-time":
					//if the value is a string let's try to convert to time
					if value, ok := data[k].(string); ok {
						ttime, err := time.Parse("2006-01-02T15:04:05Z", value)
						if err != nil {
							return nil, NewDomainError(fmt.Sprintf("invalid date time set for '%s' it should be in the format '2006-01-02T15:04:05Z', got '%s'", k, value), w.Schema.Title, w.ID, err)
						}
						data[k] = NewTime(ttime)
					}
				//if it's a ksuid and the value is nil then auto generate the field
				case "ksuid":
					if data[k] == nil {
						properties := schema.ExtensionProps.Extensions["x-identifier"]
						if properties != nil {
							propArray := []string{}
							err = json.Unmarshal(properties.(json.RawMessage), &propArray)
							if InList(propArray, k) {
								data[k] = ksuid.New().String()
								//if the identifier is only one part, and it's a string then let's use it as the entity id
								if _, ok := data["weos_id"]; !ok && len(propArray) == 1 {
									data["weos_id"] = data[k].(string)
								}
							}
						}
					}
				case "uuid":
					if data[k] == nil {
						properties := schema.ExtensionProps.Extensions["x-identifier"]
						if properties != nil {
							propArray := []string{}
							err = json.Unmarshal(properties.(json.RawMessage), &propArray)
							if InList(propArray, k) {
								data[k] = uuid.NewString()
								//if the identifier is only one part, and it's a string then let's use it as the entity id
								if _, ok := data["weos_id"]; !ok && len(propArray) == 1 {
									data["weos_id"] = data[k].(string)
								}
							}
						}
					}
				}
			case "array":
				//use schema to see which items in payload needs an id generated and do it.
				if values, ok := data[k].([]interface{}); ok {
					if property.Value != nil && property.Value.Items != nil && property.Value.Items.Value != nil {
						for i, _ := range values {
							if value, ok := values[i].(map[string]interface{}); ok {
								value, err = w.SetValue(property.Value.Items.Value, value)
								tvalue, err := json.Marshal(value)
								if err != nil {
									return nil, err
								}
								values[i], err = new(ContentEntity).FromSchemaWithValues(context.Background(), schema, tvalue)
							}
						}
					}
				}

			}
		}
	}
	return data, err
}

func (w *ContentEntity) Update(ctx context.Context, payload json.RawMessage) (*ContentEntity, error) {
	//TODO validate payload to ensure that properties that are part of the identifier is not being updated
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
func (w *ContentEntity) GetTime(name string) *Time {
	if v, ok := w.payload[name]; ok {
		if val, ok := v.(*Time); ok {
			return val
		}
	}
	return NewTime(time.Time{})
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
	//if  there is a schema set then let's use set value which does some type conversions etc
	if w.Schema != nil {
		err = w.SetValueFromPayload(context.Background(), data)
	} else {
		err = json.Unmarshal(data, &w.payload)
	}

	return err
}

func (w *ContentEntity) MarshalJSON() ([]byte, error) {
	b := w.payload
	b["weos_id"] = w.ID
	b["sequence_no"] = w.SequenceNo
	return json.Marshal(b)
}

//Deprecated: 05/08/2022 the default ids are generated in SetValueFromPayload
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
func (w *ContentEntity) UpdateTime(operationID string) error {
	for key, p := range w.Schema.Properties {
		routes := []string{}
		routeBytes, _ := json.Marshal(p.Value.Extensions["x-update"])
		err := json.Unmarshal(routeBytes, &routes)
		if err != nil {
			return err
		}
		for _, r := range routes {
			if r == operationID {
				if p.Value.Format == "date-time" {
					w.payload[key] = NewTime(time.Now())
				}
			}
		}
	}
	return nil
}
