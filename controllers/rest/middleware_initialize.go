package rest

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	ds "github.com/ompluscator/dynamic-struct"
	weosContext "github.com/wepala/weos/context"
	"github.com/wepala/weos/projections"
	"github.com/wepala/weos/utils"
	"golang.org/x/net/context"
)

//CreateSchema creates the table schemas for gorm syntax
func CreateSchema(ctx context.Context, e *echo.Echo, s *openapi3.Swagger) map[string]weosContext.ContentType {
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

	contentTypes := make(map[string]weosContext.ContentType)

	for name, scheme := range schemas {
		contentTypes[name] = weosContext.ContentType{
			Name:    name,
			Schema:  scheme.Value,
			Builder: builders[name],
		}
	}
	return contentTypes

}

//creates a new schema interface instance
func newSchema(ref *openapi3.Schema, logger echo.Logger) (ds.Builder, map[string]string, []string) {
	pks, _ := json.Marshal(ref.Extensions["x-identifier"])
	dfs, _ := json.Marshal(ref.Extensions["x-remove"])

	primaryKeys := []string{}
	deletedFields := []string{}
	json.Unmarshal(pks, &primaryKeys)
	json.Unmarshal(dfs, &deletedFields)

	if len(primaryKeys) == 0 {
		primaryKeys = append(primaryKeys, "id")
	}

	instance := ds.ExtendStruct(&projections.DefaultProjection{})

	relations := make(map[string]string)
	for name, p := range ref.Properties {
		found := false

		for _, n := range deletedFields {
			if strings.EqualFold(n, name) {
				found = true
			}
		}
		//this field should not be added to the schema
		if found {
			break
		}

		tagString := `json:"` + utils.SnakeCase(name) + `"`
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
						defaultValue = ""
					}
				case "number":
					defaultValue = 0.0
				case "integer":
					defaultValue = 0
				case "boolean":
					defaultValue = false
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

			relationKeys := keys[relationName]
			tableKeys := keys[tableName]
			inst := structs[relationName]
			f := inst.GetField("Table")
			f.SetTag(`json:"table_alias" gorm:"default:` + relationName + `"`)
			instances := inst.Build().NewSliceOfStructs()
			struc.AddField(name, instances, `json:"`+utils.SnakeCase(name)+`" gorm:"many2many:`+utils.SnakeCase(tableName)+"_"+utils.SnakeCase(name)+`;foreignKey:`+strings.Join(tableKeys, ",")+`;References:`+strings.Join(relationKeys, ",")+`"`)
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
				struc.AddField(strings.Title(name)+strings.Title(k), val, `json:"`+utils.SnakeCase(name)+`_`+k+`"`)
				if keystring != "" {
					keystring += ","
				}

				keystring += strings.Title(name) + strings.Title(k)
			}

			struc.AddField(name, instance, `json:"`+utils.SnakeCase(name)+`" gorm:"foreignKey:`+keystring+`; references `+strings.Join(key, ",")+`"`)
		}
	}
	return struc, nil
}

