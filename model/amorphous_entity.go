package model

import (
	"encoding/json"
	"fmt"
)

const UI_SINGLE_LINE = "singleLine"
const UI_CHECKBOX = "checkbox"
const UI_MULTI_LINE = "multiLine"

//Property interface that all fields should implement
type Property interface {
	IsValid() bool
	GetType() string
	GetLabel() string
	GetErrors() []error
}

//BasicProperty is basic struct for a property
type BasicProperty struct {
	Type       string      `json:"type"`
	UI         string      `json:"ui"`
	Label      string      `json:"label"`
	Value      interface{} `json:"value"`
	IsRequired bool        `json:"is_required"`
	errors     []error
}

func (b *BasicProperty) GetType() string {
	return b.Type
}
func (b *BasicProperty) GetLabel() string {
	return b.Label
}
func (b *BasicProperty) GetErrors() []error {
	return b.errors
}

//StringProperty basic string property
type StringProperty struct {
	BasicProperty
	Value string `json:"value"`
}

//IsValid add rules for validating value
func (s *StringProperty) IsValid() bool {
	if s.IsRequired && s.Value == "" {
		s.errors = append(s.errors, fmt.Errorf("'%s' is required", s.Label))
		return false
	}
	return true
}

//FromLabelAndValue create property using label
func (s *StringProperty) FromLabelAndValue(label string, value string, isRequired bool) *StringProperty {

	s.BasicProperty.Type = "string"
	s.BasicProperty.Label = label
	s.Value = value
	s.BasicProperty.IsRequired = isRequired
	s.BasicProperty.UI = UI_SINGLE_LINE //Sets default

	return s
}

//BooleanProperty basic string property
type BooleanProperty struct {
	BasicProperty
	Value bool `json:"value"`
}

//IsValid add rules for validating value
func (b *BooleanProperty) IsValid() bool {
	return true
}

//FromLabelAndValue create property using label
func (b *BooleanProperty) FromLabelAndValue(label string, value bool, isRequired bool) *BooleanProperty {

	b.BasicProperty.Type = "boolean"
	b.BasicProperty.Label = label
	b.Value = value
	b.BasicProperty.IsRequired = isRequired
	b.BasicProperty.UI = UI_CHECKBOX //Sets default

	return b
}

//NumericProperty basic string property
type NumericProperty struct {
	BasicProperty
	Value float32 `json:"value"`
}

//IsValid add rules for validating value
func (n *NumericProperty) IsValid() bool {
	if n.IsRequired && n.Value == 0 {
		n.errors = append(n.errors, fmt.Errorf("'%s' is required", n.Label))
		return false
	}
	return true
}

//FromLabelAndValue create property using label
func (n *NumericProperty) FromLabelAndValue(label string, value float32, isRequired bool) *NumericProperty {

	n.BasicProperty.Type = "numeric"
	n.BasicProperty.Label = label
	n.Value = value
	n.BasicProperty.IsRequired = isRequired
	n.BasicProperty.UI = UI_SINGLE_LINE //Sets default

	return n
}

type AmorphousEntity struct {
	*BasicEntity
	Properties map[string]Property `json:"properties"`
}

func (e *AmorphousEntity) Get(label string) Property {
	return e.Properties[label]
}
func (e *AmorphousEntity) Set(property Property) {
	if e == nil {
		e = &AmorphousEntity{}
	}
	if e.Properties == nil {
		e.Properties = make(map[string]Property)
	}
	e.Properties[property.GetLabel()] = property
}

//Umarshall AmorphousEntity into interface provided
func (e *AmorphousEntity) UnmarshalJSON(data []byte) error {

	var v map[string]interface{}
	json.Unmarshal(data, &v) //Saves all the data in a interface map

	newProperty := make(map[string]interface{})
	newProperty = v["properties"].(map[string]interface{}) //Extracts the property map from the interface map
	if len(newProperty) == 0 {
		return nil
	}

	for _, prop := range newProperty { //Iterates through the individual properties within the property map
		currProp := make(map[string]interface{})
		currProp = prop.(map[string]interface{}) //Extracts the current property

		currPropType := currProp["type"].(string) //Asserts the current property type to string and saves it for comparison

		if currPropType == "string" {
			stringProp := new(StringProperty).FromJSON(currProp)
			if stringProp != nil {
				if e.Properties == nil {
					e.Properties = make(map[string]Property)
				}
				e.Properties[stringProp.GetLabel()] = stringProp
			}
		}
		if currPropType == "boolean" {
			booleanProp := new(BooleanProperty).FromJSON(currProp)
			if booleanProp != nil {
				if e.Properties == nil {
					e.Properties = make(map[string]Property)
				}
				e.Properties[booleanProp.GetLabel()] = booleanProp
			}
		}
		if currPropType == "numeric" {
			numericProp := new(NumericProperty).FromJSON(currProp)
			if numericProp != nil {
				if e.Properties == nil {
					e.Properties = make(map[string]Property)
				}
				e.Properties[numericProp.GetLabel()] = numericProp
			}
		}
	}

	return nil
}

func (s *StringProperty) FromJSON(prop map[string]interface{}) *StringProperty {
	if len(prop) == 0 {
		return nil
	}

	s.BasicProperty.Type = prop["type"].(string)
	s.BasicProperty.Label = prop["label"].(string)
	s.Value = prop["value"].(string)
	s.BasicProperty.IsRequired = prop["is_required"].(bool)

	if prop["ui"].(string) == "" {
		s.BasicProperty.UI = UI_SINGLE_LINE
	} else {
		s.BasicProperty.UI = prop["ui"].(string)
	}
	return s
}

func (b *BooleanProperty) FromJSON(prop map[string]interface{}) *BooleanProperty {
	if len(prop) == 0 {
		return nil
	}

	b.BasicProperty.Type = prop["type"].(string)
	b.BasicProperty.Label = prop["label"].(string)
	b.Value = prop["value"].(bool)
	b.BasicProperty.IsRequired = prop["is_required"].(bool)

	if prop["ui"].(string) == "" {
		b.BasicProperty.UI = UI_CHECKBOX
	} else {
		b.BasicProperty.UI = prop["ui"].(string)
	}
	return b
}

func (n *NumericProperty) FromJSON(prop map[string]interface{}) *NumericProperty {
	if len(prop) == 0 {
		return nil
	}

	n.BasicProperty.Type = prop["type"].(string)
	n.BasicProperty.Label = prop["label"].(string)
	n.Value = float32(prop["value"].(float64))
	n.BasicProperty.IsRequired = prop["is_required"].(bool)

	if prop["ui"].(string) == "" {
		n.BasicProperty.UI = UI_SINGLE_LINE
	} else {
		n.BasicProperty.UI = prop["ui"].(string)
	}
	return n

}
