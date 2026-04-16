package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sop-chat/internal/auth"
	"sop-chat/internal/config"

	"github.com/gin-gonic/gin"
)

const oidcStateTTL = 10 * time.Minute

type oidcAuthState struct {
	Nonce     string
	ExpiresAt time.Time
}

func (s *Server) hasAuthMode(mode auth.AuthMode) bool {
	for _, current := range s.authModes {
		if current == mode {
			return true
		}
	}
	return false
}

func (s *Server) supportsPasswordLogin() bool {
	for _, current := range s.authModes {
		if current == auth.AuthModeBuiltin || current == auth.AuthModeLDAP {
			return true
		}
	}
	return false
}

func (s *Server) currentOIDCConfig() *config.OIDCConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.globalConfig == nil {
		return nil
	}
	return s.globalConfig.Auth.OIDC
}

func isOIDCConfigUsable(cfg *config.OIDCConfig) bool {
	if cfg == nil {
		return false
	}
	return strings.TrimSpace(cfg.IssuerURL) != "" &&
		strings.TrimSpace(cfg.ClientID) != "" &&
		strings.TrimSpace(cfg.ClientSecret) != ""
}

func authModesToStrings(modes []auth.AuthMode) []string {
	result := make([]string, 0, len(modes))
	for _, mode := range modes {
		result = append(result, string(mode))
	}
	return result
}

func newOIDCRandomValue() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 OIDC 随机值失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Server) storeOIDCState(state, nonce string) {
	s.oidcStateMu.Lock()
	defer s.oidcStateMu.Unlock()

	now := time.Now()
	for key, item := range s.oidcStates {
		if !item.ExpiresAt.After(now) {
			delete(s.oidcStates, key)
		}
	}
	s.oidcStates[state] = oidcAuthState{
		Nonce:     nonce,
		ExpiresAt: now.Add(oidcStateTTL),
	}
}

func (s *Server) consumeOIDCState(state string) (oidcAuthState, bool) {
	s.oidcStateMu.Lock()
	defer s.oidcStateMu.Unlock()

	now := time.Now()
	for key, item := range s.oidcStates {
		if !item.ExpiresAt.After(now) {
			delete(s.oidcStates, key)
		}
	}

	item, exists := s.oidcStates[state]
	if !exists {
		return oidcAuthState{}, false
	}
	delete(s.oidcStates, state)
	if !item.ExpiresAt.After(now) {
		return oidcAuthState{}, false
	}
	return item, true
}

func defaultOIDCHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func (s *Server) oidcClient() *http.Client {
	if s.oidcHTTPClient != nil {
		return s.oidcHTTPClient
	}
	return defaultOIDCHTTPClient()
}

func requestScheme(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if forwarded := strings.TrimSpace(r.Header.Get("Forwarded")); forwarded != "" {
		parts := strings.Split(forwarded, ";")
		for _, part := range parts {
			keyValue := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(keyValue) != 2 {
				continue
			}
			if strings.EqualFold(keyValue[0], "proto") {
				return strings.Trim(strings.TrimSpace(keyValue[1]), `"`)
			}
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func requestHost(r *http.Request) string {
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		return strings.TrimSpace(strings.Split(forwardedHost, ",")[0])
	}
	return r.Host
}

func requestBaseURL(r *http.Request) string {
	return requestScheme(r) + "://" + requestHost(r)
}

func (s *Server) effectiveOIDCRedirectURL(c *gin.Context, cfg *config.OIDCConfig) string {
	if cfg != nil && strings.TrimSpace(cfg.RedirectURL) != "" {
		return strings.TrimSpace(cfg.RedirectURL)
	}
	return requestBaseURL(c.Request) + "/api/auth/oidc/callback"
}

func frontendHashURL(c *gin.Context, hashPath string, query url.Values) string {
	fragment := "/"
	if strings.TrimSpace(hashPath) != "" {
		fragment = strings.TrimSpace(hashPath)
		if !strings.HasPrefix(fragment, "/") {
			fragment = "/" + fragment
		}
	}
	if len(query) > 0 {
		fragment += "?" + query.Encode()
	}
	return requestBaseURL(c.Request) + "/#" + fragment
}

func (s *Server) redirectOIDCError(c *gin.Context, message string) {
	query := url.Values{}
	query.Set("error", message)
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, frontendHashURL(c, "/login", query))
}

func (s *Server) handleOIDCLogin(c *gin.Context) {
	if !s.hasAuthMode(auth.AuthModeOIDC) {
		s.redirectOIDCError(c, "当前未启用 OIDC / IDaaS 登录")
		return
	}

	oidcCfg := s.currentOIDCConfig()
	if !isOIDCConfigUsable(oidcCfg) {
		s.redirectOIDCError(c, "IDaaS 配置未完成，请检查 issuerURL、clientId 和 clientSecret")
		return
	}

	metadata, err := auth.DiscoverOIDCProvider(c.Request.Context(), s.oidcClient(), oidcCfg.IssuerURL)
	if err != nil {
		s.redirectOIDCError(c, "读取 IDaaS 配置失败: "+err.Error())
		return
	}

	state, err := newOIDCRandomValue()
	if err != nil {
		s.redirectOIDCError(c, err.Error())
		return
	}
	nonce, err := newOIDCRandomValue()
	if err != nil {
		s.redirectOIDCError(c, err.Error())
		return
	}
	redirectURL := s.effectiveOIDCRedirectURL(c, oidcCfg)
	authorizeURL, err := auth.BuildOIDCAuthorizeURL(oidcCfg, metadata, redirectURL, state, nonce)
	if err != nil {
		s.redirectOIDCError(c, "构造 IDaaS 登录地址失败: "+err.Error())
		return
	}

	s.storeOIDCState(state, nonce)
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, authorizeURL)
}

