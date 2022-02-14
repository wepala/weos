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
	relations := make(map[string]map[string]string)
	keys := make(map[string][]string)
	schemas := s.Components.Schemas
	for name, scheme := range schemas {
		var instance ds.Builder
		instance, relations[name], keys[name] = newSchema(scheme.Value, e.Logger)
		builders[name] = instance
	}

	//rearrange so schemas without primary keys are first

	for name, scheme := range builders {
		if relations, ok := relations[name]; ok {
			if len(relations) != 0 {
				var err error
				scheme, err = addRelations(scheme, relations, builders, name, keys, e.Logger)
				if err != nil {
					e.Logger.Fatalf("Got an error creating the application schema '%s'", err.Error())
				}
			}
		}
		builders[name] = scheme
	}
	return builders

}

//creates a new schema interface instance
func newSchema(ref *openapi3.Schema, logger echo.Logger) (ds.Builder, map[string]string, []string) {
	pks, _ := json.Marshal(ref.Extensions["x-identifier"])

	primaryKeys := []string{}
	json.Unmarshal(pks, &primaryKeys)

	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	instance := ds.ExtendStruct(&projections.DefaultProjection{})

	relations := make(map[string]string)
	for name, p := range ref.Properties {
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
			relations[name] = strings.TrimPrefix(p.Ref, "#/components/schemas/")
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
						relations[name] = "[]" + strings.TrimPrefix(p.Value.Items.Ref, "#/components/schemas/")

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

	if primaryKeys[0] == "id" && !instance.HasField("Id") {
		instance.AddField("Id", uint(0), `json:"id" gorm:"primaryKey;size:512"`)
	}

	return instance, relations, primaryKeys
}

func addRelations(struc ds.Builder, relations map[string]string, structs map[string]ds.Builder, tableName string, keys map[string][]string, logger echo.Logger) (ds.Builder, error) {

	for name, relation := range relations {
		if strings.Contains(relation, "[]") {
			//many to many relationship
			relationName := strings.Trim(relation, "[]")
			inst := structs[relationName]
			f := inst.GetField("Table")
			f.SetTag(`json:"table_alias" gorm:"default:` + relationName + `"`)
			instances := inst.Build().NewSliceOfStructs()
			struc.AddField(name, instances, `json:"`+utils.SnakeCase(name)+`" gorm:"many2many:`+utils.SnakeCase(tableName)+"_"+utils.SnakeCase(name)+`;"`)
		} else {
			inst := structs[relation]
			f := inst.GetField("Table")
			f.SetTag(`json:"table_alias" gorm:"default:` + name + `"`)
			instance := inst.Build().New()
			key := keys[relation]
			bytes, _ := json.Marshal(instance)
			s := map[string]interface{}{}
			json.Unmarshal(bytes, &s)
			keystring := ""
			for _, k := range key {
				val := s[k]
				//foreign key references must be nullable
				if _, ok := val.(string); ok {
					var s *string
					val = s
				} else if _, ok := val.(uint); ok {
					var s *uint
					val = s
				} else if _, ok := val.(int); ok {
					var s *int
					val = s
				} else if _, ok := val.(float64); ok {
					var s *float64
					val = s
				} else if _, ok := val.(bool); ok {
					var s *bool
					val = s
				}
				//default to string if nil
				if val == nil {
					var s *string
					val = s
				}
				struc.AddField(strings.Title(name)+strings.Title(k), val, `json:"`+utils.SnakeCase(name)+`_`+k+`"`)
				if keystring != "" {
					keystring += ","
				}

				keystring += strings.Title(name) + strings.Title(k)
			}

			struc.AddField(name, instance, `json:"`+name+`" gorm:"foreignKey:`+keystring+`; references `+strings.Join(key, ",")+`"`)
		}
	}
	return struc, nil
}
