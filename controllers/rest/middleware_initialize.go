package rest

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/wepala/weos/projections"
	"github.com/wepala/weos/utils"
	"golang.org/x/net/context"
)

//CreateSchema creates the table schemas for gorm syntax
func CreateSchema(ctx context.Context, e *echo.Echo, s *openapi3.Swagger) map[string]ds.Builder {
	builders := make(map[string]ds.Builder)
	schemas := s.Components.Schemas
	for name, scheme := range schemas {
		var instance ds.Builder
		instance, _ = newSchema(name, scheme.Value, schemas, 0, e.Logger)
		builders[name] = instance
	}

	return builders

}

//creates a new schema interface instance
func newSchema(currTable string, ref *openapi3.Schema, schemaRefs map[string]*openapi3.SchemaRef, count int, logger echo.Logger) (ds.Builder, []string) {
	if count > 1 {
		return nil, nil
	}
	pks, _ := json.Marshal(ref.Extensions[IdentifierExtension])
	dfs, _ := json.Marshal(ref.Extensions[RemoveExtension])

	primaryKeys := []string{}
	deletedFields := []string{}

	json.Unmarshal(pks, &primaryKeys)
	json.Unmarshal(dfs, &deletedFields)

	//was a primary key removed but not removed in the x-identifier fields?
	for i, k := range primaryKeys {
		for _, d := range deletedFields {
			if strings.EqualFold(k, d) {
				if len(primaryKeys) == 1 {
					primaryKeys = []string{}
				} else {
					primaryKeys[i] = primaryKeys[len(primaryKeys)-1]
					primaryKeys = primaryKeys[:len(primaryKeys)-1]
				}
			}
		}
	}

	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	instance := ds.ExtendStruct(&projections.DefaultProjection{})

	for name, p := range ref.Properties {
		found := false

		for _, n := range deletedFields {
			if strings.EqualFold(n, name) {
				found = true
			}
		}
		//this field should not be added to the schema
		if found {
			continue
		}

		tagString := `json:"` + name + `"`
		var gormParts []string
		for _, req := range ref.Required {
			if strings.EqualFold(req, name) {
				gormParts = append(gormParts, "NOT NULL")
			}
		}

		if strings.Contains(strings.Join(primaryKeys, " "), strings.ToLower(name)) {
			gormParts = append(gormParts, "primaryKey", "size:512")
			//only add NOT null if it's not already in the array to avoid issue if a user also add the field to the required array
			if !strings.Contains(strings.Join(gormParts, ";"), "NOT NULL") {
				gormParts = append(gormParts, "NOT NULL")
			}
		}
		name = strings.Title(name)
		//setup gorm field tag string
		if len(gormParts) > 0 {
			gormString := strings.Join(gormParts, ";")
			tagString += ` gorm:"` + gormString + `"`
		}

		if p.Ref != "" {
			structName := strings.TrimPrefix(p.Ref, "#/components/schemas/")
			tagString := `json:"` + name + `"`
			r, rKeys := newSchema(structName, schemaRefs[structName].Value, schemaRefs, count+1, logger)
			if r != nil {
				f := r.GetField("Table")
				f.SetTag(`json:"table_alias" gorm:"default:` + structName + `"`)
				rStruct := r.Build().New()
				keystring := ""
				reader := ds.NewReader(rStruct)

				//add key references
				for _, k := range rKeys {
					instance.AddField(strings.Title(name)+strings.Title(k), reader.GetField(strings.Title(k)).Interface(), `json:"`+utils.SnakeCase(name)+`_`+k+`"`)
					if keystring != "" {
						keystring += ","
					}

					keystring += strings.Title(name) + strings.Title(k)
				}

				if len(gormParts) == 0 {
					tagString += ` gorm:"foreignKey:` + keystring + `; References: ` + strings.Join(rKeys, ",") + `"`
				} else {
					tagString += `;foreignKey:` + keystring + `; References: ` + strings.Join(rKeys, ",") + `"`
				}

				instance.AddField(name, rStruct, tagString)
			}
		} else {
			t := p.Value.Type
			if strings.EqualFold(t, "array") {
				t2 := p.Value.Items.Value.Type
				if t2 != "object" {
					if t2 == "string" {
						if p.Value.Items.Value.Format == "date-time" {
							instance.AddField(name, []time.Time{}, tagString)
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
						structName := strings.TrimPrefix(p.Value.Items.Ref, "#/components/schemas/")
						r, _ := newSchema(structName, schemaRefs[structName].Value, schemaRefs, count+1, logger)
						if r != nil {
							f := r.GetField("Table")
							f.SetTag(`json:"table_alias" gorm:"default:` + structName + `"`)
							rArray := r.Build().NewSliceOfStructs()
							if len(gormParts) == 0 {
								tagString += ` gorm:"many2many:` + utils.SnakeCase(currTable) + "_" + utils.SnakeCase(name) + `;"`
							} else {
								tagString += `;many2many:` + utils.SnakeCase(currTable) + "_" + utils.SnakeCase(name) + `;"`
							}
							instance.AddField(name, rArray, tagString)
						}
					}
				}

			} else if strings.EqualFold(t, "object") {
				//add json object

			} else {
				var defaultValue interface{}

				switch t {
				case "string":
					if p.Value.Format == "date-time" {
						defaultValue = time.Now()
					} else {
						var strings *string
						defaultValue = strings
					}
				case "number":
					var numbers *float32
					defaultValue = numbers
				case "integer":
					var integers *int
					defaultValue = integers
				case "boolean":
					var boolean *bool
					defaultValue = boolean
				}
				instance.AddField(name, defaultValue, tagString)

			}
		}
	}

	if len(primaryKeys) == 1 && primaryKeys[0] == "id" && !instance.HasField("Id") {
		instance.AddField("Id", uint(0), `json:"id" gorm:"primaryKey;size:512"`)
	}

	return instance, primaryKeys
}
