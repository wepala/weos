package model

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos/context"
	utils "github.com/wepala/weos/utils"
	"golang.org/x/net/context"
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
				enumProperty := w.GetString(strings.Title(k))

				////This checks if a "null" option was provided which is needed if nullable == true
				for _, v := range property.Value.Enum {
					nullFound = v.(string) == "null"
					if nullFound == true {
						break
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

					if enumProperty == "null" {
						enumFound = false
					}

					if enumFound == false {
						message := "invalid enumeration option provided. available options are: " + enumOptions + " (for the null option, use a blank string, not the keyword null)"
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
			case "integer":
				enumProperty := w.GetInteger(strings.Title(k))

				////This checks if a "0" option was provided which is needed if nullable == true
				for _, v := range property.Value.Enum {
					nullFound = int(v.(float64)) == 0
					if nullFound == true {
						break
					}
				}

				//If nullable == true and null is found in the options
				if property.Value.Nullable && nullFound == true {
					//Assuming if the nullable is true, the user can pass a blank string
					if enumProperty == 0 {
						enumFound = true
					}

					if enumFound == false {
						for _, v := range property.Value.Enum {
							enumFound = enumProperty == (int(v.(float64)))
							if enumFound == true {
								break
							}
						}
					}

					if enumFound == false {
						message := "invalid enumeration option provided. available options are: " + enumOptions
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}

				} else if property.Value.Nullable == true && nullFound == false {
					message := `"if nullable is set to true, 0 is needed as an enum option"`
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false

				} else if property.Value.Nullable == false {
					if enumProperty == 0 || nullFound == true {
						message := "nullable is set to false, cannot use null/blank integer nor have it as an enum option."
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}
					for _, v := range property.Value.Enum {
						enumFound = enumProperty == (int(v.(float64)))
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
			case "boolean":
				enumProperty := w.GetBool(strings.Title(k))

				////This checks if a "null" option was provided which is needed if nullable == true
				for _, v := range property.Value.Enum {
					nullFound = v.(string) == "null"
					if nullFound == true {
						break
					}
				}

				//If nullable == true and null is found in the options
				if property.Value.Nullable && nullFound == true {
					for _, v := range property.Value.Enum {
						enumFound = enumProperty == v.(bool)
						if enumFound == true {
							break
						}
					}

					if enumFound == false {
						message := "invalid enumeration option provided. available options are: " + enumOptions + " (for the null option, use a blank string, not the keyword null)"
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}

				} else if property.Value.Nullable == true && nullFound == false {
					message := `"if nullable is set to true, "null" is needed as an enum option"`
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false
				} else if property.Value.Nullable == false {
					if nullFound == true {
						message := "nullable is set to false, cannot have null as an enum option."
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}
					for _, v := range property.Value.Enum {
						enumFound = enumProperty == v.(bool)
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
			case "float":
				enumProperty := w.GetNumber(strings.Title(k))

				////This checks if a "0" option was provided which is needed if nullable == true
				for _, v := range property.Value.Enum {
					nullFound = int(v.(float64)) == 0.0
					if nullFound == true {
						break
					}
				}

				//If nullable == true and null is found in the options
				if property.Value.Nullable && nullFound == true {
					//Assuming if the nullable is true, the user can pass a blank string
					if enumProperty == 0.0 {
						enumFound = true
					}

					if enumFound == false {
						for _, v := range property.Value.Enum {
							enumFound = enumProperty == v.(float64)
							if enumFound == true {
								break
							}
						}
					}

					if enumFound == false {
						message := "invalid enumeration option provided. available options are: " + enumOptions
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}

				} else if property.Value.Nullable == true && nullFound == false {
					message := `"if nullable is set to true, 0.0 is needed as an enum option"`
					w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
					return false

				} else if property.Value.Nullable == false {
					if enumProperty == 0.0 || nullFound == true {
						message := "nullable is set to false, cannot use null/blank integer nor have it as an enum option."
						w.AddError(NewDomainError(message, w.Schema.Title, w.ID, nil))
						return false
					}
					for _, v := range property.Value.Enum {
						enumFound = enumProperty == v.(float64)
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
			case "date/time":
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
					enumOptions = enumOptions + strconv.Itoa(int(v.(float64))) + ", "
				} else if k == len(enum)-1 {
					enumOptions = enumOptions + strconv.Itoa(int(v.(float64)))
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
			//check if the lowercase version of the field is the same as the schema and use the scehma version instead
			if originialFieldName := w.GetOriginalFieldName(field.Name()); originialFieldName != "" {
				result[w.GetOriginalFieldName(field.Name())] = field.Interface()
			} else if originialFieldName == "" && strings.EqualFold(field.Name(), "id") {
				result["id"] = field.Interface()
			}
		}
	}

	return result
}

//GetOriginalFieldName the original name of the field as defined in the schema (the field is Title cased when converted to struct)
func (w *ContentEntity) GetOriginalFieldName(structName string) string {
	if w.Schema != nil {
		for key, _ := range w.Schema.Properties {
			if strings.ToLower(key) == strings.ToLower(structName) {
				return key
			}
		}
	}

	return ""
}

func (w *ContentEntity) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &w.AggregateRoot)
	err = json.Unmarshal(data, &w.Property)
	return err
}
