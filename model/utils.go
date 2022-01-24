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

//AddIDToPayload: This adds the weosID to the payload
func AddIDToPayload(payload []byte, weosID string) ([]byte, error) {
	var NewPayload []byte
	var tempPayload map[string]interface{}
	err := json.Unmarshal(payload, &tempPayload)
	if err != nil {
		return nil, err
	}
	tempPayload["weos_id"] = weosID

	NewPayload, err = json.Marshal(tempPayload)
	if err != nil {
		return nil, err
	}

	return NewPayload, nil
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

//GetSeqfromPayload: This returns the sequence number from payload
func GetSeqfromPayload(payload []byte) (string, error) {
	var tempPayload map[string]interface{}
	err := json.Unmarshal(payload, &tempPayload)
	if err != nil {
		return "", err
	}

	if tempPayload["sequence_no"] == nil {
		tempPayload["sequence_no"] = ""
	}

	seqNo := tempPayload["sequence_no"].(string)

	return seqNo, nil
}

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
