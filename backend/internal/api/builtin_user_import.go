package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"sop-chat/internal/auth"
	"sop-chat/internal/config"

	"github.com/gin-gonic/gin"
)

const builtinUsersImportMaxFileSize = 2 << 20

// handleDownloadBuiltinUsersTemplate 返回 builtin 用户导入模板。
func (s *Server) handleDownloadBuiltinUsersTemplate(c *gin.Context) {
	data, err := auth.BuildBuiltinUsersTemplateXLSX()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成模板失败: " + err.Error()})
		return
	}

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="builtin-users-template.xlsx"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

// handleImportBuiltinUsers 导入 xlsx 中的 builtin 用户，并直接写回统一配置。
func (s *Server) handleImportBuiltinUsers(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, builtinUsersImportMaxFileSize+1024)

	mode, err := auth.NormalizeBuiltinUserImportMode(c.PostForm("mode"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传 .xlsx 文件"})
		return
	}
	if fileHeader.Size <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上传文件不能为空"})
		return
	}
	if fileHeader.Size > builtinUsersImportMaxFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("上传文件过大，最大支持 %d MB", builtinUsersImportMaxFileSize>>20)})
		return
	}
	if !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".xlsx") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持导入 .xlsx 模板文件"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "打开上传文件失败: " + err.Error()})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, builtinUsersImportMaxFileSize+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传文件失败: " + err.Error()})
		return
	}
	if len(data) > builtinUsersImportMaxFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("上传文件过大，最大支持 %d MB", builtinUsersImportMaxFileSize>>20)})
		return
	}

	rows, err := auth.ParseBuiltinUsersXLSX(data)
	if err != nil {
		if writeBuiltinImportValidationError(c, err) {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.mu.RLock()
	existing := s.globalConfig
	configPath := s.configPath
	s.mu.RUnlock()

	if configPath == "" {
		configPath = "config.yaml"
		s.mu.Lock()
		s.configPath = configPath
		s.mu.Unlock()
	}

	baseConfig := existing
	if baseConfig == nil {
		baseConfig = config.DefaultConfig()
	}

	if baseConfig.BuiltinStorage() == "sqlite" {
		sqlitePath := config.ResolveBuiltinSQLitePath(configPath, baseConfig.Auth.Builtin)
		store, err := auth.NewSQLiteUserStore(sqlitePath, baseConfig.Auth.PasswordSalt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "打开 SQLite 用户库失败: " + err.Error()})
			return
		}
		defer store.Close()

		report, err := store.ApplyImport(rows, mode)
		if err != nil {
			if writeBuiltinImportValidationError(c, err) {
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		warning := ""
		if !containsString(baseConfig.Auth.Methods, "builtin") {
			warning = "导入已写入 SQLite，但当前 auth.methods 不包含 builtin，导入用户暂时无法用于登录。"
		}

		resp := gin.H{
			"message": buildBuiltinUsersImportMessage(report),
			"report":  report,
			"storage": "sqlite",
		}
		if warning != "" {
			resp["warning"] = true
			resp["warningMessage"] = warning
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	cfg, err := cloneConfigForSave(baseConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	report, err := auth.ApplyBuiltinUsersImport(cfg, rows, mode)
	if err != nil {
		if writeBuiltinImportValidationError(c, err) {
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	warning := ""
	if !containsString(cfg.Auth.Methods, "builtin") {
		warning = "导入已保存，但当前 auth.methods 不包含 builtin，导入用户暂时无法用于登录。"
	}

	if err := s.reloadConfig(); err != nil {
		resp := gin.H{
			"message": buildBuiltinUsersImportMessage(report) + " 配置已保存，但热重载失败，请手动重启服务。",
			"report":  report,
			"warning": true,
			"detail":  err.Error(),
		}
		if warning != "" {
			resp["warningMessage"] = warning
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	resp := gin.H{
		"message": buildBuiltinUsersImportMessage(report),
		"report":  report,
	}
	if warning != "" {
		resp["warning"] = true
		resp["warningMessage"] = warning
	}
	c.JSON(http.StatusOK, resp)
}

func buildBuiltinUsersImportMessage(report *auth.BuiltinUserImportReport) string {
	parts := []string{
		fmt.Sprintf("导入成功并已应用：共 %d 行", report.TotalRows),
		fmt.Sprintf("新增 %d 个用户", report.CreatedUsers),
		fmt.Sprintf("更新 %d 个用户", report.UpdatedUsers),
		fmt.Sprintf("新增 %d 个角色", report.CreatedRoles),
	}
	return strings.Join(parts, "，")
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == expected {
			return true
		}
	}
	return false
}

func writeBuiltinImportValidationError(c *gin.Context, err error) bool {
	var validationErr *auth.BuiltinUserImportValidationError
	if !errors.As(err, &validationErr) {
		return false
	}
	c.JSON(http.StatusBadRequest, gin.H{
		"error":   validationErr.Error(),
		"details": validationErr.Details,
	})
	return true
}
