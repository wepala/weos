package model

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos/utils"
)

func GetType(myvar interface{}) string {
	if t := reflect.TypeOf(myvar); t.Kind() == reflect.Ptr {
		return t.Elem().Name()
	} else {
		return t.Name()
	}
}

//GetIDfromPayload: This returns the weosID from payload
func GetIDfromPayload(payload []byte) (string, error) {
	var tempPayload map[string]interface{}
	err := json.Unmarshal(payload, &tempPayload)
	if err != nil {
		return "", err
	}

	if tempPayload["weos_id"] == nil {
		tempPayload["weos_id"] = ""
	}

	weosID := tempPayload["weos_id"].(string)

	return weosID, nil
}

//Deprecated: 02/01/2022 not sure this is needed. Marshal into Property directly
//helper function used to parse string values to type
func ParseToType(bytes json.RawMessage, contentType *openapi3.Schema) (json.RawMessage, error) {

	payload := map[string]interface{}{}
	err := json.Unmarshal(bytes, &payload)
	if err != nil {
		return bytes, err
	}
	for name, p := range contentType.Properties {
		if p.Value != nil && p.Value.Type == "string" {
			if p.Value.Format == "date-time" {
				if _, ok := payload[utils.SnakeCase(name)].(string); ok {
					t, err := time.Parse("2006-01-02T15:04:00Z", payload[utils.SnakeCase(name)].(string))
					payload[utils.SnakeCase(name)] = t
					if err != nil {
						return bytes, err
					}
				}
			}
		}
	}
	bytes, err = json.Marshal(payload)
	return bytes, err
}

//GetSchemaURL returns the url for the schema
func GetSchemaURL(serverURL string, path string) string {
	return serverURL + SCHEMA_SEGMENT + path
}

func SchemaFromPayload(payload []byte) *openapi3.Schema {
	schema := &openapi3.Schema{
		Properties: make(map[string]*openapi3.SchemaRef),
	}
	tprops := make(map[string]interface{})
	if err := json.Unmarshal(payload, &tprops); err == nil {
		for k, v := range tprops {
			//if it's a boolean then set to string type
			if _, ok := v.(bool); ok {
				schema.Properties[k] = &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "boolean"}}
				//check to see if it's a date, email and set the format accordingly
			}
			//if it's a string then set to string type
			if _, ok := v.(string); ok {
				schema.Properties[k] = &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "string"}}
				//check to see if it's a date, email and set the format accordingly
			}
		}
	}
	return schema
}
