package auth

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sop-chat/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

var (
	defaultOIDCScopes = []string{"openid", "profile", "email"}
)

// OIDCProviderMetadata 是 discovery 文档中当前链路需要的字段。
type OIDCProviderMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	EndSessionEndpoint    string `json:"end_session_endpoint,omitempty"`
}

// OIDCIdentity 表示从 id_token 中解析出的用户身份。
type OIDCIdentity struct {
	User         *User
	DisplayName  string
	ExternalRefs []string
	Claims       map[string]any
}

type oidcTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

type oidcJWKSet struct {
	Keys []oidcJWK `json:"keys"`
}

type oidcJWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// DefaultOIDCScopes 返回配置缺失时使用的默认 scopes。
func DefaultOIDCScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return append([]string(nil), defaultOIDCScopes...)
	}
	result := make([]string, 0, len(scopes))
	seen := map[string]struct{}{}
	for _, scope := range scopes {
		value := strings.TrimSpace(scope)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return append([]string(nil), defaultOIDCScopes...)
	}
	return result
}

// DiscoverOIDCProvider 读取 OIDC discovery 文档。
func DiscoverOIDCProvider(ctx context.Context, client *http.Client, issuerURL string) (*OIDCProviderMetadata, error) {
	if strings.TrimSpace(issuerURL) == "" {
		return nil, fmt.Errorf("oidc issuerURL 未配置")
	}
	discoveryURL := strings.TrimRight(strings.TrimSpace(issuerURL), "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 discovery 请求失败: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取 discovery 文档失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("discovery 文档返回异常状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var metadata OIDCProviderMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("解析 discovery 文档失败: %w", err)
	}
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" || metadata.JWKSURI == "" {
		return nil, fmt.Errorf("discovery 文档缺少必要字段")
	}
	if metadata.Issuer == "" {
		metadata.Issuer = strings.TrimRight(strings.TrimSpace(issuerURL), "/")
	}
	return &metadata, nil
}

// BuildOIDCAuthorizeURL 构造授权跳转地址。
func BuildOIDCAuthorizeURL(cfg *config.OIDCConfig, metadata *OIDCProviderMetadata, redirectURL, state, nonce string) (string, error) {
	if cfg == nil || metadata == nil {
		return "", fmt.Errorf("oidc 配置不完整")
	}
	authURL, err := url.Parse(metadata.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("无效的 authorization endpoint: %w", err)
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", redirectURL)
	query.Set("scope", strings.Join(DefaultOIDCScopes(cfg.Scopes), " "))
	query.Set("state", state)
	query.Set("nonce", nonce)
	authURL.RawQuery = query.Encode()
	return authURL.String(), nil
}

// BuildOIDCLogoutURL 构造 Provider 退出地址。
func BuildOIDCLogoutURL(endSessionEndpoint, postLogoutRedirectURL, clientID string) (string, error) {
	if strings.TrimSpace(endSessionEndpoint) == "" {
		return "", fmt.Errorf("oidc 退出端点未配置")
	}
	logoutURL, err := url.Parse(strings.TrimSpace(endSessionEndpoint))
	if err != nil {
		return "", fmt.Errorf("无效的 logout endpoint: %w", err)
	}

	query := logoutURL.Query()
	if strings.TrimSpace(postLogoutRedirectURL) != "" {
		query.Set("post_logout_redirect_uri", strings.TrimSpace(postLogoutRedirectURL))
	}
	if strings.TrimSpace(clientID) != "" {
		query.Set("client_id", strings.TrimSpace(clientID))
	}
	logoutURL.RawQuery = query.Encode()
	return logoutURL.String(), nil
}

// ExchangeOIDCCode 使用授权码换取 id_token。
func ExchangeOIDCCode(ctx context.Context, client *http.Client, cfg *config.OIDCConfig, metadata *OIDCProviderMetadata, code, redirectURL string) (*oidcTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURL)
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, metadata.TokenEndpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建 token 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("交换授权码失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token endpoint 返回异常状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp oidcTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("解析 token 响应失败: %w", err)
	}
	if strings.TrimSpace(tokenResp.IDToken) == "" {
		return nil, fmt.Errorf("token 响应中缺少 id_token")
	}
	return &tokenResp, nil
}

