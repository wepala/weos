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

func (w *ContentEntity) IsValid() bool {
	for _, req := range w.Schema.Required {
		if req == "id" {
			if w.ID == "" {
				return false
			}
			continue
		}
		switch w.Schema.Properties[req].Value.Type {
		case "string":
			if w.GetString(strings.Title(req)) == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

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
						instance.AddField(name, []string{}, strcase.SnakeCase(name))
					} else if t2 == "number" {
						instance.AddField(name, []float64{}, strcase.SnakeCase(name))
					} else if t == "integer" {
						instance.AddField(name, []int{}, strcase.SnakeCase(name))
					} else if t == "boolean" {
						instance.AddField(name, []bool{}, strcase.SnakeCase(name))
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

func (w *ContentEntity) FromSchemaWithValues(ctx context.Context, schema *openapi3.Schema, payload json.RawMessage) (*ContentEntity, error) {
	w.FromSchema(ctx, schema)
	err := json.Unmarshal(payload, &w.BasicEntity)
	if err != nil {
		return nil, err
	}
	if w.ID == "" {
		w.ID = ksuid.New().String()
	}
	err = json.Unmarshal(payload, &w.Property)
	if err != nil {
		return nil, err
	}
	validating := w.IsValid()
	if !validating {
		return nil, NewDomainError("payload is invalid", weosContext.GetContentType(ctx).Name, "", nil)
	}
	event := NewEntityEvent("create", w, w.ID, payload)
	if err != nil {
		return nil, err
	}
	w.NewChange(event)

	return w, w.ApplyChanges([]*Event{event})
}

func (w *ContentEntity) GetString(name string) string {
	if w.Property == nil {
		return ""
	}
	reader := ds.NewReader(w.Property)
	return reader.GetField(name).String()
}

func (w *ContentEntity) ApplyChanges(changes []*Event) error {
	for _, change := range changes {
		w.SequenceNo = change.Meta.SequenceNo
		switch change.Type {
		case "create":
			var payload *ContentEntity
			err := json.Unmarshal(change.Payload, w)
			if err != nil {
				return err
			}
			err = json.Unmarshal(change.Payload, &payload)
			if err != nil {
				return err
			}
			w.User.BasicEntity.ID = change.Meta.User
		}
	}
	return nil
}
