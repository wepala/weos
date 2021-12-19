package rest

import (
	"encoding/json"
	weos "github.com/wepala/weos-content-service/model"
)

type APIConfig struct {
	*weos.ApplicationConfig
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
	Middleware    []string `json:"middleware"`
	PreMiddleware []string `json:"pre-middleware"`
}

type HealthCheckResponse struct {
	Version string `json:"version"`
}