//AddStandardController adds controller to the path
func AddStandardController(e *echo.Echo, pathData *openapi3.PathItem, method string, swagger *openapi3.Swagger, operationConfig *PathConfig) (bool, error) {
	autoConfigure := false
	switch strings.ToUpper(method) {
	case "POST":
		if pathData.Post.RequestBody == nil {
			e.Logger.Warnf("unexpected error: expected request body but got nil")
			break
		}
		//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
		for _, value := range pathData.Post.RequestBody.Value.Content {
			if strings.Contains(value.Schema.Ref, "#/components/schemas/") {
				operationConfig.Handler = "Create"
				autoConfigure = true
			} else if value.Schema.Value.Type == "array" && value.Schema.Value.Items != nil && strings.Contains(value.Schema.Value.Items.Ref, "#/components/schemas/") {
				operationConfig.Handler = "CreateBatch"
				autoConfigure = true

			}
		}
	case "PUT":
		allParam := true
		if pathData.Put.RequestBody == nil {
			break
		}
		//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
		for _, value := range pathData.Put.RequestBody.Value.Content {
			if strings.Contains(value.Schema.Ref, "#/components/schemas/") {
				var identifiers []string
				identifierExtension := swagger.Components.Schemas[strings.Replace(value.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IdentifierExtension]
				if identifierExtension != nil {
					bytesId := identifierExtension.(json.RawMessage)
					json.Unmarshal(bytesId, &identifiers)
				}
				var contextName string
				//check for identifiers
				if identifiers != nil && len(identifiers) > 0 {
					for _, identifier := range identifiers {
						//check the parameters for the identifiers
						for _, param := range pathData.Put.Parameters {
							cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
							if identifier == param.Value.Name || (cName != nil && identifier == cName.(string)) {
								break
							}
							if !(identifier == param.Value.Name) && !(cName != nil && identifier == cName.(string)) {
								allParam = false
								e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
								return autoConfigure, nil
							}
						}
					}
					if allParam {
						operationConfig.Handler = "Update"
						autoConfigure = true
						break
					}
				}
				//if there is no identifiers then id is the default identifier
				for _, param := range pathData.Put.Parameters {

					if "id" == param.Value.Name {
						operationConfig.Handler = "Update"
						autoConfigure = true
						break
					}
					interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
					if interfaceContext != nil {
						bytesContext := interfaceContext.(json.RawMessage)
						json.Unmarshal(bytesContext, &contextName)
						if "id" == contextName {
							operationConfig.Handler = "Update"
							autoConfigure = true
							break
						}
					}
				}
			}
		}

	case "PATCH":
		allParam := true
		if pathData.Patch.RequestBody == nil {
			break
		}
		//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
		for _, value := range pathData.Patch.RequestBody.Value.Content {
			if strings.Contains(value.Schema.Ref, "#/components/schemas/") {
				var identifiers []string
				identifierExtension := swagger.Components.Schemas[strings.Replace(value.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IdentifierExtension]
				if identifierExtension != nil {
					bytesId := identifierExtension.(json.RawMessage)
					json.Unmarshal(bytesId, &identifiers)
				}
				var contextName string
				//check for identifiers
				if identifiers != nil && len(identifiers) > 0 {
					for _, identifier := range identifiers {
						//check the parameters for the identifiers
						for _, param := range pathData.Patch.Parameters {
							cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
							if identifier == param.Value.Name || (cName != nil && identifier == cName.(string)) {
								break
							}
							if !(identifier == param.Value.Name) && !(cName != nil && identifier == cName.(string)) {
								allParam = false
								e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
								return autoConfigure, nil
							}
						}
					}
					if allParam {
						operationConfig.Handler = "Update"
						autoConfigure = true
						break
					}
				}
				//if there is no identifiers then id is the default identifier
				for _, param := range pathData.Patch.Parameters {

					if "id" == param.Value.Name {
						operationConfig.Handler = "Update"
						autoConfigure = true
						break
					}
					interfaceContext := param.Value.ExtensionProps.Extensions[ContextNameExtension]
					if interfaceContext != nil {
						bytesContext := interfaceContext.(json.RawMessage)
						json.Unmarshal(bytesContext, &contextName)
						if "id" == contextName {
							operationConfig.Handler = "Update"
							autoConfigure = true
							break
						}
					}
				}
			}
		}
	case "GET":
		allParam := true
		//check to see if the path can be autoconfigured. If not show a warning to the developer is made aware
		//checks if the response refers to a schema
		if pathData.Get.Responses != nil && pathData.Get.Responses["200"].Value.Content != nil {
			for _, val := range pathData.Get.Responses["200"].Value.Content {
				if strings.Contains(val.Schema.Ref, "#/components/schemas/") {
					var identifiers []string
					identifierExtension := swagger.Components.Schemas[strings.Replace(val.Schema.Ref, "#/components/schemas/", "", -1)].Value.ExtensionProps.Extensions[IdentifierExtension]
					if identifierExtension != nil {
						bytesId := identifierExtension.(json.RawMessage)
						err := json.Unmarshal(bytesId, &identifiers)
						if err != nil {
							return autoConfigure, err
						}
					}
					var contextName string
					if identifiers != nil && len(identifiers) > 0 {
						for _, identifier := range identifiers {
							//check the parameters
							for _, param := range pathData.Get.Parameters {
								cName := param.Value.ExtensionProps.Extensions[ContextNameExtension]
								if identifier == param.Value.Name || (cName != nil && identifier == cName.(string)) {
									break
								}
								if !(identifier == param.Value.Name) && !(cName != nil && identifier == cName.(string)) {
									allParam = false
									e.Logger.Warnf("unexpected error: a parameter for each part of the identifier must be set")
									return autoConfigure, nil
								}
							}
						}
					}
					//check the parameters for id
					if pathData.Get.Parameters != nil && len(pathData.Get.Parameters) != 0 {
						for _, param := range pathData.Get.Parameters {
							if "id" == param.Value.Name {
								allParam = true
							}
							contextInterface := param.Value.ExtensionProps.Extensions[ContextNameExtension]
							if contextInterface != nil {
								bytesContext := contextInterface.(json.RawMessage)
								json.Unmarshal(bytesContext, &contextName)
								if "id" == contextName {
									allParam = true
								}
							}
						}
					}
					if allParam {
						operationConfig.Handler = "View"
						autoConfigure = true
						break
					}
				}
				//checks if the response refers to an array schema
				if val.Schema.Value.Properties != nil && val.Schema.Value.Properties["items"] != nil && val.Schema.Value.Properties["items"].Value.Type == "array" && val.Schema.Value.Properties["items"].Value.Items != nil && strings.Contains(val.Schema.Value.Properties["items"].Value.Items.Ref, "#/components/schemas/") {
					operationConfig.Handler = "List"
					autoConfigure = true
					break
				}
				if val.Schema.Value.Properties != nil {
					var alias string
					for _, prop := range val.Schema.Value.Properties {
						aliasInterface := prop.Value.ExtensionProps.Extensions[AliasExtension]
						if aliasInterface != nil {
							bytesContext := aliasInterface.(json.RawMessage)
							json.Unmarshal(bytesContext, &alias)
							if alias == "items" {
								if prop.Value.Type == "array" && prop.Value.Items != nil && strings.Contains(prop.Value.Items.Ref, "#/components/schemas/") {
									operationConfig.Handler = "List"
									autoConfigure = true
									break
								}
							}
						}

					}
				}

			}
		}
	}

	return autoConfigure, nil
}
