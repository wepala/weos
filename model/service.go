package model

import (
	"context"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/stoewer/go-strcase"
)

type Service struct {
	Repository
	eventRepository EventRepository
}

//ToDo: Saving the structs to a map and making them entities to save events
func (s *Service) CreateSchema(ctx context.Context, schemas map[string]*openapi3.SchemaRef) (map[string]interface{}, error) {
	structs := make(map[string]interface{})
	relations := make(map[string]map[string]string)

	for name, scheme := range schemas {
		var instance interface{}
		instance, relations[name] = updateSchema(scheme.Value)

		structs[name] = instance
	}

	//reloop through and add object relations

	return structs, nil
}

func updateSchema(ref *openapi3.Schema) (interface{}, map[string]string) {
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

	return instance.Build().New(), relations
}
