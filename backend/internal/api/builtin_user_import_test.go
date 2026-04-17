package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"sop-chat/internal/auth"
	"sop-chat/internal/config"

	"github.com/gin-gonic/gin"
)

func TestHandleDownloadBuiltinUsersTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := &Server{configUIToken: "test-token"}
	router := gin.New()
	router.GET("/admin-ui/api/auth/builtin-users/template", server.configUITokenMiddleware(), server.handleDownloadBuiltinUsersTemplate)

	req := httptest.NewRequest(http.MethodGet, "/admin-ui/api/auth/builtin-users/template?token=test-token", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("unexpected content-type: %s", got)
	}
	if _, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len())); err != nil {
		t.Fatalf("expected response to be a valid xlsx zip, got error: %v", err)
	}
}

func TestHandleImportBuiltinUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	server := &Server{
		configUIToken: "test-token",
		configPath:    configPath,
	}
	router := gin.New()
	router.POST("/admin-ui/api/auth/builtin-users/import", server.configUITokenMiddleware(), server.handleImportBuiltinUsers)

	body, contentType := buildBuiltinUsersImportRequestBody(t, "append", buildImportWorkbookForAPITest(t, [][]string{
		{"admin.local", "Strong@123456", "admin"},
		{"ops.user", "Temp@123456", "user,ops"},
	}))
	req := httptest.NewRequest(http.MethodPost, "/admin-ui/api/auth/builtin-users/import?token=test-token", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Message string                       `json:"message"`
		Report  auth.BuiltinUserImportReport `json:"report"`
		Warning bool                         `json:"warning"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Report.CreatedUsers != 2 || resp.Report.UpdatedUsers != 0 || resp.Warning {
		t.Fatalf("unexpected import response: %+v", resp)
	}

	cfg, _, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}
	if len(cfg.Auth.BuiltinUsers) != 2 {
		t.Fatalf("expected 2 builtin users in config, got %+v", cfg.Auth.BuiltinUsers)
	}
	if cfg.Auth.BuiltinUsers[0].Name != "admin.local" {
		t.Fatalf("unexpected first user: %+v", cfg.Auth.BuiltinUsers[0])
	}
	if cfg.Auth.BuiltinUsers[0].Password != auth.HashPassword(cfg.Auth.PasswordSalt, "Strong@123456") {
		t.Fatalf("expected imported password hash to use saved salt, got %+v", cfg.Auth.BuiltinUsers[0])
	}
	if len(cfg.Auth.Roles) != 3 {
		t.Fatalf("expected imported roles to be saved, got %+v", cfg.Auth.Roles)
	}
}

func TestHandleImportBuiltinUsersConflictDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	existing := config.DefaultConfig()
	existing.Auth.BuiltinUsers = []config.UserConfig{{Name: "admin.local", Password: "old-hash"}}
	existing.Auth.Roles = []config.RoleConfig{{Name: "admin", Users: []string{"admin.local"}}}
	if err := config.SaveConfig(configPath, existing); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}

	server := &Server{
		configUIToken: "test-token",
		configPath:    configPath,
		globalConfig:  existing,
	}
	router := gin.New()
	router.POST("/admin-ui/api/auth/builtin-users/import", server.configUITokenMiddleware(), server.handleImportBuiltinUsers)

	body, contentType := buildBuiltinUsersImportRequestBody(t, "append", buildImportWorkbookForAPITest(t, [][]string{
		{"admin.local", "Strong@123456", "admin"},
		{"admin.local-2", "Temp@123456", "user"},
	}))
	req := httptest.NewRequest(http.MethodPost, "/admin-ui/api/auth/builtin-users/import?token=test-token", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Error   string   `json:"error"`
		Details []string `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if len(resp.Details) != 1 || resp.Details[0] == "" {
		t.Fatalf("expected detailed conflict errors, got %+v", resp)
	}
}

func TestHandleImportBuiltinUsersToSQLite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.DefaultConfig()
	cfg.Auth.Builtin = &config.BuiltinAuthConfig{
		Storage:    "sqlite",
		SQLitePath: "data/builtin-users.db",
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}

	server := &Server{
		configUIToken: "test-token",
		configPath:    configPath,
		globalConfig:  cfg,
	}
	router := gin.New()
	router.POST("/admin-ui/api/auth/builtin-users/import", server.configUITokenMiddleware(), server.handleImportBuiltinUsers)

	body, contentType := buildBuiltinUsersImportRequestBody(t, "append", buildImportWorkbookForAPITest(t, [][]string{
		{"sqlite.admin", "Strong@123456", "admin"},
		{"sqlite.ops", "Temp@123456", "ops,user"},
	}))
	req := httptest.NewRequest(http.MethodPost, "/admin-ui/api/auth/builtin-users/import?token=test-token", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	store, err := auth.NewSQLiteUserStore(filepath.Join(filepath.Dir(configPath), "data", "builtin-users.db"), cfg.Auth.PasswordSalt)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	users, err := store.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 sqlite users, got %+v", users)
	}
	if users[0].PasswordHash == "" || users[1].PasswordHash == "" {
		t.Fatalf("expected sqlite users to have password hashes, got %+v", users)
	}
}

func buildBuiltinUsersImportRequestBody(t *testing.T, mode string, workbook []byte) (*bytes.Buffer, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("mode", mode); err != nil {
		t.Fatalf("failed to write mode field: %v", err)
	}
	part, err := writer.CreateFormFile("file", "builtin-users.xlsx")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write(workbook); err != nil {
		t.Fatalf("failed to write workbook: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

func buildImportWorkbookForAPITest(t *testing.T, rows [][]string) []byte {
	t.Helper()

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`,
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="users" sheetId="1" r:id="rId1"/>
  </sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": buildBuiltinUsersWorksheetXML(rows),
	}

	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close workbook: %v", err)
	}
	return buf.Bytes()
}

func buildBuiltinUsersWorksheetXML(rows [][]string) string {
	allRows := make([][]string, 0, len(rows)+1)
	allRows = append(allRows, []string{"username", "initial_password", "roles"})
	allRows = append(allRows, rows...)

	var builder bytes.Buffer
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	builder.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for rowIndex, row := range allRows {
		builder.WriteString(fmt.Sprintf(`<row r="%d">`, rowIndex+1))
		for colIndex, value := range row {
			builder.WriteString(fmt.Sprintf(`<c r="%s%d" t="inlineStr"><is><t>`, xlsxColumnNameForAPITest(colIndex), rowIndex+1))
			builder.WriteString(escapeXMLTextForAPITest(value))
			builder.WriteString(`</t></is></c>`)
		}
		builder.WriteString(`</row>`)
	}
	builder.WriteString(`</sheetData></worksheet>`)
	return builder.String()
}

func xlsxColumnNameForAPITest(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+(index%26))) + name
		index = index/26 - 1
	}
	return name
}

func escapeXMLTextForAPITest(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		panic(err)
	}
	return buf.String()
}
