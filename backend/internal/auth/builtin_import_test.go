package auth

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"testing"

	"sop-chat/internal/config"
)

func TestParseBuiltinUsersXLSX(t *testing.T) {
	data := buildImportWorkbookForTest(t, [][]string{
		{"admin.local", "Strong@123456", "admin"},
		{"ops.user", "Temp@123456", "user, ops, user"},
	})

	rows, err := ParseBuiltinUsersXLSX(data)
	if err != nil {
		t.Fatalf("ParseBuiltinUsersXLSX returned error: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Username != "admin.local" || rows[0].InitialPassword != "Strong@123456" {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if len(rows[1].Roles) != 2 || rows[1].Roles[0] != "user" || rows[1].Roles[1] != "ops" {
		t.Fatalf("expected deduplicated roles, got %+v", rows[1].Roles)
	}
}

func TestParseBuiltinUsersXLSXValidation(t *testing.T) {
	data := buildImportWorkbookForTest(t, [][]string{
		{"admin.local", "Strong@123456", "admin"},
		{"admin.local", "", "ops"},
	})

	_, err := ParseBuiltinUsersXLSX(data)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	validationErr, ok := err.(*BuiltinUserImportValidationError)
	if !ok {
		t.Fatalf("expected BuiltinUserImportValidationError, got %T", err)
	}
	if len(validationErr.Details) == 0 {
		t.Fatalf("expected validation details, got %+v", validationErr)
	}
}

func TestApplyBuiltinUsersImportAppend(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			PasswordSalt: "salt-",
		},
	}

	report, err := ApplyBuiltinUsersImport(cfg, []BuiltinUserImportRow{
		{Row: 2, Username: "admin.local", InitialPassword: "Strong@123456", Roles: []string{"admin"}},
		{Row: 3, Username: "ops.user", InitialPassword: "Temp@123456", Roles: []string{"user", "ops"}},
	}, BuiltinUserImportModeAppend)
	if err != nil {
		t.Fatalf("ApplyBuiltinUsersImport returned error: %v", err)
	}

	if report.CreatedUsers != 2 || report.UpdatedUsers != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(cfg.Auth.BuiltinUsers) != 2 {
		t.Fatalf("expected 2 builtin users, got %+v", cfg.Auth.BuiltinUsers)
	}
	if cfg.Auth.BuiltinUsers[0].Password != HashPassword("salt-", "Strong@123456") {
		t.Fatalf("unexpected password hash: %+v", cfg.Auth.BuiltinUsers[0])
	}
	if len(cfg.Auth.Roles) != 3 {
		t.Fatalf("expected 3 roles, got %+v", cfg.Auth.Roles)
	}
}

func TestApplyBuiltinUsersImportOverwrite(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			PasswordSalt: "salt-",
			BuiltinUsers: []config.UserConfig{
				{Name: "admin.local", Password: "old-hash"},
				{Name: "other.user", Password: "other-hash"},
			},
			Roles: []config.RoleConfig{
				{Name: "admin", Users: []string{"admin.local", "other.user"}},
				{Name: "legacy", Users: []string{"admin.local"}},
			},
		},
	}

	report, err := ApplyBuiltinUsersImport(cfg, []BuiltinUserImportRow{
		{Row: 2, Username: "admin.local", InitialPassword: "New@123456", Roles: []string{"ops"}},
	}, BuiltinUserImportModeOverwrite)
	if err != nil {
		t.Fatalf("ApplyBuiltinUsersImport returned error: %v", err)
	}

	if report.CreatedUsers != 0 || report.UpdatedUsers != 1 || report.CreatedRoles != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if cfg.Auth.BuiltinUsers[0].Password != HashPassword("salt-", "New@123456") {
		t.Fatalf("expected password to be overwritten, got %+v", cfg.Auth.BuiltinUsers[0])
	}
	if got := cfg.Auth.Roles[0].Users; len(got) != 1 || got[0] != "other.user" {
		t.Fatalf("expected admin role to keep only other.user, got %+v", got)
	}
	if got := cfg.Auth.Roles[1].Users; len(got) != 0 {
		t.Fatalf("expected legacy role to be emptied, got %+v", got)
	}
	if got := cfg.Auth.Roles[2].Users; len(got) != 1 || got[0] != "admin.local" {
		t.Fatalf("expected new ops role to include imported user, got %+v", got)
	}
}

func buildImportWorkbookForTest(t *testing.T, rows [][]string) []byte {
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
		"xl/worksheets/sheet1.xml": buildWorksheetXML(rows),
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

func buildWorksheetXML(rows [][]string) string {
	allRows := make([][]string, 0, len(rows)+1)
	allRows = append(allRows, []string{"username", "initial_password", "roles"})
	allRows = append(allRows, rows...)

	var builder bytes.Buffer
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	builder.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for rowIndex, row := range allRows {
		builder.WriteString(fmt.Sprintf(`<row r="%d">`, rowIndex+1))
		for colIndex, value := range row {
			builder.WriteString(fmt.Sprintf(`<c r="%s%d" t="inlineStr"><is><t>`, xlsxColumnName(colIndex), rowIndex+1))
			builder.WriteString(escapeXMLText(value))
			builder.WriteString(`</t></is></c>`)
		}
		builder.WriteString(`</row>`)
	}
	builder.WriteString(`</sheetData></worksheet>`)
	return builder.String()
}

func xlsxColumnName(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+(index%26))) + name
		index = index/26 - 1
	}
	return name
}

func escapeXMLText(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		panic(err)
	}
	return buf.String()
}
