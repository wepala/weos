package rest

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/wepala/weos-service/projections"
	"github.com/wepala/weos-service/utils"
	"golang.org/x/net/context"
)

//ToDo: Saving the structs to a map and making them entities to save events

const WEOS_SCHEMA = "WEOS-Schemas"

//CreateSchema creates the table schemas for gorm syntax
func CreateSchema(ctx context.Context, e *echo.Echo, s *openapi3.Swagger) map[string]interface{} {
	structs := make(map[string]interface{})
	relations := make(map[string]map[string]string)
	schemas := s.Components.Schemas
	for name, scheme := range schemas {
		var instance interface{}
		instance, relations[name] = newSchema(scheme.Value, name, e.Logger)
		structs[name] = instance
	}
	return structs

}

//creates a new schema interface instance
func newSchema(ref *openapi3.Schema, tableName string, logger echo.Logger) (interface{}, map[string]string) {
	pks, _ := json.Marshal(ref.Extensions["x-identifier"])

	primaryKeys := []string{}
	json.Unmarshal(pks, &primaryKeys)

	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	instance := ds.ExtendStruct(&projections.DefaultProjection{})

	relations := make(map[string]string)
	for name, p := range ref.Properties {
		tagString := `json:"` + utils.SnakeCase(name) + `"`
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
						if p.Value.Items.Value.Format == "date-time" {
							instance.AddField(name, time.Now(), tagString)
						} else {
							instance.AddField(name, []string{}, tagString)
						}
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
					if p.Value.Format == "date-time" {
						instance.AddField(name, time.Now(), tagString)
					} else {
						instance.AddField(name, "", tagString)
					}
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

	err := json.Unmarshal([]byte(`{
			"table_alias": "`+tableName+`"
		}`), &inst)
	if err != nil {
		logger.Errorf("unable to set the table name '%s'", err)
	}

	return inst, relations
}
