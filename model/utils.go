package model

import (
	"encoding/json"
	"reflect"
	"strconv"
)

func GetType(myvar interface{}) string {
	if t := reflect.TypeOf(myvar); t.Kind() == reflect.Ptr {
		return t.Elem().Name()
	} else {
		return t.Name()
	}
}

//NewEtag: This takes in a contentEntity and concatenates the weosID and SequenceID
func NewEtag(entity *ContentEntity) string {
	seqNo := int(entity.SequenceNo)
	ETag := entity.ID + "." + strconv.Itoa(seqNo)
	return ETag
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
