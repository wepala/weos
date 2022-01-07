package model

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/segmentio/ksuid"
	"github.com/stoewer/go-strcase"
	weosContext "github.com/wepala/weos-service/context"
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
	isValid := true
	if w.Property == nil {
		return false
	}
	for _, req := range w.Schema.Required {
		isValid = w.IsNull(req, w.Schema.Properties[req].Value.Type) && isValid
	}
	return isValid
}

//IsNull checks if the property is nullable and if the value is null
func (w *ContentEntity) IsNull(name, contentType string) bool {
	temp := strings.Title(name)
	switch contentType {
	case "string":
		newString := w.GetString(temp)
		if !w.Schema.Properties[name].Value.Nullable && newString == "" {
			return false
		}
	case "number":
		if !w.Schema.Properties[name].Value.Nullable && w.GetNumber(temp) == 0 {
			return false
		}
	case "integer":
		if !w.Schema.Properties[name].Value.Nullable && w.GetInteger(temp) == 0 {
			return false
		}
	}

	return true
}

//FromSchema builds properties from the schema
func (w *ContentEntity) FromSchema(ctx context.Context, ref *openapi3.Schema) (*ContentEntity, error) {
	w.User.ID = weosContext.GetUser(ctx)
	w.Schema = ref
	instance := ds.NewStruct()
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
						if p.Value.Nullable {
							instance.AddField(name, []*string{}, strcase.SnakeCase(name))
						} else {
							instance.AddField(name, []string{}, strcase.SnakeCase(name))
						}

					} else if t2 == "number" {
						if p.Value.Nullable {
							instance.AddField(name, []*float64{}, strcase.SnakeCase(name))
						} else {
							instance.AddField(name, []float64{}, strcase.SnakeCase(name))
						}

					} else if t == "integer" {
						if p.Value.Nullable {
							instance.AddField(name, []*int{}, strcase.SnakeCase(name))
						} else {
							instance.AddField(name, []int{}, strcase.SnakeCase(name))
						}

					} else if t == "boolean" {
						if p.Value.Nullable {
							instance.AddField(name, []*bool{}, strcase.SnakeCase(name))
						} else {
							instance.AddField(name, []bool{}, strcase.SnakeCase(name))
						}

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
					instance.AddField(name, "", strcase.SnakeCase(name))
				} else if t == "number" {
					instance.AddField(name, 0.0, strcase.SnakeCase(name))
				} else if t == "integer" {
					instance.AddField(name, 0, strcase.SnakeCase(name))
				} else if t == "boolean" {
					instance.AddField(name, false, strcase.SnakeCase(name))
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
	err := json.Unmarshal(payload, &w.BasicEntity)
	if err != nil {
		return nil, err
	}
	if w.ID == "" {
		w.ID = ksuid.New().String()
	}
	event := NewEntityEvent("create", w, w.ID, payload)
	if err != nil {
		return nil, err
	}
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
	return reader.GetField(name).String()
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
	return reader.GetField(name).Int()
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
	return reader.GetField(name).Bool()
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
	return reader.GetField(name).Float64()
}

//ApplyChanges apply the new changes from payload to the entity
func (w *ContentEntity) ApplyChanges(changes []*Event) error {
	for _, change := range changes {
		w.SequenceNo = change.Meta.SequenceNo
		switch change.Type {
		case "create":
			err := json.Unmarshal(change.Payload, w.Property)
			if err != nil {
				return err
			}
			w.User.BasicEntity.ID = change.Meta.User
		}
	}
	return nil
}
