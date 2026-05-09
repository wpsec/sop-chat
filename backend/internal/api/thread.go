package api

import (
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/gin-gonic/gin"

	"sop-chat/internal/auth"
	"sop-chat/internal/config"
	"sop-chat/pkg/sopchat"
)

const (
	threadUserAttributeKey          = "user"
	threadFirstQuestionAttributeKey = "firstUserQuestion"
	threadQuestionPreviewMaxRunes   = 80
)

// CreateThreadRequest 创建线程请求
type CreateThreadRequest struct {
	EmployeeName   string                 `json:"employeeName" binding:"required"`
	CloudAccountID string                 `json:"cloudAccountId,omitempty"`
	Message        string                 `json:"message,omitempty"`
	Title          string                 `json:"title"`
	Attributes     map[string]interface{} `json:"attributes"`
}

// handleCreateThread 创建会话线程
func (s *Server) handleCreateThread(c *gin.Context) {
	var req CreateThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid request parameters",
			"detail": err.Error(),
		})
		return
	}

	effectiveCloudAccountID := req.CloudAccountID
	if strings.TrimSpace(req.Message) != "" {
		s.mu.RLock()
		globalCfg := s.globalConfig
		s.mu.RUnlock()
		if globalCfg != nil {
			if matches := globalCfg.MatchCloudAccountIDsByText(req.Message, nil); len(matches) == 1 {
				effectiveCloudAccountID = matches[0]
			}
		}
	}

	runtimeCfg, options, err := s.resolveEmployeeRuntime(req.EmployeeName, effectiveCloudAccountID, req.Message)
	if err != nil {
		log.Printf("Failed to resolve thread runtime: %v", err)
		statusCode := http.StatusInternalServerError
		payload := gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		}
		if len(options) > 0 {
			statusCode = http.StatusBadRequest
			payload["error"] = "目标环境不明确，请确认云账号"
			payload["needConfirm"] = true
			payload["options"] = options
		}
		c.JSON(statusCode, payload)
		return
	}
	log.Printf(
		"Creating thread employee=%s resolvedEmployee=%s cloudAccountId=%s product=%s project=%s workspace=%s region=%s endpoint=%s",
		req.EmployeeName,
		runtimeCfg.EmployeeName,
		runtimeCfg.CloudAccountID,
		runtimeCfg.Context.Product,
		runtimeCfg.Context.Project,
		runtimeCfg.Context.Workspace,
		runtimeCfg.Context.Region,
		runtimeCfg.ClientConfig.Endpoint,
	)

	client, err := newSOPChatClientFromClientConfig(runtimeCfg.ClientConfig)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	attributes := req.Attributes
	if attributes == nil {
		attributes = make(map[string]interface{})
	}

	// 将登录用户名写入 user attribute，用于后续按用户过滤线程
	if user, exists := auth.GetUserFromContext(c); exists {
		attributes["user"] = user.Username
	}

	threadConfig := &sopchat.ThreadConfig{
		EmployeeName: runtimeCfg.EmployeeName,
		Title:        req.Title,
		Attributes:   attributes,
		Project:      runtimeCfg.Context.Project,
		Workspace:    runtimeCfg.Context.Workspace,
		Region:       runtimeCfg.Context.Region,
	}

	response, err := client.CreateThread(threadConfig)
	if err != nil {
		log.Printf("Failed to create thread: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create thread",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Create thread response is empty",
		})
		return
	}

	result := gin.H{}
	if response.Body.ThreadId != nil {
		result["threadId"] = *response.Body.ThreadId
	}
	if response.Body.RequestId != nil {
		result["requestId"] = *response.Body.RequestId
	}
	result["cloudAccountId"] = runtimeCfg.CloudAccountID
	result["employeeName"] = runtimeCfg.EmployeeName

	c.JSON(http.StatusOK, result)
}

