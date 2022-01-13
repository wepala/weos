package model

import (
	"encoding/json"
	"reflect"
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
