package model

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos-service/context"
	utils "github.com/wepala/weos-service/utils"
	"golang.org/x/net/context"
	"strings"
)

type ContentEntity struct {
	AggregateRoot
	Schema   *openapi3.Schema
	Property interface{}
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
	return true
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

//FromSchema builds properties from the schema
func (w *ContentEntity) FromSchema(ctx context.Context, ref *openapi3.Schema) (*ContentEntity, error) {
	w.User.ID = weosContext.GetUser(ctx)
	w.Schema = ref
	identifiers := w.Schema.Extensions["x-identifier"]
	instance := ds.NewStruct()
	if identifiers == nil {
		var idType *uint //NOT SURE ABOUT THE TYPE
		name := "Id"
		instance.AddField(name, idType, "id")
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
						instance.AddField(name, []*string{}, utils.SnakeCase(name))
					} else if t2 == "number" {
						instance.AddField(name, []*float64{}, utils.SnakeCase(name))
					} else if t == "integer" {
						instance.AddField(name, []*int{}, utils.SnakeCase(name))
					} else if t == "boolean" {
						instance.AddField(name, []*bool{}, utils.SnakeCase(name))
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
					var strings *string
					instance.AddField(name, strings, utils.SnakeCase(name))
				} else if t == "number" {
					var numbers *float32
					instance.AddField(name, numbers, utils.SnakeCase(name))
				} else if t == "integer" {
					var integers *int
					instance.AddField(name, integers, utils.SnakeCase(name))
				} else if t == "boolean" {
					var boolean *bool
					instance.AddField(name, boolean, utils.SnakeCase(name))
				}
			}
		}
	}
	w.Property = instance.Build().New()
	return w, nil

}

//FromSchemaWithValues builds properties from schema and unmarshall payload into it
func (w *ContentEntity) FromSchemaWithValues(ctx context.Context, schema *openapi3.Schema, payload json.RawMessage) (*ContentEntity, error) {
	w.FromSchema(ctx, schema)

	weosID, err := GetIDfromPayload(payload)
	if err != nil {
		return w, NewDomainError("unexpected error unmarshalling payload", w.Schema.Title, w.ID, err)
	}

	w.ID = weosID
	event := NewEntityEvent("create", w, w.ID, payload)
	w.NewChange(event)
	return w, w.ApplyChanges([]*Event{event})
}

func (w *ContentEntity) Update(ctx context.Context, existingPayload json.RawMessage, updatedPayload json.RawMessage) (*ContentEntity, error) {
	contentType := weosContext.GetContentType(ctx)

	w.FromSchema(ctx, contentType.Schema)

	err := json.Unmarshal(existingPayload, &w.BasicEntity)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(existingPayload, &w.Property)
	if err != nil {
		return nil, err
	}

	event := NewEntityEvent("update", w, w.ID, updatedPayload)
	w.NewChange(event)
	return w, w.ApplyChanges([]*Event{event})
}

//GetString returns the string property value stored of a given the property name
func (w *ContentEntity) GetString(name string) string {
	if w.Property == nil {
		return ""
	}
	reader := ds.NewReader(w.Property)
	isValid := reader.HasField(name)
	if !isValid {
		return ""
	}
	if reader.GetField(name).PointerString() == nil {
		return ""
	}
	return *reader.GetField(name).PointerString()
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
	if reader.GetField(name).PointerUint() == nil {
		return uint(0)
	}
	return *reader.GetField(name).PointerUint()
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

//ApplyChanges apply the new changes from payload to the entity
func (w *ContentEntity) ApplyChanges(changes []*Event) error {
	for _, change := range changes {
		w.SequenceNo = change.Meta.SequenceNo
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
			w.User.BasicEntity.ID = change.Meta.User
		case "update":
			err := json.Unmarshal(change.Payload, &w.Property)
			if err != nil {
				return NewDomainError("invalid: error unmarshalling changed payload", change.Meta.EntityType, w.ID, err)
			}
			w.User.BasicEntity.ID = change.Meta.User

		}
	}
	return nil
}