// handleListThreads 列出员工的所有线程
func (s *Server) handleListThreads(c *gin.Context) {
	employeeName := c.Param("employeeName")
	if employeeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name cannot be empty",
		})
		return
	}

	cloudAccountID := c.Query("cloudAccountId")
	includeQuestionPreview := isTruthyQueryValue(c.Query("includeQuestionPreview"))
	limit, err := parseThreadLimit(c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "Invalid limit",
			"detail": err.Error(),
		})
		return
	}
	client, err := s.createClientForCloudAccount(cloudAccountID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	// 按登录用户的 user attribute 过滤线程
	var filters []sopchat.ThreadFilter
	if user, exists := auth.GetUserFromContext(c); exists {
		filters = append(filters, sopchat.ThreadFilter{
			Key:   "user",
			Value: user.Username,
		})
	}

	response, err := client.ListThreads(employeeName, filters)
	if err != nil {
		log.Printf("Failed to list threads: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to list threads",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil || response.Body.Threads == nil {
		c.JSON(http.StatusOK, gin.H{
			"threads": []gin.H{},
		})
		return
	}

	normalizedCloudAccountID := config.NormalizeCloudAccountID(cloudAccountID)
	threadItems := append([]*cmsclient.ListThreadsResponseBodyThreads(nil), response.Body.Threads...)
	sort.SliceStable(threadItems, func(i, j int) bool {
		left := threadSortTime(threadItems[i])
		right := threadSortTime(threadItems[j])
		if left.Equal(right) {
			return strings.TrimSpace(pointerString(threadItems[i].ThreadId)) > strings.TrimSpace(pointerString(threadItems[j].ThreadId))
		}
		return left.After(right)
	})
	if limit > 0 && len(threadItems) > limit {
		threadItems = threadItems[:limit]
	}

	threads := make([]gin.H, 0, len(threadItems))
	for _, thread := range threadItems {
		item := gin.H{
			"employeeName": employeeName,
		}
		if thread.ThreadId != nil {
			item["threadId"] = *thread.ThreadId
		}
		if thread.Title != nil {
			item["title"] = *thread.Title
		}
		if thread.CreateTime != nil {
			item["createTime"] = *thread.CreateTime
		}
		if thread.Status != nil {
			item["status"] = *thread.Status
		}
		questionPreview := normalizeThreadQuestionPreview(
			threadAttributeString(thread.Attributes, threadFirstQuestionAttributeKey),
		)
		threadID := pointerString(thread.ThreadId)
		if questionPreview == "" {
			if cached, ok := s.loadThreadQuestionPreview(normalizedCloudAccountID, employeeName, threadID); ok {
				questionPreview = cached
			} else if includeQuestionPreview {
				questionPreview = s.fetchAndCacheThreadQuestionPreview(client, normalizedCloudAccountID, employeeName, threadID)
			}
		}
		if questionPreview != "" {
			item["questionPreview"] = questionPreview
		}
		if normalizedCloudAccountID != "" {
			item["cloudAccountId"] = normalizedCloudAccountID
		}
		threads = append(threads, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"threads": threads,
	})
}

// handleGetThread 获取线程详细信息
func (s *Server) handleGetThread(c *gin.Context) {
	employeeName := c.Param("employeeName")
	threadId := c.Param("threadId")

	if employeeName == "" || threadId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name and thread ID cannot be empty",
		})
		return
	}

	cloudAccountID := c.Query("cloudAccountId")
	client, err := s.createClientForCloudAccount(cloudAccountID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetThread(employeeName, threadId)
	if err != nil {
		log.Printf("Failed to get thread info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get thread info",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Thread not found",
		})
		return
	}

	body := response.Body
	result := gin.H{}
	if body.ThreadId != nil {
		result["threadId"] = *body.ThreadId
	}
	if body.Title != nil {
		result["title"] = *body.Title
	}
	if body.CreateTime != nil {
		result["createTime"] = *body.CreateTime
	}
	if body.Status != nil {
		result["status"] = *body.Status
	}
	if questionPreview := normalizeThreadQuestionPreview(
		threadAttributeString(body.Attributes, threadFirstQuestionAttributeKey),
	); questionPreview != "" {
		result["questionPreview"] = questionPreview
	}
	if normalizedCloudAccountID := config.NormalizeCloudAccountID(cloudAccountID); normalizedCloudAccountID != "" {
		result["cloudAccountId"] = normalizedCloudAccountID
	}

	c.JSON(http.StatusOK, result)
}

