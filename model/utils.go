package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos/utils"
	"reflect"
	"time"
)

//Time wrapper that marshals as iso8601 which is what open api uses instead of rfc3339Nano
type Time struct {
	time.Time
}

func (t Time) MarshalJSON() ([]byte, error) {
	if y := t.Year(); y < 0 || y >= 10000 {
		// RFC 3339 is clear that years are 4 digits exactly.
		// See golang.org/issue/4556#c15 for more discussion.
		return nil, errors.New("Time.MarshalJSON: year outside of range [0,9999]")
	}
	iso8601Format := "2006-01-02T15:04:05Z"
	b := make([]byte, 0, len(iso8601Format)+2)
	b = append(b, '"')
	b = t.AppendFormat(b, iso8601Format)
	b = append(b, '"')
	return b, nil
}

//Scan implement Scanenr interface for Gorm
func (t *Time) Scan(value interface{}) error {
	var err error
	if date, ok := value.(string); ok {
		t.Time, err = time.Parse("2006-01-02 15:04:05", date)
	}
	return err
}

// Value return time value, implement driver.Valuer interface
func (t Time) Value() (driver.Value, error) {
	return t.Time, nil
}

func NewTime(time time.Time) *Time {
	return &Time{Time: time}
}

func GetType(myvar interface{}) string {
	if t := reflect.TypeOf(myvar); t.Kind() == reflect.Ptr {
		if t.Elem().Name() == "ContentEntity" {
			ttype := myvar.(*ContentEntity).Name
			if ttype != "" {
				return ttype
			}

		}
		return t.Elem().Name()
	} else {
		if t.Name() == "ContentEntity" {
			ttype := myvar.(*ContentEntity).Name
			if ttype != "" {
				return ttype
			}
		}
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

func InList(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
