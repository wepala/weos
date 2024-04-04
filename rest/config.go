package rest

import (
	"encoding/json"
	"errors"
	"github.com/getkin/kin-openapi/openapi3"
	"go.uber.org/fx"
	"net/url"
	"os"
	"strings"
)

// Config loads the OpenAPI spec from the environment
func Config() (*openapi3.T, error) {
	spec := os.Getenv("WEOS_SPEC")
	if spec != "" {
		//check if the spec is a file or a url
		if strings.HasPrefix(spec, "http") {
			turl, err := url.Parse(spec)
			if err == nil {
				return openapi3.NewLoader().LoadFromURI(turl)
			}
		}

		//read the file
		content, err := os.ReadFile(spec)
		if err != nil {
			return nil, err
		}
		//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
		tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
		//replace environment variables in file
		tempFile = os.ExpandEnv(string(tempFile))
		tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
		content = []byte(tempFile)
		return openapi3.NewLoader().LoadFromData(content)

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
