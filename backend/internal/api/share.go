package api

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"sop-chat/internal/auth"
	"sop-chat/internal/config"
	"sop-chat/internal/sharetoken"

	"github.com/gin-gonic/gin"
)

type createShareLinkRequest struct {
	EmployeeName   string `json:"employeeName" binding:"required"`
	ThreadID       string `json:"threadId" binding:"required"`
	CloudAccountID string `json:"cloudAccountId,omitempty"`
}

func (s *Server) handleCreateShareLink(c *gin.Context) {
	var req createShareLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid request parameters",
			"detail": err.Error(),
		})
		return
	}

	if s.shareManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Share token manager is not initialized",
		})
		return
	}

	cloudAccountID := config.NormalizeCloudAccountID(req.CloudAccountID)
	client, err := s.createClientForCloudAccount(cloudAccountID)
	if err != nil {
		log.Printf("Failed to create client for share link: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	threadResp, err := client.GetThread(req.EmployeeName, req.ThreadID)
	if err != nil {
		log.Printf("Failed to validate thread for share link: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get thread info",
			"detail": err.Error(),
		})
		return
	}
	if threadResp == nil || threadResp.Body == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Thread not found",
		})
		return
	}

	if user, exists := auth.GetUserFromContext(c); exists {
		owner := threadAttributeString(threadResp.Body.Attributes, threadUserAttributeKey)
		if owner != "" && owner != user.Username {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "You do not have permission to share this thread",
			})
			return
		}
	}

	shareToken, expiresAt, err := s.shareManager.Generate(req.EmployeeName, req.ThreadID, cloudAccountID)
	if err != nil {
		log.Printf("Failed to generate share token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to generate share token",
			"detail": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"shareToken": shareToken,
		"expiresAt":  expiresAt.Format(time.RFC3339),
	})
}

func (s *Server) validateShareToken(c *gin.Context, expectedEmployeeName, expectedThreadID string) (*sharetoken.Claims, bool) {
	if s.shareManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Share token manager is not initialized",
		})
		return nil, false
	}

	token := strings.TrimSpace(c.Query(sharetoken.QueryParam))
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "Missing share token",
			"detail": "分享链接缺少访问令牌，请重新生成分享链接。",
		})
		return nil, false
	}

	claims, err := s.shareManager.Validate(token)
	if err != nil {
		statusCode := http.StatusUnauthorized
		errorMessage := "Invalid share token"
		detail := "分享链接无效，请重新生成分享链接。"
		if errors.Is(err, sharetoken.ErrExpiredToken) {
			statusCode = http.StatusGone
			errorMessage = "Share link expired"
			detail = "分享链接已过期，请重新生成分享链接。"
		}
		c.JSON(statusCode, gin.H{
			"error":  errorMessage,
			"detail": detail,
		})
		return nil, false
	}

	if expectedEmployeeName = strings.TrimSpace(expectedEmployeeName); expectedEmployeeName != "" && claims.EmployeeName != expectedEmployeeName {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "Share token does not match employee",
			"detail": "分享链接与当前员工不匹配。",
		})
		return nil, false
	}
	if expectedThreadID = strings.TrimSpace(expectedThreadID); expectedThreadID != "" && claims.ThreadID != expectedThreadID {
		c.JSON(http.StatusForbidden, gin.H{
			"error":  "Share token does not match thread",
			"detail": "分享链接与当前会话不匹配。",
		})
		return nil, false
	}

	return claims, true
}