// handleGetThreadMessages 获取线程的消息历史
func (s *Server) handleGetThreadMessages(c *gin.Context) {
	employeeName := c.Param("employeeName")
	threadId := c.Param("threadId")

	if employeeName == "" || threadId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name and thread ID cannot be empty",
		})
		return
	}

	cloudAccountID := c.Query("cloudAccountId")
	client, err := s.createClientForCloudAccount(cloudAccountID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetThreadData(employeeName, threadId)
	if err != nil {
		log.Printf("Failed to get thread messages: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get thread messages",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil || response.Body.Data == nil || len(response.Body.Data) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"messages": []gin.H{},
		})
		return
	}

	if questionPreview := extractThreadQuestionPreview(response.Body.Data); questionPreview != "" {
		s.storeThreadQuestionPreview(config.NormalizeCloudAccountID(cloudAccountID), employeeName, threadId, questionPreview)
	}

	// 收集所有数据记录中的消息
	messages := make([]gin.H, 0)
	for _, data := range response.Body.Data {
		if data.Messages == nil {
			continue
		}
		for _, msg := range data.Messages {
			item := gin.H{}
			if msg.Role != nil {
				item["role"] = *msg.Role
			}
			if msg.Contents != nil && len(msg.Contents) > 0 {
				contents := make([]gin.H, 0, len(msg.Contents))
				for _, content := range msg.Contents {
					// content 是 map[string]interface{}
					contentItem := gin.H{}
					if contentType, ok := content["type"].(string); ok {
						contentItem["type"] = contentType
					}
					if contentValue, ok := content["value"].(string); ok {
						contentItem["value"] = contentValue
					}
					contents = append(contents, contentItem)
				}
				item["contents"] = contents
			}
			// 添加 Tools 字段（工具调用）
			if msg.Tools != nil && len(msg.Tools) > 0 {
				item["tools"] = msg.Tools
			}
			messages = append(messages, item)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"messages": messages,
	})
}

// handleGetSharedThread 获取分享的线程详细信息（公开访问，无需认证）
func (s *Server) handleGetSharedThread(c *gin.Context) {
	employeeName := c.Param("employeeName")
	threadId := c.Param("threadId")
	claims, ok := s.validateShareToken(c, employeeName, threadId)
	if !ok {
		return
	}

	client, err := s.createClientForCloudAccount(claims.CloudAccountID)
	if err != nil {
		log.Printf("Failed to create share client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetThread(employeeName, threadId)
	if err != nil {
		log.Printf("Failed to get shared thread info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get thread info",
			"detail": err.Error(),
		})
		return
	}
	if response.Body == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Thread not found",
		})
		return
	}

	body := response.Body
	result := gin.H{}
	if body.ThreadId != nil {
		result["threadId"] = *body.ThreadId
	}
	if body.Title != nil {
		result["title"] = *body.Title
	}
	if body.CreateTime != nil {
		result["createTime"] = *body.CreateTime
	}
	if body.Status != nil {
		result["status"] = *body.Status
	}
	if questionPreview := normalizeThreadQuestionPreview(
		threadAttributeString(body.Attributes, threadFirstQuestionAttributeKey),
	); questionPreview != "" {
		result["questionPreview"] = questionPreview
	}
	result["cloudAccountId"] = config.NormalizeCloudAccountID(claims.CloudAccountID)
	if claims.ExpiresAt != nil {
		result["shareExpiresAt"] = claims.ExpiresAt.Time.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, result)
}

// handleGetSharedThreadMessages 获取分享的线程消息（公开访问，无需认证）
func (s *Server) handleGetSharedThreadMessages(c *gin.Context) {
	employeeName := c.Param("employeeName")
	threadId := c.Param("threadId")
	claims, ok := s.validateShareToken(c, employeeName, threadId)
	if !ok {
		return
	}

	client, err := s.createClientForCloudAccount(claims.CloudAccountID)
	if err != nil {
		log.Printf("Failed to create share client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetThreadData(employeeName, threadId)
	if err != nil {
		log.Printf("Failed to get shared thread messages: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get thread messages",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil || response.Body.Data == nil || len(response.Body.Data) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"messages": []gin.H{},
		})
		return
	}

	if questionPreview := extractThreadQuestionPreview(response.Body.Data); questionPreview != "" {
		s.storeThreadQuestionPreview(config.NormalizeCloudAccountID(claims.CloudAccountID), employeeName, threadId, questionPreview)
	}

	messages := make([]gin.H, 0)
	for _, data := range response.Body.Data {
		if data.Messages == nil {
			continue
		}
		for _, msg := range data.Messages {
			item := gin.H{}
			if msg.Role != nil {
				item["role"] = *msg.Role
			}
			if msg.Contents != nil && len(msg.Contents) > 0 {
				contents := make([]gin.H, 0, len(msg.Contents))
				for _, content := range msg.Contents {
					contentItem := gin.H{}
					if contentType, ok := content["type"].(string); ok {
						contentItem["type"] = contentType
					}
					if contentValue, ok := content["value"].(string); ok {
						contentItem["value"] = contentValue
					}
					contents = append(contents, contentItem)
				}
				item["contents"] = contents
			}
			if msg.Tools != nil && len(msg.Tools) > 0 {
				item["tools"] = msg.Tools
			}
			messages = append(messages, item)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"messages": messages,
	})
}

// handleGetSharedEmployee 获取分享的员工信息（公开访问，无需认证）
func (s *Server) handleGetSharedEmployee(c *gin.Context) {
	employeeName := c.Param("employeeName")
	// 如果路由是 /share/employee/:employeeName，参数名就是 employeeName
	if employeeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Employee name cannot be empty",
		})
		return
	}

	claims, ok := s.validateShareToken(c, employeeName, "")
	if !ok {
		return
	}

	client, err := s.createClientForCloudAccount(claims.CloudAccountID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to create client",
			"detail": err.Error(),
		})
		return
	}

	response, err := client.GetEmployee(employeeName)
	if err != nil {
		log.Printf("Failed to get employee info: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Failed to get employee info",
			"detail": err.Error(),
		})
		return
	}

	if response.Body == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Employee not found",
		})
		return
	}

	body := response.Body
	result := gin.H{}
	if body.Name != nil {
		result["name"] = *body.Name
	}
	if body.DisplayName != nil {
		result["displayName"] = *body.DisplayName
	}
	if body.Description != nil {
		result["description"] = *body.Description
	}
	result["cloudAccountId"] = config.NormalizeCloudAccountID(claims.CloudAccountID)

	c.JSON(http.StatusOK, result)
}

func parseThreadLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, strconv.ErrSyntax
	}
	return limit, nil
}

func isTruthyQueryValue(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func threadAttributeString(attrs map[string]*string, key string) string {
	if len(attrs) == 0 {
		return ""
	}
	if value, ok := attrs[key]; ok && value != nil {
		return *value
	}
	return ""
}

func normalizeThreadQuestionPreview(text string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if normalized == "" {
		return ""
	}

	runes := []rune(normalized)
	if len(runes) <= threadQuestionPreviewMaxRunes {
		return normalized
	}
	return strings.TrimSpace(string(runes[:threadQuestionPreviewMaxRunes])) + "..."
}

func extractThreadQuestionPreview(data []*cmsclient.GetThreadDataResponseBodyData) string {
	for _, item := range data {
		if item == nil || len(item.Messages) == 0 {
			continue
		}
		for _, message := range item.Messages {
			if message == nil || message.Role == nil || strings.ToLower(strings.TrimSpace(*message.Role)) != "user" {
				continue
			}
			text := extractThreadQuestionPreviewText(message.Contents)
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func extractThreadQuestionPreviewText(contents []map[string]interface{}) string {
	if len(contents) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, content := range contents {
		contentType, _ := content["type"].(string)
		if contentType != "text" {
			continue
		}
		value, _ := content["value"].(string)
		if value == "" {
			continue
		}
		builder.WriteString(value)
	}

	return normalizeThreadQuestionPreview(builder.String())
}

func threadSortTime(thread *cmsclient.ListThreadsResponseBodyThreads) time.Time {
	for _, candidate := range []string{pointerString(thread.UpdateTime), pointerString(thread.CreateTime)} {
		if parsed, ok := parseThreadTime(candidate); ok {
			return parsed
		}
	}
	return time.Time{}
}

func parseThreadTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func threadQuestionPreviewCacheKey(cloudAccountID, employeeName, threadID string) string {
	return config.NormalizeCloudAccountID(cloudAccountID) + "\x00" +
		strings.TrimSpace(employeeName) + "\x00" +
		strings.TrimSpace(threadID)
}

func (s *Server) loadThreadQuestionPreview(cloudAccountID, employeeName, threadID string) (string, bool) {
	value, ok := s.threadPreviewCache.Load(threadQuestionPreviewCacheKey(cloudAccountID, employeeName, threadID))
	if !ok {
		return "", false
	}
	preview, ok := value.(string)
	return preview, ok && preview != ""
}

func (s *Server) storeThreadQuestionPreview(cloudAccountID, employeeName, threadID, preview string) {
	preview = normalizeThreadQuestionPreview(preview)
	if preview == "" {
		return
	}
	s.threadPreviewCache.Store(threadQuestionPreviewCacheKey(cloudAccountID, employeeName, threadID), preview)
}

func (s *Server) fetchAndCacheThreadQuestionPreview(client *sopchat.Client, cloudAccountID, employeeName, threadID string) string {
	if client == nil || strings.TrimSpace(threadID) == "" {
		return ""
	}
	if preview, ok := s.loadThreadQuestionPreview(cloudAccountID, employeeName, threadID); ok {
		return preview
	}

	response, err := client.GetThreadData(employeeName, threadID)
	if err != nil || response == nil || response.Body == nil {
		if err != nil {
			log.Printf("Failed to fetch thread preview for %s: %v", threadID, err)
		}
		return ""
	}

	preview := extractThreadQuestionPreview(response.Body.Data)
	if preview != "" {
		s.storeThreadQuestionPreview(cloudAccountID, employeeName, threadID, preview)
	}
	return preview
}