// VerifyOIDCIDToken 校验 id_token 并抽取用户身份。
func VerifyOIDCIDToken(ctx context.Context, client *http.Client, cfg *config.OIDCConfig, metadata *OIDCProviderMetadata, rawIDToken, expectedNonce string) (*OIDCIdentity, error) {
	jwks, err := fetchOIDCJWKSet(ctx, client, metadata.JWKSURI)
	if err != nil {
		return nil, err
	}

	parsedToken, err := jwt.Parse(rawIDToken, func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		alg, _ := token.Header["alg"].(string)
		key, err := jwks.findRSAPublicKey(kid, alg)
		if err != nil {
			return nil, err
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}), jwt.WithIssuer(metadata.Issuer), jwt.WithAudience(cfg.ClientID), jwt.WithLeeway(time.Minute))
	if err != nil {
		return nil, fmt.Errorf("校验 id_token 失败: %w", err)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok || !parsedToken.Valid {
		return nil, fmt.Errorf("无效的 id_token claims")
	}
	if expectedNonce != "" && getStringClaim(claims, "nonce") != expectedNonce {
		return nil, fmt.Errorf("id_token nonce 校验失败")
	}

	username := resolveOIDCUsername(cfg, claims)
	if username == "" {
		return nil, fmt.Errorf("未能从 id_token 中解析用户名")
	}
	email := resolveOIDCEmail(cfg, claims)
	displayName := resolveOIDCDisplayName(cfg, claims)
	externalRefs := resolveOIDCExternalRefs(cfg, claims)
	roles := mapOIDCRoles(cfg, externalRefs)

	return &OIDCIdentity{
		User: &User{
			Username: username,
			Email:    email,
			Roles:    roles,
		},
		DisplayName:  displayName,
		ExternalRefs: externalRefs,
		Claims:       claims,
	}, nil
}

func resolveOIDCUsername(cfg *config.OIDCConfig, claims map[string]any) string {
	if cfg != nil {
		if v := getStringClaim(claims, cfg.UsernameClaim); v != "" {
			return v
		}
	}
	for _, key := range []string{"preferred_username", "username", "login", "email", "sub"} {
		if v := getStringClaim(claims, key); v != "" {
			return v
		}
	}
	return ""
}

func resolveOIDCEmail(cfg *config.OIDCConfig, claims map[string]any) string {
	if cfg != nil {
		if v := getStringClaim(claims, cfg.EmailClaim); v != "" {
			return v
		}
	}
	return getStringClaim(claims, "email")
}

func resolveOIDCDisplayName(cfg *config.OIDCConfig, claims map[string]any) string {
	if cfg != nil {
		if v := getStringClaim(claims, cfg.DisplayNameClaim); v != "" {
			return v
		}
	}
	for _, key := range []string{"name", "display_name", "nickname"} {
		if v := getStringClaim(claims, key); v != "" {
			return v
		}
	}
	return ""
}

func resolveOIDCExternalRefs(cfg *config.OIDCConfig, claims map[string]any) []string {
	values := []string{}
	if cfg != nil && strings.TrimSpace(cfg.GroupsClaim) != "" {
		values = append(values, getStringSliceClaim(claims, cfg.GroupsClaim)...)
	} else {
		values = append(values, getStringSliceClaim(claims, "groups")...)
		values = append(values, getStringSliceClaim(claims, "roles")...)
	}
	return uniqueStrings(values)
}

func mapOIDCRoles(cfg *config.OIDCConfig, externalRefs []string) []string {
	if cfg == nil {
		return nil
	}
	mapped := []string{}
	normalizedExternal := uniqueStrings(externalRefs)
	for _, external := range normalizedExternal {
		for _, mapping := range cfg.RoleMappings {
			if strings.TrimSpace(mapping.External) != external {
				continue
			}
			mapped = append(mapped, mapping.Roles...)
		}
	}
	mapped = uniqueStrings(mapped)
	if len(mapped) == 0 {
		mapped = uniqueStrings(cfg.DefaultRoles)
	}
	return mapped
}

func getStringClaim(claims map[string]any, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	value, exists := claims[key]
	if !exists || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func getStringSliceClaim(claims map[string]any, key string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	value, exists := claims[key]
	if !exists || value == nil {
		return nil
	}
	switch v := value.(type) {
	case []string:
		return uniqueStrings(v)
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, strings.TrimSpace(fmt.Sprintf("%v", item)))
		}
		return uniqueStrings(items)
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		if strings.Contains(v, ",") {
			return uniqueStrings(strings.Split(v, ","))
		}
		return []string{strings.TrimSpace(v)}
	default:
		return nil
	}
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func fetchOIDCJWKSet(ctx context.Context, client *http.Client, jwksURI string) (*oidcJWKSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 jwks 请求失败: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取 jwks 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("jwks 返回异常状态 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var jwks oidcJWKSet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("解析 jwks 失败: %w", err)
	}
	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("jwks 中没有可用密钥")
	}
	return &jwks, nil
}

func (s *oidcJWKSet) findRSAPublicKey(kid, alg string) (*rsa.PublicKey, error) {
	for _, key := range s.Keys {
		if kid != "" && key.Kid != "" && key.Kid != kid {
			continue
		}
		if key.Kty != "RSA" {
			continue
		}
		if alg != "" && key.Alg != "" && key.Alg != alg {
			continue
		}
		return key.toRSAPublicKey()
	}
	if kid == "" && len(s.Keys) == 1 {
		return s.Keys[0].toRSAPublicKey()
	}
	return nil, fmt.Errorf("未找到匹配的 jwk 公钥")
}

func (k *oidcJWK) toRSAPublicKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("解析 jwk.n 失败: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("解析 jwk.e 失败: %w", err)
	}
	eInt := 0
	for _, b := range eBytes {
		eInt = eInt<<8 + int(b)
	}
	if eInt == 0 {
		return nil, fmt.Errorf("无效的 jwk 指数")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: eInt,
	}, nil
}
