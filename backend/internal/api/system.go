package api

import (
	"log"
	"net/http"

	"sop-chat/internal/auth"
	appversion "sop-chat/internal/version"
	"sop-chat/pkg/sopchat"

	"github.com/gin-gonic/gin"
)

// handleGetAccountId 获取当前阿里云账号ID
func (s *Server) handleGetAccountId(c *gin.Context) {
	s.mu.RLock()
	globalCfg := s.globalConfig
	legacyCfg := s.config
	s.mu.RUnlock()

	accessKeyID := ""
	accessKeySecret := ""
	if globalCfg != nil {
		if cfg, err := globalCfg.ToClientConfig(); err == nil && cfg != nil {
			accessKeyID = cfg.AccessKeyId
			accessKeySecret = cfg.AccessKeySecret
		}
	}
	if accessKeyID == "" && legacyCfg != nil {
		accessKeyID = legacyCfg.AccessKeyId
		accessKeySecret = legacyCfg.AccessKeySecret
	}
	accountId, err := sopchat.GetAccountId(accessKeyID, accessKeySecret)
	if err != nil {
		log.Printf("Failed to get account ID: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get account ID",
			"detail": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"accountId": accountId,
	})
}

// handleGetSystemConfig 获取系统配置（语言、时区等）
func (s *Server) handleGetSystemConfig(c *gin.Context) {
	language := "zh"
	timeZone := "Asia/Shanghai"

	if s.globalConfig != nil {
		language = s.globalConfig.GetLanguage()
		timeZone = s.globalConfig.GetTimeZone()
	}

	c.JSON(http.StatusOK, gin.H{
		"language": language,
		"timeZone": timeZone,
	})
}

// handleGetSetupStatus 返回系统是否已完成初始化配置（公开接口，无需认证）
// configured 表示“业务运行配置”是否完整；loginReady 表示“登录入口”是否已经可用。
func (s *Server) handleGetSetupStatus(c *gin.Context) {
	s.mu.RLock()
	cfg := s.config
	globalCfg := s.globalConfig
	authModes := append([]auth.AuthMode(nil), s.authModes...)
	userStore := s.userStore
	s.mu.RUnlock()

	authConfigured := len(authModes) > 0

	credConfigured := false
	if globalCfg != nil {
		if resolved, err := globalCfg.ToClientConfig(); err == nil && resolved != nil && resolved.AccessKeyId != "" {
			credConfigured = true
		}
	}
	if !credConfigured {
		credConfigured = cfg != nil && cfg.AccessKeyId != ""
	}

	// 检查是否存在至少一个用户账号
	usersConfigured := false
	builtinUserCount := 0
	if userStore != nil {
		if users, err := userStore.ListUsers(); err == nil && len(users) > 0 {
			usersConfigured = true
			builtinUserCount = len(users)
		}
	}

	builtinEnabled := false
	oidcEnabled := false
	for _, mode := range authModes {
		switch mode {
		case auth.AuthModeBuiltin:
			builtinEnabled = true
		case auth.AuthModeOIDC:
			oidcEnabled = true
		}
	}

	oidcConfigured := false
	builtinStorage := "yaml"
	if globalCfg != nil {
		oidcConfigured = isOIDCConfigUsable(globalCfg.Auth.OIDC)
		builtinStorage = globalCfg.BuiltinStorage()
	}

	builtinAvailable := builtinEnabled && usersConfigured
	oidcAvailable := oidcEnabled && oidcConfigured
	loginAvailable := builtinAvailable || oidcAvailable
	loginReady := authConfigured && loginAvailable
	versionInfo := appversion.Current()

	c.JSON(http.StatusOK, gin.H{
		"configured":       credConfigured && authConfigured && loginAvailable,
		"loginReady":       loginReady,
		"credConfigured":   credConfigured,
		"authConfigured":   authConfigured,
		"usersConfigured":  usersConfigured,
		"builtinUserCount": builtinUserCount,
		"builtinStorage":   builtinStorage,
		"methods":          authModesToStrings(authModes),
		"builtinEnabled":   builtinEnabled,
		"builtinAvailable": builtinAvailable,
		"oidcEnabled":      oidcEnabled,
		"oidcConfigured":   oidcConfigured,
		"oidcAvailable":    oidcAvailable,
		"version":          versionInfo.Version,
		"versionDisplay":   versionInfo.Display(),
		"versionCommit":    versionInfo.ShortCommit(),
		"versionDirty":     versionInfo.Dirty,
	})
}
