package api

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"sop-chat/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestHandleOIDCLoginRedirectsToProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)

	provider := newAPIOIDCTestProvider(t)

	server := newOIDCAPITestServer(t, provider)
	router := gin.New()
	router.GET("/api/auth/oidc/login", server.handleOIDCLogin)

	req := httptest.NewRequest(http.MethodGet, "http://app.example.com/api/auth/oidc/login", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rec.Code, rec.Body.String())
	}

	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("failed to parse redirect location: %v", err)
	}
	if !strings.HasPrefix(location.String(), provider.issuerURL+"/authorize") {
		t.Fatalf("unexpected redirect location: %s", location.String())
	}
	if location.Query().Get("client_id") != "client-id" {
		t.Fatalf("unexpected client_id in redirect: %s", location.RawQuery)
	}
	state := location.Query().Get("state")
	if state == "" || location.Query().Get("nonce") == "" {
		t.Fatalf("expected state and nonce in redirect: %s", location.RawQuery)
	}
	if _, ok := server.oidcStates[state]; !ok {
		t.Fatalf("expected state %q to be stored", state)
	}
}

func TestHandleOIDCCallbackWritesSessionBootstrapPage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	provider := newAPIOIDCTestProvider(t)

	server := newOIDCAPITestServer(t, provider)
	router := gin.New()
	router.GET("/api/auth/oidc/login", server.handleOIDCLogin)
	router.GET("/api/auth/oidc/callback", server.handleOIDCCallback)

	loginReq := httptest.NewRequest(http.MethodGet, "http://app.example.com/api/auth/oidc/login", nil)
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusFound {
		t.Fatalf("expected login redirect, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	loginLocation, err := url.Parse(loginRec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("failed to parse login redirect: %v", err)
	}
	state := loginLocation.Query().Get("state")
	if state == "" {
		t.Fatalf("expected state in login redirect")
	}

	storedState, ok := server.oidcStates[state]
	if !ok {
		t.Fatalf("expected stored oidc state for %q", state)
	}
	provider.expectedNonce = storedState.Nonce

	callbackReq := httptest.NewRequest(http.MethodGet, "http://app.example.com/api/auth/oidc/callback?code=good-code&state="+url.QueryEscape(state), nil)
	callbackRec := httptest.NewRecorder()
	router.ServeHTTP(callbackRec, callbackReq)

	if callbackRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", callbackRec.Code, callbackRec.Body.String())
	}
	body := callbackRec.Body.String()
	if !strings.Contains(body, "localStorage.setItem('auth_token'") || !strings.Contains(body, "localStorage.setItem('auth_user'") {
		t.Fatalf("expected session bootstrap page, got %s", body)
	}
	if !strings.Contains(body, "/#/") {
		t.Fatalf("expected redirect to hash router root, got %s", body)
	}
	if _, stillExists := server.oidcStates[state]; stillExists {
		t.Fatalf("expected state to be consumed after callback")
	}
}

func newOIDCAPITestServer(t *testing.T, provider *apiOIDCTestProvider) *Server {
	t.Helper()

	server := &Server{
		globalConfig: &config.Config{
			Auth: config.AuthConfig{
				Methods: []string{"oidc"},
				JWT: config.JWTConfig{
					SecretKey: "test-secret",
					ExpiresIn: "1h",
				},
				OIDC: &config.OIDCConfig{
					IssuerURL:        provider.issuerURL,
					ClientID:         "client-id",
					ClientSecret:     "client-secret",
					UsernameClaim:    "preferred_username",
					EmailClaim:       "email",
					DisplayNameClaim: "name",
					GroupsClaim:      "groups",
					DefaultRoles:     []string{"viewer"},
					RoleMappings: []config.OIDCRoleMapping{
						{External: "ops", Roles: []string{"admin", "ops"}},
					},
				},
			},
		},
		oidcStates:     make(map[string]oidcAuthState),
		oidcHTTPClient: provider.Client(),
	}
	if err := server.initAuth(); err != nil {
		t.Fatalf("initAuth returned error: %v", err)
	}
	return server
}

type apiOIDCTestProvider struct {
	issuerURL     string
	key           *rsa.PrivateKey
	expectedNonce string
}

type apiOIDCTestRoundTripper struct {
	provider *apiOIDCTestProvider
}

func newAPIOIDCTestProvider(t *testing.T) *apiOIDCTestProvider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}

	return &apiOIDCTestProvider{
		issuerURL: "https://login.example.com",
		key:       key,
	}
}

func (p *apiOIDCTestProvider) Client() *http.Client {
	return &http.Client{
		Transport: &apiOIDCTestRoundTripper{provider: p},
	}
}

func (rt *apiOIDCTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	provider := rt.provider
	switch req.URL.Path {
	case "/.well-known/openid-configuration":
		return newJSONHTTPResponse(http.StatusOK, map[string]any{
			"issuer":                 provider.issuerURL,
			"authorization_endpoint": provider.issuerURL + "/authorize",
			"token_endpoint":         provider.issuerURL + "/token",
			"jwks_uri":               provider.issuerURL + "/jwks",
		}), nil
	case "/jwks":
		return newJSONHTTPResponse(http.StatusOK, map[string]any{
			"keys": []map[string]any{
				{
					"kid": "test-key",
					"kty": "RSA",
					"alg": "RS256",
					"use": "sig",
					"n":   base64.RawURLEncoding.EncodeToString(provider.key.PublicKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(provider.key.PublicKey.E)).Bytes()),
				},
			},
		}), nil
	case "/token":
		idToken, err := provider.signToken()
		if err != nil {
			return nil, err
		}
		return newJSONHTTPResponse(http.StatusOK, map[string]any{
			"access_token": "access-token",
			"id_token":     idToken,
			"token_type":   "Bearer",
		}), nil
	default:
		return newTextHTTPResponse(http.StatusNotFound, "not found"), nil
	}
}

func (p *apiOIDCTestProvider) signToken() (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":                p.issuerURL,
		"aud":                "client-id",
		"sub":                "user-1",
		"preferred_username": "demo.user",
		"email":              "demo@example.com",
		"name":               "Demo User",
		"groups":             []string{"ops", "sre"},
		"nonce":              p.expectedNonce,
		"iat":                now.Unix(),
		"nbf":                now.Add(-time.Minute).Unix(),
		"exp":                now.Add(time.Hour).Unix(),
	})
	token.Header["kid"] = "test-key"
	return token.SignedString(p.key)
}

func newJSONHTTPResponse(status int, payload any) *http.Response {
	body, _ := json.Marshal(payload)
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

func newTextHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
