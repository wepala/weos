package projections

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/stoewer/go-strcase"
)

//ToDo: Saving the structs to a map and making them entities to save events
func CreateSchema(ctx context.Context, schemas map[string]*openapi3.SchemaRef) (map[string]interface{}, error) {
	structs := make(map[string]interface{})
	relations := make(map[string]map[string]string)

	for name, scheme := range schemas {
		var instance interface{}
		instance, relations[name] = updateSchema(scheme.Value, name)

		structs[name] = instance
	}

	//reloop through and add object relations

	return structs, nil
}

func updateSchema(ref *openapi3.Schema, tableName string) (interface{}, map[string]string) {
	instance := ds.ExtendStruct(&DefaultProjection{})
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
						instance.AddField(name, []string{}, `json:"`+strcase.SnakeCase(name)+`"`)
					} else if t2 == "number" {
						instance.AddField(name, []float64{}, `json:"`+strcase.SnakeCase(name)+`"`)
					} else if t == "integer" {
						instance.AddField(name, []int{}, `json:"`+strcase.SnakeCase(name)+`"`)
					} else if t == "boolean" {
						instance.AddField(name, []bool{}, `json:"`+strcase.SnakeCase(name)+`"`)
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
					instance.AddField(name, "", `json:"`+strcase.SnakeCase(name)+`"`)
				} else if t == "number" {
					instance.AddField(name, 0.0, `json:"`+strcase.SnakeCase(name)+`"`)
				} else if t == "integer" {
					instance.AddField(name, 0, `json:"`+strcase.SnakeCase(name)+`"`)
				} else if t == "boolean" {
					instance.AddField(name, false, `json:"`+strcase.SnakeCase(name)+`"`)
				}
			}
		}
	}

	inst := instance.Build().New()

	json.Unmarshal([]byte(`
		{
			"table_alias": "`+tableName+`",
			"type": "`+tableName+`"
		}
	`), &inst)

	bytes, _ := json.Marshal(inst)
	fmt.Println("structure from service: ", string(bytes))
	return inst, relations
}
