package rest

import (
	"encoding/json"
	"errors"
	"github.com/getkin/kin-openapi/openapi3"
	"go.uber.org/fx"
	"net/url"
	"os"
)

// Config loads the OpenAPI spec from the environment
func Config() (*openapi3.T, error) {
	spec := os.Getenv("WEOS_SPEC")
	if spec != "" {
		turl, err := url.Parse(spec)
		if err == nil {
			return openapi3.NewLoader().LoadFromURI(turl)
		} else {
			return openapi3.NewLoader().LoadFromFile(spec)

		}
	}
	return nil, errors.New("spec not found")
}

type WeOSConfigParams struct {
	fx.In
	Config *openapi3.T
	Logger Log
}

type WeOSConfigResult struct {
	fx.Out
	Config *APIConfig
}

func WeOSConfig(p WeOSConfigParams) (WeOSConfigResult, error) {
	if p.Config != nil {
		var config *APIConfig
		if data, ok := p.Config.Extensions[WeOSConfigExtension]; ok {
			dataBytes, err := json.Marshal(data)
			if err != nil {
				p.Logger.Errorf("error encountered marshalling config '%s'", err)
				return WeOSConfigResult{}, err
			}
			err = json.Unmarshal(dataBytes, &config)
			return WeOSConfigResult{
				Config: config,
			}, nil
		}
	}

	return WeOSConfigResult{}, nil
}
