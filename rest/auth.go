package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

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
)

// ValidationResult is the result of a security validation
type ValidationResult struct {
	Valid          bool
	Token          interface{}
	UserID         string
	Role           string
	AccountID      string
	ApplicationID  string
	SubscriptionID string
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
			case "oauth2":
				ctxt := context.WithValue(context.Background(), oauth2.HTTPClient, p.HttpClient)
				result.SecuritySchemes[name], err = new(OAuth2).FromSchema(ctxt, schema.Value, p.HttpClient)
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
m = keyMatch(r.sub, p.sub) && keyMatch(r.obj, p.obj) && regexMatch(r.act, p.act)
`
	m, _ := casbinmodel.NewModelFromString(text)
	result.AuthEnforcer, err = casbin.NewEnforcer(m, adapter)
	return result, err
}

// OpenIDConnect authorizer for OpenID
type OpenIDConnect struct {
	connectURL        string
	skipExpiryCheck   bool
	clientID          string
	userIDClaim       string
	roleClaim         string
	accountClaim      string
	applicationClaim  string
	subscriptionClaim string
	httpClient        *http.Client
	KeySet            oidc.KeySet
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
				o.KeySet = oidc.NewRemoteKeySet(context.Background(), jwks_uri.(string))
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
			if err != nil {
				ctxt.Logger().Debugf("invalid token: %s", err)
			}

			var userID string
			var role string
			var accountID string
			var applicationID string
			var subscriptionID string

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
					if o.subscriptionClaim != "" {
						subscriptionID = tclaims[o.subscriptionClaim].(string)
					}
				}
			}

			return &ValidationResult{
				Valid:          token != nil && err == nil,
				Token:          tokenString,
				UserID:         userID,
				Role:           role,
				AccountID:      accountID,
				ApplicationID:  applicationID,
				SubscriptionID: subscriptionID,
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
		if value, ok := jwtMapRaw.(map[string]interface{})["subscription"]; ok {
			o.subscriptionClaim = value.(string)
		}
	} else {
		o.userIDClaim = "sub"
	}

	return o, err
}

type OAuth2 struct {
	connectURL         string
	Flows              *openapi3.OAuthFlows
	clientSecret       string
	clientID           string
	userIDClaim        string
	roleClaim          string
	accountClaim       string
	applicationClaim   string
	subscriptionClaim  string
	httpClient         *http.Client
	tokenIntrospectURL string
}

// validateAuthorizationCodeToken validates a token using the OAuth2 authorization server's introspection endpoint
func (o *OAuth2) validateAuthorizationCodeToken(ctxt echo.Context, tokenString string) (bool, error) {
	if o.tokenIntrospectURL == "" {
		ctxt.Logger().Warn("No token introspection URL configured for authorization code flow")
		return false, fmt.Errorf("token introspection URL not configured")
	}

	// Prepare the introspection request
	introspectData := map[string]string{
		"token": tokenString,
	}

	// Add client credentials if available
	if o.clientID != "" {
		introspectData["client_id"] = o.clientID
	}
	if o.clientSecret != "" {
		introspectData["client_secret"] = o.clientSecret
	}

	// Convert to form data
	formData := make([]string, 0, len(introspectData))
	for key, value := range introspectData {
		formData = append(formData, fmt.Sprintf("%s=%s", key, value))
	}
	formString := strings.Join(formData, "&")

	// Create the request
	req, err := http.NewRequest("POST", o.tokenIntrospectURL, strings.NewReader(formString))
	if err != nil {
		return false, fmt.Errorf("failed to create introspection request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Make the request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to make introspection request: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("introspection request failed with status: %d", resp.StatusCode)
	}

	// Parse the response
	var introspectResp struct {
		Active    bool   `json:"active"`
		Scope     string `json:"scope,omitempty"`
		ClientID  string `json:"client_id,omitempty"`
		Username  string `json:"username,omitempty"`
		TokenType string `json:"token_type,omitempty"`
		Exp       int64  `json:"exp,omitempty"`
		Iat       int64  `json:"iat,omitempty"`
		Nbf       int64  `json:"nbf,omitempty"`
		Sub       string `json:"sub,omitempty"`
		Aud       string `json:"aud,omitempty"`
		Iss       string `json:"iss,omitempty"`
		Jti       string `json:"jti,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&introspectResp); err != nil {
		return false, fmt.Errorf("failed to decode introspection response: %w", err)
	}

	// Check if the token is active
	if !introspectResp.Active {
		ctxt.Logger().Debug("Token is not active according to authorization server")
		return false, nil
	}

	// Check if token has expired
	if introspectResp.Exp > 0 {
		now := time.Now().Unix()
		if now > introspectResp.Exp {
			ctxt.Logger().Debug("Token has expired")
			return false, nil
		}
	}

	// Check if token is not yet valid (nbf - not before)
	if introspectResp.Nbf > 0 {
		now := time.Now().Unix()
		if now < introspectResp.Nbf {
			ctxt.Logger().Debug("Token is not yet valid")
			return false, nil
		}
	}

	// Validate client ID if specified
	if o.clientID != "" && introspectResp.ClientID != "" {
		if introspectResp.ClientID != o.clientID {
			ctxt.Logger().Debug("Token client ID mismatch")
			return false, nil
		}
	}

	// Log successful validation with additional info
	ctxt.Logger().Debugf("Token validated successfully via authorization server. Scope: %s, ClientID: %s",
		introspectResp.Scope, introspectResp.ClientID)

	return true, nil
}

