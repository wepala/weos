package rest

import (
	"encoding/json"
)

type ServiceConfig struct {
	ModuleID      string      `json:"moduleId"`
	Title         string      `json:"title"`
	AccountID     string      `json:"accountId"`
	ApplicationID string      `json:"applicationId"`
	AccountName   string      `json:"accountName"`
	Database      *DBConfig   `json:"database"`
	Databases     []*DBConfig `json:"databases"`
	Log           *LogConfig  `json:"log"`
	BaseURL       string      `json:"baseURL"`
	LoginURL      string      `json:"loginURL"`
	GraphQLURL    string      `json:"graphQLURL"`
	SessionKey    string      `json:"sessionKey"`
	Secret        string      `json:"secret"`
	AccountURL    string      `json:"accountURL"`
}

type DBConfig struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	User        string `json:"username"`
	Password    string `json:"password"`
	Port        int    `json:"port"`
	Database    string `json:"database"`
	Driver      string `json:"driver"`
	MaxOpen     int    `json:"max-open"`
	MaxIdle     int    `json:"max-idle"`
	MaxIdleTime int    `json:"max-idle-time"`
	AwsIam      bool   `json:"aws-iam"`
	AwsRegion   string `json:"aws-region"`
}

type LogConfig struct {
	Level        string `json:"level"`
	Name         string `json:"name"`
	ReportCaller bool   `json:"report-caller"`
	Formatter    string `json:"formatter"`
}

type APIConfig struct {
	*ServiceConfig
	BasePath            string `json:"basePath" ,yaml:"basePath"`
	RecordingBaseFolder string
	Rest                *Rest           `json:"rest"`
	JWTConfig           *JWTConfig      `json:"jwtConfig"`
	Config              json.RawMessage `json:"config"`
	Version             string          `json:"version"`
}

type PathConfig struct {
	Handler        string          `json:"handler" ,yaml:"handler"`
	Group          bool            `json:"group" ,yaml:"group"`
	Middleware     []string        `json:"middleware"`
	Config         json.RawMessage `json:"config"`
	DisableCors    bool            `json:"disable-cors"`
	AllowedHeaders []string        `json:"allowed-headers" ,yaml:"allowed-headers"`
	AllowedOrigins []string        `json:"allowed-origins" ,yaml:"allowed-origins"`
}

type JWTConfig struct {
	Key             string                 `json:"key"`         //Signing key needed for validating token
	SigningKeys     map[string]interface{} `json:"signingKeys"` //Key map used for validating token.  Can be used in place of a single key
	Certificate     []byte                 `json:"certificate"`
	CertificatePath string                 `json:"certificatePath"` //Path the signing certificate used to validate token.  Can  be used in place of a key
	JWKSUrl         string                 `json:"jwksUrl"`         //URL to JSON Web Key set.  Can be used in place of a Key
	TokenLookup     string                 `json:"tokenLookup"`
	Claims          map[string]interface{} `json:"claims"`
	AuthScheme      string                 `json:"authScheme"`
	ContextKey      string                 `json:"contextKey"`
	SigningMethod   string                 `json:"signingMethod"`
}

type Rest struct {
	Middleware    []string `json:"x-middleware"`
	PreMiddleware []string `json:"pre-middleware"`
}

// HealthCheckResponse used int he health check controller to return a response with version
type HealthCheckResponse struct {
	Version string `json:"version"`
}

// ListApiResponse used in the list controller to return a response with total, page and items retrieved
type ListApiResponse struct {
	Total int64           `json:"total"`
	Page  int             `json:"page"`
	Items []BasicResource `json:"items"`
}

// FilterProperties is the properties need to use filters
type FilterProperties struct {
	Field    string        `json:"field"`
	Operator string        `json:"operator"`
	Value    interface{}   `json:"value"`
	Values   []interface{} `json:"values"`
}

// QueryProperties is the properties needed to use key value pair query parameters
type QueryProperties struct {
	Value string `json:"value"`
	Field string `json:"field"`
}

type CResponseType struct {
	Status string `json:"status"`
	Type   string `json:"Type"`
}
