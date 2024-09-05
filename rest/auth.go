package rest

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"net/http"
	"strings"
	"time"
)

// ValidationResult is the result of a security validation
type ValidationResult struct {
	Valid         bool
	Token         interface{}
	UserID        string
	Role          string
	AccountID     string
	ApplicationID string
}

//security interfaces

type Validator interface {
	Validate(ctxt echo.Context) (*ValidationResult, error)
	FromSchema(ctx context.Context, scheme *openapi3.SecurityScheme, httpClient *http.Client) (Validator, error)
}

type SecurityParams struct {
	fx.In
	Config     *openapi3.T
	HttpClient *http.Client
	GORMDB     *gorm.DB
}

type SecurityConfiguration struct {
	fx.Out
	SecuritySchemes map[string]Validator
	AuthEnforcer    *casbin.Enforcer
}

func NewSecurityConfiguration(p SecurityParams) (result SecurityConfiguration, err error) {
	result = SecurityConfiguration{
		SecuritySchemes: make(map[string]Validator),
	}
	for name, schema := range p.Config.Components.SecuritySchemes {
		if schema.Value != nil {
			switch schema.Value.Type {
			case "openIdConnect":
				ctxt := context.WithValue(context.Background(), oauth2.HTTPClient, p.HttpClient)
				result.SecuritySchemes[name], err = new(OpenIDConnect).FromSchema(ctxt, schema.Value, p.HttpClient)
			default:
				err = fmt.Errorf("unsupported security scheme '%s'", name)
				return result, err
			}
		}
	}

	//setup casbin enforcer
	adapter, err := gormadapter.NewAdapterByDB(p.GORMDB)
	if err != nil {
		return result, err
	}

	//default REST permission model
	text := `[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && keyMatch(r.obj, p.obj) && regexMatch(r.act, p.act)
`
	m, _ := casbinmodel.NewModelFromString(text)
	result.AuthEnforcer, err = casbin.NewEnforcer(m, adapter)
	return result, err
}

// OpenIDConnect authorizer for OpenID
type OpenIDConnect struct {
	connectURL       string
	skipExpiryCheck  bool
	clientID         string
	userIDClaim      string
	roleClaim        string
	accountClaim     string
	applicationClaim string
	httpClient       *http.Client
	KeySet           oidc.KeySet
}

func (o *OpenIDConnect) Validate(ctxt echo.Context) (result *ValidationResult, err error) {
	//get the Jwk url from open id connect url and validate url
	openIDConfig, err := GetOpenIDConfig(o.connectURL, o.httpClient)
	if err != nil {
		return result, err
	} else {
		if jwks_uri, ok := openIDConfig["jwks_uri"]; ok {
			//create key set and verifier
			if o.KeySet == nil {
				o.KeySet = oidc.NewRemoteKeySet(ctxt.Request().Context(), jwks_uri.(string))
			}
			keySet := o.KeySet
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
			token, err := tokenVerifier.Verify(context.Background(), tokenString)
			ctxt.Logger().Debugf("invalid token: %s", err)

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

			return &ValidationResult{
				Valid:         token != nil && err == nil,
				Token:         tokenString,
				UserID:        userID,
				Role:          role,
				AccountID:     accountID,
				ApplicationID: applicationID,
			}, err
		} else {
			return result, fmt.Errorf("expected jwks_url to be set")
		}
	}
}

func (o *OpenIDConnect) FromSchema(ctxt context.Context, scheme *openapi3.SecurityScheme, httpClient *http.Client) (Validator, error) {
	var err error
	o.httpClient = httpClient
	o.connectURL = scheme.OpenIdConnectUrl

	if tinterface, ok := scheme.Extensions[SkipExpiryCheckExtension]; ok {
		if expiryCheck, ok := tinterface.(json.RawMessage); ok {
			err = json.Unmarshal(expiryCheck, &o.skipExpiryCheck)
		}
	}

	if jwtMapRaw, ok := scheme.Extensions[JWTMapExtension]; ok {
		if user, ok := jwtMapRaw.(map[string]interface{})["user"]; ok {
			o.userIDClaim = user.(string)
		}
		if value, ok := jwtMapRaw.(map[string]interface{})["role"]; ok {
			o.roleClaim = value.(string)
		}
		if value, ok := jwtMapRaw.(map[string]interface{})["account"]; ok {
			o.accountClaim = value.(string)
		}
		if value, ok := jwtMapRaw.(map[string]interface{})["application"]; ok {
			o.applicationClaim = value.(string)
		}
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

func (o *OAuth2) Validate(ctxt echo.Context) (*ValidationResult, error) {
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
	return &ValidationResult{
		Valid: token.Valid,
	}, err
}

func (o *OAuth2) FromSchema(ctxt context.Context, scheme *openapi3.SecurityScheme, client *http.Client) (Validator, error) {
	var err error
	o.Flows = scheme.Flows
	return o, err
}
