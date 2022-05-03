package model

import (
	"crypto/sha256"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/segmentio/ksuid"
	weosContext "github.com/wepala/weos/context"
	utils "github.com/wepala/weos/utils"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/sha3"
	"golang.org/x/net/context"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type ContentEntity struct {
	AggregateRoot
	Schema   *openapi3.Schema
	Property interface{}
	reader   ds.Reader
	builder  ds.Builder
}

//IsValid checks if the property is valid using the IsNull function
func (w *ContentEntity) IsValid() bool {
	if w.Property == nil {
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
//If nullable == true, this means a blank string can be used as an option
//If statements are structured around this ^ and covers different cases.
func (w *ContentEntity) IsEnumValid() bool {
	for k, property := range w.Schema.Properties {
		nullFound := false
		enumFound := false
		enumOptions := EnumString(property.Value.Enum)

		if property.Value.Enum != nil {
			switch property.Value.Type {
			case "string":
				if property.Value.Format == "date-time" {
					var enumProperty *time.Time
					reader := ds.NewReader(w.Property)
					isValid := reader.HasField(strings.Title(k))
					if !isValid {
						message := "this content entity does not contain the field: " + strings.Title(k)
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}
					if reader.GetField(strings.Title(k)).PointerTime() == nil {
						enumProperty = nil
					} else {
						enumProperty = reader.GetField(strings.Title(k)).PointerTime()
					}

					//This checks if a "null" option was provided which is needed if nullable == true
					for _, v := range property.Value.Enum {
						switch reflect.TypeOf(v).String() {
						case "string":
							if v.(string) == "null" {
								nullFound = true
							}
						}
					}

					//If nullable == true and null is found in the options
					if property.Value.Nullable && nullFound == true {
						//Assuming if the nullable is true, the user can pass a blank string
						if enumProperty == nil {
							enumFound = true
							//The user may only use a blank string to indicate a null field, not the actual keyword
						} else if enumFound == false {
						findTimeEnum:
							for _, v := range property.Value.Enum {
								switch reflect.TypeOf(v).String() {
								case "string":
									currTime, _ := time.Parse("2006-01-02T15:04:00Z", v.(string))
									enumFound = *enumProperty == currTime

									if enumFound == true {
										break findTimeEnum
									}
								}
							}
						}

						if enumFound == false {
							message := "invalid enumeration option provided. available options are: " + enumOptions + "(for the null option, use the keyword null(without quotes))"
							w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
							return false
						}

					} else if property.Value.Nullable == true && nullFound == false {
						message := `"if nullable is set to true, "null" is needed as an enum option"`
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false

					} else if property.Value.Nullable == false {
						if enumProperty == nil || nullFound == true {
							message := "nullable is set to false, cannot use null nor have it as an enum option."
							w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
							return false
						}
					findTimeEnum1:
						for _, v := range property.Value.Enum {
							switch reflect.TypeOf(v).String() {
							case "string":
								currTime, _ := time.Parse("2006-01-02T15:04:00Z", v.(string))
								enumFound = *enumProperty == currTime

								if enumFound == true {
									break findTimeEnum1
								}
							}
						}
						if enumFound == false {
							message := "invalid enumeration option provided. available options are: " + enumOptions
							w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
							return false
						}
					}
				} else {
					//var enumProperty *string
					enumProperty := w.GetString(strings.Title(k))

					//This checks if a "null" option was provided which is needed if nullable == true
					for _, v := range property.Value.Enum {
						switch reflect.TypeOf(v).String() {
						case "string":
							if v.(string) == "null" {
								nullFound = true
							}
						}
					}

					//If nullable == true and null is found in the options
					if property.Value.Nullable && nullFound == true {
						//Assuming if the nullable is true, the user can pass a blank string
						if enumProperty == "" {
							enumFound = true
							//The user may only use a blank string to indicate a null field, not the actual keyword
						} else if enumFound == false {
							for _, v := range property.Value.Enum {
								enumFound = enumProperty == v.(string)
								if enumFound == true {
									break
								}
							}
						}

						if enumFound == false || enumProperty == "null" {
							message := "invalid enumeration option provided. available options are: " + enumOptions + " (for the null option, use a blank string, or the keyword null(without quotes))"
							w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
							return false
						}

					} else if property.Value.Nullable == true && nullFound == false {
						message := `"if nullable is set to true, "null" is needed as an enum option"`
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false

					} else if property.Value.Nullable == false {
						if enumProperty == "null" || enumProperty == "" || nullFound == true {
							message := "nullable is set to false, cannot use null/blank string nor have it as an enum option."
							w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
							return false
						}
						for _, v := range property.Value.Enum {
							enumFound = enumProperty == v.(string)
							if enumFound == true {
								break
							}
						}
						if enumFound == false {
							message := "invalid enumeration option provided. available options are: " + enumOptions
							w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
							return false
						}
					}
				}
			case "integer":
				var enumProperty *int
				reader := ds.NewReader(w.Property)
				isValid := reader.HasField(strings.Title(k))
				if !isValid {
					message := "this content entity does not contain the field: " + strings.Title(k)
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false
				}
				if reader.GetField(strings.Title(k)).PointerInt() == nil {
					enumProperty = nil
				} else {
					enumProperty = reader.GetField(strings.Title(k)).PointerInt()
				}

				//This checks if a "null" option was provided which is needed if nullable == true
				for _, v := range property.Value.Enum {
					switch reflect.TypeOf(v).String() {
					case "string":
						if v.(string) == "null" {
							nullFound = true
						}
					}
				}

				//If nullable == true and null is found in the options
				if property.Value.Nullable && nullFound == true {
					//Assuming if the nullable is true, the user can pass a blank string
					if enumProperty == nil {
						enumFound = true
					}

					if enumFound == false {
					findIntEnum:
						for _, v := range property.Value.Enum {
							switch reflect.TypeOf(v).String() {
							case "string":
								if v.(string) == "null" {
									continue
								}
							case "float64":
								enumFound = *enumProperty == (int(v.(float64)))

								if enumFound == true {
									break findIntEnum
								}
							}
						}
					}

					if enumFound == false {
						message := "invalid enumeration option provided. available options are: " + enumOptions + "(for the null option, use a blank string, or the keyword null(without quotes))"
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}

				} else if property.Value.Nullable == true && nullFound == false {
					message := `"if nullable is set to true, "null" is needed as an enum option"`
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false

				} else if property.Value.Nullable == false {
					if nullFound == true || enumProperty == nil {
						message := "nullable is set to false, cannot use null nor have it as an enum option."
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}

				findIntEnum1:
					for _, v := range property.Value.Enum {
						switch reflect.TypeOf(v).String() {
						case "float64":
							enumFound = *enumProperty == (int(v.(float64)))

							if enumFound == true {
								break findIntEnum1
							}
						}
					}
				}

				if enumFound == false {
					message := "invalid enumeration option provided. available options are: " + enumOptions
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false
				}

			case "number":
				//enumProperty := w.GetNumber(strings.Title(k))
				var enumProperty *float64
				reader := ds.NewReader(w.Property)
				isValid := reader.HasField(strings.Title(k))
				if !isValid {
					message := "this content entity does not contain the field: " + strings.Title(k)
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false
				}
				if reader.GetField(strings.Title(k)).PointerFloat64() == nil {
					enumProperty = nil
				} else {
					enumProperty = reader.GetField(strings.Title(k)).PointerFloat64()
				}

				//This checks if a "null" option was provided which is needed if nullable == true
				for _, v := range property.Value.Enum {
					switch reflect.TypeOf(v).String() {
					case "string":
						if v.(string) == "null" {
							nullFound = true
						}
					}
				}

				//If nullable == true and null is found in the options
				if property.Value.Nullable && nullFound == true {
					//Assuming if the nullable is true, the user can pass a blank string
					if enumProperty == nil {
						enumFound = true
					}

					if enumFound == false {
					findFloatEnum:
						for _, v := range property.Value.Enum {
							switch reflect.TypeOf(v).String() {
							case "string":
								if v.(string) == "null" {
									continue
								}
							case "float64":
								enumFound = fmt.Sprintf("%.3f", *enumProperty) == fmt.Sprintf("%.3f", v.(float64))

								if enumFound == true {
									break findFloatEnum
								}
							}
						}
					}

					if enumFound == false {
						message := "invalid enumeration option provided. available options are: " + enumOptions + "(for the null option, use a blank string, or the keyword null(without quotes))"
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}

				} else if property.Value.Nullable == true && nullFound == false {
					message := `"if nullable is set to true, null is needed as an enum option"`
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false

				} else if property.Value.Nullable == false {
					if enumProperty == nil || nullFound == true {
						message := "nullable is set to false, cannot use null nor have it as an enum option."
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}
				findFloatEnum1:
					for _, v := range property.Value.Enum {
						switch reflect.TypeOf(v).String() {
						case "float64":
							enumFound = fmt.Sprintf("%.3f", *enumProperty) == fmt.Sprintf("%.3f", v.(float64))

							if enumFound == true {
								break findFloatEnum1
							}
						}
					}
				}
				if enumFound == false {
					message := "invalid enumeration option provided. available options are: " + enumOptions
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false
				}
			}
		}
	}
	return true
}

//EnumString takes the interface of enum options, ranges over them and concatenates it into one string
//If the enum interface is empty, it will return a blank string
func EnumString(enum []interface{}) string {
	enumOptions := ""
	if len(enum) > 0 {
		for k, v := range enum {
			switch v.(type) {
			case string:
				if k < len(enum)-1 {
					enumOptions = enumOptions + v.(string) + ", "
				} else if k == len(enum)-1 {
					enumOptions = enumOptions + v.(string)
				}
			case float64:
				if k < len(enum)-1 {
					enumOptions = enumOptions + fmt.Sprintf("%g", v.(float64)) + ", "
				} else if k == len(enum)-1 {
					enumOptions = enumOptions + fmt.Sprintf("%g", v.(float64))
				}
			case bool:
				if k < len(enum)-1 {
					enumOptions = enumOptions + strconv.FormatBool(v.(bool)) + ", "
				} else if k == len(enum)-1 {
					enumOptions = enumOptions + strconv.FormatBool(v.(bool))
				}
			}

		}
		return enumOptions
	}
	return ""
}

//IsNull checks if the value of the property is null
func (w *ContentEntity) IsNull(name string) bool {
	reader := ds.NewReader(w.Property)
	switch w.Schema.Properties[name].Value.Type {
	case "string":
		temp := reader.GetField(strings.Title(name)).PointerString()
		if temp == nil {
			return true
		}
	case "number":
		temp := reader.GetField(strings.Title(name)).PointerFloat64()
		if temp == nil {
			return true
		}
	case "integer":
		temp := reader.GetField(strings.Title(name)).PointerInt()
		if temp == nil {
			return true
		}
	}

	return false
}

//FromSchemaAndBuilder the entity factor uses this to initialize the content entity
func (w *ContentEntity) FromSchemaAndBuilder(ctx context.Context, ref *openapi3.Schema, builder ds.Builder) (*ContentEntity, error) {
	w.Schema = ref
	w.builder = builder
	w.Property = w.builder.Build().New()
	w.reader = ds.NewReader(w.Property)
	return w, nil
}

func (w *ContentEntity) Init(ctx context.Context, payload json.RawMessage) (*ContentEntity, error) {
	var err error

	payload, err = w.GenerateHash(payload)
	if err != nil {
		return nil, err
	}

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
	eventPayload, err := json.Marshal(w.Property)
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

//Deprecated: this duplicates the work of making the dynamic struct builder. Use FromSchemaAndBuilder instead (this is used by the EntityFactory)
//FromSchema builds properties from the schema
func (w *ContentEntity) FromSchema(ctx context.Context, ref *openapi3.Schema) (*ContentEntity, error) {
	w.User.ID = weosContext.GetUser(ctx)
	w.Schema = ref
	identifiers := w.Schema.Extensions["x-identifier"]
	instance := ds.NewStruct()
	if identifiers == nil {
		name := "ID"
		instance.AddField(name, uint(0), `json:"id"`)
	}
	relations := make(map[string]string)
	for name, p := range ref.Properties {
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
	w.Property = instance.Build().New()
	w.reader = ds.NewReader(w.Property)
	return w, nil

}

//Deprecated: 02/01/2022 Use FromSchemaAndBulider then call SetValues
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

	err = json.Unmarshal(payload, w.Property)
	if err != nil {
		return NewDomainError("unexpected error unmarshalling payload", w.Schema.Title, w.ID, err)
	}

	return nil
}

func (w *ContentEntity) Update(ctx context.Context, payload json.RawMessage) (*ContentEntity, error) {
	var err error

	payload, err = w.GenerateHash(payload)
	if err != nil {
		return nil, err
	}

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
	name = strings.Title(name)
	if w.Property == nil {
		return ""
	}
	isValid := w.reader.HasField(name)
	if !isValid {
		return ""
	}
	if w.reader.GetField(name).PointerString() == nil {
		return ""
	}
	return *w.reader.GetField(name).PointerString()
}

//GetInteger returns the integer property value stored of a given the property name
func (w *ContentEntity) GetInteger(name string) int {
	name = strings.Title(name)
	if w.Property == nil {
		return 0
	}
	reader := ds.NewReader(w.Property)
	isValid := reader.HasField(name)
	if !isValid {
		return 0
	}
	if reader.GetField(name).PointerInt() == nil {
		return 0
	}
	return *reader.GetField(name).PointerInt()
}

//GetUint returns the unsigned integer property value stored of a given the property name
func (w *ContentEntity) GetUint(name string) uint {
	name = strings.Title(name)
	if w.Property == nil {
		return uint(0)
	}
	reader := ds.NewReader(w.Property)
	isValid := reader.HasField(name)
	if !isValid {
		return uint(0)
	}
	if reader.GetField(name).Uint() == uint(0) {
		return uint(0)
	}
	return reader.GetField(name).Uint()
}

//GetBool returns the boolean property value stored of a given the property name
func (w *ContentEntity) GetBool(name string) bool {
	name = strings.Title(name)
	if w.Property == nil {
		return false
	}
	reader := ds.NewReader(w.Property)
	isValid := reader.HasField(name)
	if !isValid {
		return false
	}
	if reader.GetField(name).PointerBool() == nil {
		return false
	}
	return *reader.GetField(name).PointerBool()
}

//GetNumber returns the float64 property value stored of a given the property name
func (w *ContentEntity) GetNumber(name string) float64 {
	name = strings.Title(name)
	if w.Property == nil {
		return 0
	}
	reader := ds.NewReader(w.Property)
	isValid := reader.HasField(name)
	if !isValid {
		return 0
	}
	if reader.GetField(name).PointerFloat64() == nil {
		return 0.0
	}
	return *reader.GetField(name).PointerFloat64()
}

//GetTime returns the time.Time property value stored of a given the property name
func (w *ContentEntity) GetTime(name string) time.Time {
	name = strings.Title(name)
	if w.Property == nil {
		return time.Time{}
	}
	reader := ds.NewReader(w.Property)
	isValid := reader.HasField(name)
	if !isValid {
		return time.Time{}
	}
	if reader.GetField(name).PointerTime() == nil {
		return time.Time{}
	}
	return *reader.GetField(name).PointerTime()
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
				err = json.Unmarshal(change.Payload, &w.Property)
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
	result := make(map[string]interface{})
	//get all fields and return the map
	if w.reader != nil {
		fields := w.reader.GetAllFields()
		for _, field := range fields {
			//check if the lowercase version of the field is the same as the schema and use the schema version instead
			if originialFieldName, _ := w.GetOriginalFieldName(field.Name()); originialFieldName != "" {
				//if the field is not a scalar then use marshalling
				originalKey, propertyType := w.GetOriginalFieldName(field.Name())
				switch propertyType {
				case "array":
					tvalue := []interface{}{}
					value, _ := json.Marshal(field.Interface())
					json.Unmarshal(value, &tvalue)
					result[originalKey] = tvalue
				case "object":
					tvalue := make(map[string]interface{})
					value, _ := json.Marshal(field.Interface())
					json.Unmarshal(value, &tvalue)
					result[originalKey] = tvalue
				default:
					result[originalKey] = field.Interface()
				}

			} else if originialFieldName == "" && strings.EqualFold(field.Name(), "id") {
				result["id"] = field.Interface()
			}
		}
	}

	return result
}

//GetOriginalFieldName the original name of the field as defined in the schema (the field is Title cased when converted to struct)
func (w *ContentEntity) GetOriginalFieldName(structName string) (string, string) {
	if w.Schema != nil {
		for key, _ := range w.Schema.Properties {
			if strings.ToLower(key) == strings.ToLower(structName) {
				return key, w.Schema.Properties[key].Value.Type
			}
		}
	}
	return "", ""
}

func (w *ContentEntity) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &w.AggregateRoot)
	err = json.Unmarshal(data, &w.Property)
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
				reader := ds.NewReader(w.Property)
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

func (w *ContentEntity) GenerateHash(payload []byte) ([]byte, error) {
	if payload == nil {
		return payload, fmt.Errorf("unexpected error: empty payload")
	}

	payloadMap := make(map[string]interface{})
	err := json.Unmarshal(payload, &payloadMap)
	if err != nil {
		return payload, err
	}

	for name, prop := range w.Schema.Properties {
		if prop.Value.Format != "" { //if the format is specified
			switch prop.Value.Format {
			case "bcrypt":
				if payloadMap[name] != nil {
					hash, errr := bcrypt.GenerateFromPassword([]byte(payloadMap[name].(string)), 14)
					if errr != nil {
						return payload, err
					}
					payloadMap[name] = string(hash)
				}
			case "base64":
				if payloadMap[name] != nil {
					hash := b64.StdEncoding.EncodeToString([]byte(payloadMap[name].(string)))
					payloadMap[name] = hash
				}
			case "sha256":
				if payloadMap[name] != nil {
					hash := sha256.Sum256([]byte(payloadMap[name].(string)))
					payloadMap[name] = b64.StdEncoding.EncodeToString(hash[:])
				}
			case "sha3-256":
				if payloadMap[name] != nil {
					hash := sha3.Sum256([]byte(payloadMap[name].(string)))
					payloadMap[name] = b64.StdEncoding.EncodeToString(hash[:])
				}
			case "sha3-512":
				if payloadMap[name] != nil {
					hash := sha3.Sum512([]byte(payloadMap[name].(string)))
					payloadMap[name] = b64.StdEncoding.EncodeToString(hash[:])
				}
			}
		}

	}

	hashedPayload, err := json.Marshal(payloadMap)
	if err != nil {
		return payload, err
	}

	return hashedPayload, nil
}
