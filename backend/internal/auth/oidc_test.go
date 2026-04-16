package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"sop-chat/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

func TestOIDCHelpersEndToEnd(t *testing.T) {
	t.Parallel()

	provider := newOIDCTestProvider(t)

	cfg := &config.OIDCConfig{
		IssuerURL:        provider.issuerURL,
		ClientID:         "client-id",
		ClientSecret:     "client-secret",
		Scopes:           []string{"openid", "profile", "email", "groups"},
		UsernameClaim:    "preferred_username",
		EmailClaim:       "email",
		DisplayNameClaim: "name",
		GroupsClaim:      "groups",
		DefaultRoles:     []string{"viewer"},
		RoleMappings: []config.OIDCRoleMapping{
			{External: "ops", Roles: []string{"admin", "ops"}},
		},
	}
	redirectURL := "https://app.example.com/api/auth/oidc/callback"
	state := "state-123"
	nonce := "nonce-123"
	provider.expectedNonce = nonce

	metadata, err := DiscoverOIDCProvider(context.Background(), provider.Client(), cfg.IssuerURL)
	if err != nil {
		t.Fatalf("DiscoverOIDCProvider returned error: %v", err)
	}

	authURL, err := BuildOIDCAuthorizeURL(cfg, metadata, redirectURL, state, nonce)
	if err != nil {
		t.Fatalf("BuildOIDCAuthorizeURL returned error: %v", err)
	}
	parsedURL, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("failed to parse authorize URL: %v", err)
	}
	if parsedURL.Query().Get("client_id") != cfg.ClientID || parsedURL.Query().Get("nonce") != nonce {
		t.Fatalf("unexpected authorize URL query: %s", parsedURL.RawQuery)
	}

	tokenResp, err := ExchangeOIDCCode(context.Background(), provider.Client(), cfg, metadata, "good-code", redirectURL)
	if err != nil {
		t.Fatalf("ExchangeOIDCCode returned error: %v", err)
	}

	identity, err := VerifyOIDCIDToken(context.Background(), provider.Client(), cfg, metadata, tokenResp.IDToken, nonce)
	if err != nil {
		t.Fatalf("VerifyOIDCIDToken returned error: %v", err)
	}
	if identity.User.Username != "demo.user" || identity.User.Email != "demo@example.com" {
		t.Fatalf("unexpected identity user: %+v", identity.User)
	}
	if identity.DisplayName != "Demo User" {
		t.Fatalf("expected display name to be populated, got %q", identity.DisplayName)
	}
	if len(identity.ExternalRefs) != 2 || identity.ExternalRefs[0] != "ops" || identity.ExternalRefs[1] != "sre" {
		t.Fatalf("unexpected external refs: %+v", identity.ExternalRefs)
	}
	if len(identity.User.Roles) != 2 || identity.User.Roles[0] != "admin" || identity.User.Roles[1] != "ops" {
		t.Fatalf("unexpected mapped roles: %+v", identity.User.Roles)
	}
}

func TestMapOIDCRolesFallbackToDefaultRoles(t *testing.T) {
	t.Parallel()

	cfg := &config.OIDCConfig{
		DefaultRoles: []string{"viewer", "viewer"},
		RoleMappings: []config.OIDCRoleMapping{
			{External: "ops", Roles: []string{"admin"}},
		},
	}

	roles := mapOIDCRoles(cfg, []string{"finance"})
	if len(roles) != 1 || roles[0] != "viewer" {
		t.Fatalf("expected default roles fallback, got %+v", roles)
	}
}

type oidcTestProvider struct {
	issuerURL      string
	key            *rsa.PrivateKey
	expectedNonce  string
	expectedCode   string
	expectedClient string
	expectedSecret string
}

type oidcTestRoundTripper struct {
	provider *oidcTestProvider
}

func newOIDCTestProvider(t *testing.T) *oidcTestProvider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}

	return &oidcTestProvider{
		issuerURL:      "https://issuer.example.com",
		key:            key,
		expectedCode:   "good-code",
		expectedClient: "client-id",
		expectedSecret: "client-secret",
	}
}

func (p *oidcTestProvider) Client() *http.Client {
	return &http.Client{
		Transport: &oidcTestRoundTripper{provider: p},
	}
}

func (p *oidcTestProvider) Close() {}

func (rt *oidcTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	provider := rt.provider
	switch req.URL.Path {
	case "/.well-known/openid-configuration":
		return jsonHTTPResponse(http.StatusOK, map[string]any{
			"issuer":                 provider.issuerURL,
			"authorization_endpoint": provider.issuerURL + "/authorize",
			"token_endpoint":         provider.issuerURL + "/token",
			"jwks_uri":               provider.issuerURL + "/jwks",
		}), nil
	case "/jwks":
		return jsonHTTPResponse(http.StatusOK, map[string]any{
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
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(payload))
		if err != nil {
			return nil, err
		}
		if values.Get("code") != provider.expectedCode {
			return textHTTPResponse(http.StatusBadRequest, "invalid code"), nil
		}
		if values.Get("client_id") != provider.expectedClient || values.Get("client_secret") != provider.expectedSecret {
			return textHTTPResponse(http.StatusBadRequest, "invalid client credentials"), nil
		}
		idToken, err := provider.signToken(provider.expectedNonce)
		if err != nil {
			return nil, err
		}
		return jsonHTTPResponse(http.StatusOK, map[string]any{
			"access_token": "access-token",
			"id_token":     idToken,
			"token_type":   "Bearer",
		}), nil
	default:
		return textHTTPResponse(http.StatusNotFound, "not found"), nil
	}
}

func (p *oidcTestProvider) signToken(nonce string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":                p.issuerURL,
		"aud":                "client-id",
		"sub":                "user-1",
		"preferred_username": "demo.user",
		"email":              "demo@example.com",
		"name":               "Demo User",
		"groups":             []string{"ops", "sre"},
		"nonce":              nonce,
		"iat":                now.Unix(),
		"nbf":                now.Add(-time.Minute).Unix(),
		"exp":                now.Add(time.Hour).Unix(),
	})
	token.Header["kid"] = "test-key"
	return token.SignedString(p.key)
}

func jsonHTTPResponse(status int, payload any) *http.Response {
	body, _ := json.Marshal(payload)
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

func textHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestDefaultOIDCScopesDeduplicatesAndBackfills(t *testing.T) {
	t.Parallel()

	scopes := DefaultOIDCScopes([]string{"openid", "profile", "openid", " email "})
	if strings.Join(scopes, ",") != "openid,profile,email" {
		t.Fatalf("unexpected scopes: %+v", scopes)
	}
}