func (s *Server) handleOIDCCallback(c *gin.Context) {
	if !s.hasAuthMode(auth.AuthModeOIDC) {
		s.redirectOIDCError(c, "当前未启用 OIDC / IDaaS 登录")
		return
	}

	if providerError := strings.TrimSpace(c.Query("error")); providerError != "" {
		description := strings.TrimSpace(c.Query("error_description"))
		message := providerError
		if description != "" {
			message += ": " + description
		}
		s.redirectOIDCError(c, "IDaaS 登录失败: "+message)
		return
	}

	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	if code == "" || state == "" {
		s.redirectOIDCError(c, "IDaaS 回调缺少 code 或 state")
		return
	}

	storedState, ok := s.consumeOIDCState(state)
	if !ok {
		s.redirectOIDCError(c, "登录状态已失效，请重新发起 IDaaS 登录")
		return
	}

	oidcCfg := s.currentOIDCConfig()
	if !isOIDCConfigUsable(oidcCfg) {
		s.redirectOIDCError(c, "IDaaS 配置未完成，请联系管理员检查配置")
		return
	}

	metadata, err := auth.DiscoverOIDCProvider(c.Request.Context(), s.oidcClient(), oidcCfg.IssuerURL)
	if err != nil {
		s.redirectOIDCError(c, "读取 IDaaS 配置失败: "+err.Error())
		return
	}

	redirectURL := s.effectiveOIDCRedirectURL(c, oidcCfg)
	tokenResp, err := auth.ExchangeOIDCCode(c.Request.Context(), s.oidcClient(), oidcCfg, metadata, code, redirectURL)
	if err != nil {
		s.redirectOIDCError(c, "交换授权码失败: "+err.Error())
		return
	}

	identity, err := auth.VerifyOIDCIDToken(c.Request.Context(), s.oidcClient(), oidcCfg, metadata, tokenResp.IDToken, storedState.Nonce)
	if err != nil {
		s.redirectOIDCError(c, "校验 IDaaS 身份失败: "+err.Error())
		return
	}
	if identity == nil || identity.User == nil {
		s.redirectOIDCError(c, "IDaaS 未返回有效用户信息")
		return
	}

	if s.jwtManager == nil {
		s.redirectOIDCError(c, "服务端 JWT 未初始化")
		return
	}
	token, err := s.jwtManager.GenerateToken(identity.User)
	if err != nil {
		s.redirectOIDCError(c, "生成登录令牌失败: "+err.Error())
		return
	}

	s.renderOIDCSuccess(c, token, identity.User)
}

func (s *Server) renderOIDCSuccess(c *gin.Context, token string, user *auth.User) {
	tokenJSON, _ := json.Marshal(token)
	userJSON, _ := json.Marshal(user)
	redirectJSON, _ := json.Marshal(frontendHashURL(c, "/", nil))
	fallbackJSON, _ := json.Marshal(frontendHashURL(c, "/login", url.Values{
		"error": []string{"浏览器阻止了本地会话写入，请重试"},
	}))

	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>IDaaS 登录中</title>
  <style>
    body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:#f4f7fb;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#24324a}
    .card{max-width:420px;padding:28px 32px;border-radius:18px;background:#fff;box-shadow:0 18px 48px rgba(15,23,42,.12);text-align:center}
    .title{font-size:20px;font-weight:700;margin-bottom:10px}
    .desc{font-size:14px;line-height:1.7;color:#526079}
  </style>
</head>
<body>
  <div class="card">
    <div class="title">登录成功，正在进入系统</div>
    <div class="desc">正在写入本地会话并跳转，如果长时间停留在此页，请返回登录页重试。</div>
  </div>
  <script>
    const token = %s;
    const user = %s;
    const successRedirect = %s;
    const errorRedirect = %s;
    try {
      localStorage.setItem('auth_token', token);
      localStorage.setItem('auth_user', JSON.stringify(user));
      window.location.replace(successRedirect);
    } catch (err) {
      window.location.replace(errorRedirect);
    }
  </script>
</body>
</html>`, tokenJSON, userJSON, redirectJSON, fallbackJSON)

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}
