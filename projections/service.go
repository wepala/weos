package projections

import (
	"context"
	"encoding/json"
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
	pks, _ := json.Marshal(ref.Extensions["x-identifier"])

	primaryKeys := []string{}
	json.Unmarshal(pks, &primaryKeys)

	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	instance := ds.ExtendStruct(&DefaultProjection{})

	relations := make(map[string]string)
	for name, p := range ref.Properties {
		tagString := `json:"` + strcase.SnakeCase(name) + `"`
		if strings.Contains(strings.Join(primaryKeys, " "), strings.ToLower(name)) {
			tagString += ` gorm:"primaryKey;size:512;NOT NULL"`
		}
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
						instance.AddField(name, []string{}, tagString)
					} else if t2 == "number" {
						instance.AddField(name, []float64{}, tagString)
					} else if t == "integer" {
						instance.AddField(name, []int{}, tagString)
					} else if t == "boolean" {
						instance.AddField(name, []bool{}, tagString)
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
					instance.AddField(name, "", tagString)
				} else if t == "number" {
					instance.AddField(name, 0.0, tagString)
				} else if t == "integer" {
					instance.AddField(name, 0, tagString)
				} else if t == "boolean" {
					instance.AddField(name, false, tagString)
				}
			}
		}
	}

	if primaryKeys[0] == "id" && !instance.HasField("Id") {
		instance.AddField("Id", uint(0), `json:"id" gorm:"primaryKey;size:512"`)
	}

	inst := instance.Build().New()

	json.Unmarshal([]byte(`
		{
			"table_alias": "`+tableName+`",
		}
	`), &inst)

	return inst, relations
}
