package rest

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"golang.org/x/net/context"
	"strings"
	"time"
)

// OpenIDConnect authorizer for OpenID
type OpenIDConnect struct {
	connectURL       string
	skipExpiryCheck  bool
	clientID         string
	userIDClaim      string
	roleClaim        string
	accountClaim     string
	applicationClaim string
}

func (o OpenIDConnect) Validate(ctxt echo.Context) (bool, interface{}, string, string, string, string, error) {
	//get the Jwk url from open id connect url and validate url
	openIDConfig, err := GetOpenIDConfig(o.connectURL)
	if err != nil {
		return false, nil, "", "", "", "", err
	} else {
		if jwks_uri, ok := openIDConfig["jwks_uri"]; ok {
			//create key set and verifier
			keySet := oidc.NewRemoteKeySet(context.Background(), jwks_uri.(string))
			var algs []string
			if talgs, ok := openIDConfig["id_token_signing_alg_values_supported"]; ok {
				for _, alg := range talgs.([]interface{}) {
					algs = append(algs, alg.(string))
				}

			}
			if talgs, ok := openIDConfig["request_object_signing_alg_values_supported"]; ok {
				for _, alg := range talgs.([]interface{}) {
					algs = append(algs, alg.(string))
				}
			}
			tokenVerifier := oidc.NewVerifier(o.connectURL, keySet, &oidc.Config{
				ClientID:             o.clientID,
				SupportedSigningAlgs: algs,
				SkipClientIDCheck:    o.clientID == "",
				SkipExpiryCheck:      o.skipExpiryCheck,
				SkipIssuerCheck:      true,
				Now:                  time.Now,
			})
			authorizationHeader := ctxt.Request().Header.Get("Authorization")
			tokenString := strings.Replace(authorizationHeader, "Bearer ", "", -1)
			token, err := tokenVerifier.Verify(ctxt.Request().Context(), tokenString)
			err = fmt.Errorf("invalid token '%s': %s. Headers '%s'", tokenString, err, ctxt.Request().Header)

			var userID string
			var role string
			var accountID string
			var applicationID string

			if token != nil {
				tclaims := make(map[string]interface{})
				tclaims[o.userIDClaim] = token.Subject
				tclaims[o.roleClaim] = ""
				if o.accountClaim != "" {
					tclaims[o.accountClaim] = ""
				}
				if o.applicationClaim != "" {
					tclaims[o.applicationClaim] = ""
				}
				err = token.Claims(&tclaims)
				if err == nil {
					role = tclaims[o.roleClaim].(string)
					userID = tclaims[o.userIDClaim].(string)
					if o.accountClaim != "" {
						accountID = tclaims[o.accountClaim].(string)
					}
					if o.applicationClaim != "" {
						applicationID = tclaims[o.applicationClaim].(string)
					}
				}
			}

			return token != nil && err == nil, token, userID, role, accountID, applicationID, err
		} else {
			return false, nil, "", "", "", "", fmt.Errorf("expected jwks_url to be set")
		}
	}
}

func (o OpenIDConnect) FromSchema(scheme *openapi3.SecurityScheme) (Validator, error) {
	var err error
	if tinterface, ok := scheme.Extensions[OpenIDConnectUrlExtension]; ok {
		if rawURL, ok := tinterface.(json.RawMessage); ok {
			err = json.Unmarshal(rawURL, &o.connectURL)
		}
	}

	if tinterface, ok := scheme.Extensions[SkipExpiryCheckExtension]; ok {
		if expiryCheck, ok := tinterface.(json.RawMessage); ok {
			err = json.Unmarshal(expiryCheck, &o.skipExpiryCheck)
		}
	}

	if jwtMapRaw, ok := scheme.Extensions[JWTMapExtension]; ok {
		var jwtMap struct {
			User        string `json:"user"`
			Role        string `json:"role"`
			Account     string `json:"account"`
			Application string `json:"application"`
		}
		err = json.Unmarshal(jwtMapRaw.(json.RawMessage), &jwtMap)
		if err != nil {
			return o, err
		}
		o.userIDClaim = jwtMap.User
		o.roleClaim = jwtMap.Role
		o.accountClaim = jwtMap.Account
		o.applicationClaim = jwtMap.Application
	} else {
		o.userIDClaim = "sub"
	}
	return o, err
}

type OAuth2 struct {
	connectURL   string
	Flows        *openapi3.OAuthFlows
	clientSecret string
}

func (o OAuth2) Validate(ctxt echo.Context) (bool, interface{}, string, string, string, string, error) {
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
	return token.Valid, nil, "", "", "", "", err
}

func (o OAuth2) FromSchema(scheme *openapi3.SecurityScheme) (Validator, error) {
	var err error
	o.Flows = scheme.Flows
	return o, err
}
