package rest

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"strings"
)

//OpenIDConnect authorizer for OpenID
type OpenIDConnect struct {
	connectURL string
}

func (o OpenIDConnect) Validate(ctxt echo.Context) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (o OpenIDConnect) FromSchema(scheme *openapi3.SecurityScheme) (Validator, error) {
	var err error
	if tinterface, ok := scheme.Extensions[OpenIDConnectUrlExtension]; ok {
		if rawURL, ok := tinterface.(json.RawMessage); ok {
			err = json.Unmarshal(rawURL, &o.connectURL)
		}
	}
	return o, err
}

type OAuth2 struct {
	connectURL   string
	Flows        *openapi3.OAuthFlows
	clientSecret string
}

func (o OAuth2) Validate(ctxt echo.Context) (bool, error) {
	authorizationHeader := ctxt.Request().Header.Get("Authorization")
	tokenString := strings.Replace(authorizationHeader, "Bearer ", "", -1)
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		//TODO figure out good way to load certificate here
		cert := ``

		block, _ := pem.Decode([]byte(cert))
		if block == nil {
			return nil, fmt.Errorf("unable to decode cert")
		}

		pub, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key '%s'", err)
		}

		return pub.PublicKey, nil
	})
	return token.Valid, err
}

func (o OAuth2) FromSchema(scheme *openapi3.SecurityScheme) (Validator, error) {
	var err error
	o.Flows = scheme.Flows
	return o, err
}