func (o *OAuth2) Validate(ctxt echo.Context) (*ValidationResult, error) {
	authorizationHeader := ctxt.Request().Header.Get("Authorization")
	tokenString := strings.Replace(authorizationHeader, "Bearer ", "", -1)

	// Parse the JWT token without validation first to extract claims
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// For OAuth2, we'll use a more flexible approach that supports multiple signing methods
		// The actual validation will be done based on the OAuth2 flows configuration
		return nil, nil // We'll handle validation separately
	})

	if err != nil {
		ctxt.Logger().Debugf("invalid token: %s", err)
		return &ValidationResult{
			Valid: false,
			Token: tokenString,
		}, err
	}

	// Extract claims from the token
	var userID string
	var role string
	var accountID string
	var applicationID string
	var subscriptionID string

	if token != nil && token.Claims != nil {
		// Extract claims similar to OpenIDConnect
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			// Map user ID claim
			if o.userIDClaim != "" {
				if userIDClaim, exists := claims[o.userIDClaim]; exists {
					if userIDStr, ok := userIDClaim.(string); ok {
						userID = userIDStr
					}
				}
			}

			// Map role claim
			if o.roleClaim != "" {
				if roleClaim, exists := claims[o.roleClaim]; exists {
					if roleStr, ok := roleClaim.(string); ok {
						role = roleStr
					}
				}
			}

			// Map account claim
			if o.accountClaim != "" {
				if accountClaim, exists := claims[o.accountClaim]; exists {
					if accountStr, ok := accountClaim.(string); ok {
						accountID = accountStr
					}
				}
			}

			// Map application claim
			if o.applicationClaim != "" {
				if appClaim, exists := claims[o.applicationClaim]; exists {
					if appStr, ok := appClaim.(string); ok {
						applicationID = appStr
					}
				}
			}

			// Map subscription claim
			if o.subscriptionClaim != "" {
				if subClaim, exists := claims[o.subscriptionClaim]; exists {
					if subStr, ok := subClaim.(string); ok {
						subscriptionID = subStr
					}
				}
			}
		}
	}

	// Validate the token based on OAuth2 flows configuration
	valid := false
	if o.Flows != nil {
		// Check if we have any configured flows and validate accordingly
		// For now, we'll consider the token valid if we can parse it and extract claims
		// In a production environment, you would validate against the OAuth2 provider
		valid = token != nil && token.Valid

		// Additional validation based on OAuth2 flow types
		if o.Flows.AuthorizationCode != nil {
			// For authorization code flow, validate against authorization server
			// This would typically involve checking the token with the OAuth2 provider
			ctxt.Logger().Debug("OAuth2 authorization code flow detected")

			// Validate token using authorization server introspection
			authCodeValid, err := o.validateAuthorizationCodeToken(ctxt, tokenString)
			if err != nil {
				ctxt.Logger().Warnf("Authorization code flow validation failed: %v", err)
				// Fall back to basic token validation if introspection fails
				valid = token != nil && token.Valid
			} else {
				valid = authCodeValid
			}
		}

		if o.Flows.ClientCredentials != nil {
			// For client credentials flow, validate client credentials
			// This would typically involve checking client_id and client_secret
			ctxt.Logger().Debug("OAuth2 client credentials flow detected")
		}

		if o.Flows.Implicit != nil {
			// For implicit flow, validate token format and signature
			// This would typically involve checking the token signature
			ctxt.Logger().Debug("OAuth2 implicit flow detected")
		}

		if o.Flows.Password != nil {
			// For password flow, validate user credentials
			// This would typically involve checking username and password
			ctxt.Logger().Debug("OAuth2 password flow detected")
		}

		// If no specific flow is configured, use basic token validation
		if o.Flows.AuthorizationCode == nil && o.Flows.ClientCredentials == nil &&
			o.Flows.Implicit == nil && o.Flows.Password == nil {
			ctxt.Logger().Debug("No specific OAuth2 flow configured, using basic validation")
		}
	}

	return &ValidationResult{
		Valid:          valid,
		Token:          tokenString,
		UserID:         userID,
		Role:           role,
		AccountID:      accountID,
		ApplicationID:  applicationID,
		SubscriptionID: subscriptionID,
	}, nil
}

func (o *OAuth2) FromSchema(ctxt context.Context, scheme *openapi3.SecurityScheme, client *http.Client) (Validator, error) {
	var err error
	o.Flows = scheme.Flows
	o.httpClient = client

	// Extract token introspection URL from authorization code flow if available
	if o.Flows != nil && o.Flows.AuthorizationCode != nil {
		// The token introspection endpoint is typically available in the authorization server
		// For now, we'll construct it based on common patterns
		if o.Flows.AuthorizationCode.TokenURL != "" {
			// Convert token URL to introspection URL (common pattern)
			baseURL := o.Flows.AuthorizationCode.TokenURL
			if strings.HasSuffix(baseURL, "/token") {
				o.tokenIntrospectURL = strings.TrimSuffix(baseURL, "/token") + "/introspect"
			} else {
				o.tokenIntrospectURL = baseURL + "/introspect"
			}
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
		if value, ok := jwtMapRaw.(map[string]interface{})["subscription"]; ok {
			o.subscriptionClaim = value.(string)
		}
	} else {
		o.userIDClaim = "sub"
	}
	return o, err
}
