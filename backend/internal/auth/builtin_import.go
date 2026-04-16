package auth

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"sop-chat/internal/config"
)

const (
	builtinUsersImportSheetName = "users"
	builtinUsersImportMaxRows   = 2000
)

// BuiltinUserImportMode 表示导入时如何处理已存在用户名。
type BuiltinUserImportMode string

const (
	BuiltinUserImportModeAppend    BuiltinUserImportMode = "append"
	BuiltinUserImportModeOverwrite BuiltinUserImportMode = "overwrite"
)

// BuiltinUserImportRow 表示一行导入记录。
type BuiltinUserImportRow struct {
	Row             int
	Username        string
	InitialPassword string
	Roles           []string
}

// BuiltinUserImportReport 返回本次导入的汇总结果。
type BuiltinUserImportReport struct {
	Mode         BuiltinUserImportMode `json:"mode"`
	TotalRows    int                   `json:"totalRows"`
	CreatedUsers int                   `json:"createdUsers"`
	UpdatedUsers int                   `json:"updatedUsers"`
	CreatedRoles int                   `json:"createdRoles"`
}

// BuiltinUserImportValidationError 收集模板校验问题，便于前端一次性展示。
type BuiltinUserImportValidationError struct {
	Details []string
}

type roleState struct {
	order []string
	set   map[string]struct{}
}

func (e *BuiltinUserImportValidationError) Error() string {
	if e == nil || len(e.Details) == 0 {
		return "导入文件校验失败"
	}
	return "导入文件校验失败: " + strings.Join(e.Details, "；")
}

// HashPassword 统一计算内置账号密码哈希，避免各入口各写一套规则。
func HashPassword(salt, plaintext string) string {
	hash := md5.Sum([]byte(salt + plaintext))
	return hex.EncodeToString(hash[:])
}

// NormalizeBuiltinUserImportMode 将前端传入的导入模式规范为受支持的取值。
func NormalizeBuiltinUserImportMode(v string) (BuiltinUserImportMode, error) {
	mode := BuiltinUserImportMode(strings.TrimSpace(strings.ToLower(v)))
	switch mode {
	case "", BuiltinUserImportModeAppend:
		return BuiltinUserImportModeAppend, nil
	case BuiltinUserImportModeOverwrite:
		return BuiltinUserImportModeOverwrite, nil
	default:
		return "", fmt.Errorf("不支持的导入模式: %s", v)
	}
}

// BuildBuiltinUsersTemplateXLSX 生成内置用户导入模板。
func BuildBuiltinUsersTemplateXLSX() ([]byte, error) {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)

	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
  <Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>
</Relationships>`,
		"docProps/app.xml": `<?xml version="1.0" encoding="UTF-8"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">
  <Application>sop-chat</Application>
</Properties>`,
		"docProps/core.xml": `<?xml version="1.0" encoding="UTF-8"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <dc:title>builtin users template</dc:title>
  <dc:creator>sop-chat</dc:creator>
</cp:coreProperties>`,
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
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <dimension ref="A1:C1"/>
  <sheetData>
    <row r="1">
      <c r="A1" t="inlineStr"><is><t>username</t></is></c>
      <c r="B1" t="inlineStr"><is><t>initial_password</t></is></c>
      <c r="C1" t="inlineStr"><is><t>roles</t></is></c>
    </row>
  </sheetData>
</worksheet>`,
	}

	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			return nil, fmt.Errorf("创建模板文件失败: %w", err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			return nil, fmt.Errorf("写入模板文件失败: %w", err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("生成模板压缩包失败: %w", err)
	}
	return buf.Bytes(), nil
}

// ParseBuiltinUsersXLSX 解析 users sheet 中的用户导入记录。
func ParseBuiltinUsersXLSX(data []byte) ([]BuiltinUserImportRow, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("读取 xlsx 文件失败: %w", err)
	}

	workbookData, err := readZipEntry(reader, "xl/workbook.xml")
	if err != nil {
		return nil, fmt.Errorf("读取工作簿失败: %w", err)
	}
	workbookRelsData, err := readZipEntry(reader, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, fmt.Errorf("读取工作簿关系失败: %w", err)
	}

	var workbook workbookXML
	if err := xml.Unmarshal(workbookData, &workbook); err != nil {
		return nil, fmt.Errorf("解析 workbook.xml 失败: %w", err)
	}
	var workbookRels workbookRelsXML
	if err := xml.Unmarshal(workbookRelsData, &workbookRels); err != nil {
		return nil, fmt.Errorf("解析 workbook.xml.rels 失败: %w", err)
	}

	relTargets := make(map[string]string, len(workbookRels.Relationships))
	for _, rel := range workbookRels.Relationships {
		target := strings.TrimSpace(rel.Target)
		if target == "" {
			continue
		}
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = path.Clean(path.Join("xl", target))
		}
		relTargets[rel.ID] = target
	}

	sheetPath := ""
	for _, sheet := range workbook.Sheets {
		if strings.EqualFold(strings.TrimSpace(sheet.Name), builtinUsersImportSheetName) {
			sheetPath = relTargets[sheet.RelID]
			break
		}
	}
	if sheetPath == "" {
		return nil, fmt.Errorf("未找到名为 %q 的工作表", builtinUsersImportSheetName)
	}

	sheetData, err := readZipEntry(reader, sheetPath)
	if err != nil {
		return nil, fmt.Errorf("读取工作表失败: %w", err)
	}

	sharedStrings := []string{}
	if sharedData, err := readZipEntry(reader, "xl/sharedStrings.xml"); err == nil {
		sharedStrings, err = parseSharedStrings(sharedData)
		if err != nil {
			return nil, fmt.Errorf("解析 sharedStrings.xml 失败: %w", err)
		}
	}

	return parseBuiltinUsersSheet(sheetData, sharedStrings)
}

// ApplyBuiltinUsersImport 将导入结果合并回统一配置中的 builtin 用户与角色。
func ApplyBuiltinUsersImport(cfg *config.Config, rows []BuiltinUserImportRow, mode BuiltinUserImportMode) (*BuiltinUserImportReport, error) {
	if cfg == nil {
		return nil, fmt.Errorf("配置为空，无法导入用户")
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("没有可导入的用户记录")
	}

	report := &BuiltinUserImportReport{
		Mode:      mode,
		TotalRows: len(rows),
	}

	userIndex := make(map[string]int, len(cfg.Auth.BuiltinUsers))
	for i, user := range cfg.Auth.BuiltinUsers {
		userIndex[user.Name] = i
	}
	if mode == BuiltinUserImportModeAppend {
		conflicts := make([]string, 0)
		for _, row := range rows {
			if _, exists := userIndex[row.Username]; exists {
				conflicts = append(conflicts, fmt.Sprintf("用户名 %q 已存在（模板第 %d 行）", row.Username, row.Row))
			}
		}
		if len(conflicts) > 0 {
			return nil, &BuiltinUserImportValidationError{Details: conflicts}
		}
	}

	roleStates := make(map[string]*roleState, len(cfg.Auth.Roles))
	roleOrder := make([]string, 0, len(cfg.Auth.Roles))
	for _, role := range cfg.Auth.Roles {
		if _, exists := roleStates[role.Name]; exists {
			continue
		}
		state := &roleState{
			order: make([]string, 0, len(role.Users)),
			set:   make(map[string]struct{}, len(role.Users)),
		}
		for _, username := range role.Users {
			name := strings.TrimSpace(username)
			if name == "" {
				continue
			}
			if _, exists := state.set[name]; exists {
				continue
			}
			state.set[name] = struct{}{}
			state.order = append(state.order, name)
		}
		roleStates[role.Name] = state
		roleOrder = append(roleOrder, role.Name)
	}

	for _, row := range rows {
		if mode == BuiltinUserImportModeOverwrite {
			for _, roleName := range roleOrder {
				removeUserFromRole(roleStates[roleName], row.Username)
			}
		}

		hashedPassword := HashPassword(cfg.Auth.PasswordSalt, row.InitialPassword)
		if idx, exists := userIndex[row.Username]; exists {
			cfg.Auth.BuiltinUsers[idx].Password = hashedPassword
			report.UpdatedUsers++
		} else {
			cfg.Auth.BuiltinUsers = append(cfg.Auth.BuiltinUsers, config.UserConfig{
				Name:     row.Username,
				Password: hashedPassword,
			})
			userIndex[row.Username] = len(cfg.Auth.BuiltinUsers) - 1
			report.CreatedUsers++
		}

		for _, roleName := range row.Roles {
			state, exists := roleStates[roleName]
			if !exists {
				state = &roleState{
					order: []string{},
					set:   map[string]struct{}{},
				}
				roleStates[roleName] = state
				roleOrder = append(roleOrder, roleName)
				report.CreatedRoles++
			}
			addUserToRole(state, row.Username)
		}
	}

	cfg.Auth.Roles = make([]config.RoleConfig, 0, len(roleOrder))
	for _, roleName := range roleOrder {
		state := roleStates[roleName]
		users := append([]string(nil), state.order...)
		cfg.Auth.Roles = append(cfg.Auth.Roles, config.RoleConfig{
			Name:  roleName,
			Users: users,
		})
	}

	return report, nil
}

func removeUserFromRole(state *roleState, username string) {
	if state == nil {
		return
	}
	if _, exists := state.set[username]; !exists {
		return
	}
	delete(state.set, username)
	filtered := state.order[:0]
	for _, current := range state.order {
		if current == username {
			continue
		}
		filtered = append(filtered, current)
	}
	state.order = filtered
}

func addUserToRole(state *roleState, username string) {
	if state == nil {
		return
	}
	if _, exists := state.set[username]; exists {
		return
	}
	state.set[username] = struct{}{}
	state.order = append(state.order, username)
}

func parseBuiltinUsersSheet(data []byte, sharedStrings []string) ([]BuiltinUserImportRow, error) {
	var sheet worksheetXML
	if err := xml.Unmarshal(data, &sheet); err != nil {
		return nil, fmt.Errorf("解析工作表失败: %w", err)
	}
	if len(sheet.Rows) == 0 {
		return nil, fmt.Errorf("模板中没有任何数据行")
	}

	headerIndex := -1
	headerRowNumber := 0
	headerColumns := map[int]string{}
	for i, row := range sheet.Rows {
		values := make(map[int]string, len(row.Cells))
		for _, cell := range row.Cells {
			columnIndex, ok := cellColumnIndex(cell.Ref)
			if !ok {
				continue
			}
			values[columnIndex] = normalizeHeaderName(cellValue(cell, sharedStrings))
		}
		if len(values) == 0 {
			continue
		}
		headerIndex = i
		headerRowNumber = effectiveRowNumber(row, i)
		headerColumns = values
		break
	}
	if headerIndex == -1 {
		return nil, fmt.Errorf("模板中未找到表头")
	}

	usernameColumn, ok := findHeaderColumn(headerColumns, "username")
	if !ok {
		return nil, fmt.Errorf("模板缺少 username 列")
	}
	passwordColumn, ok := findHeaderColumn(headerColumns, "initial_password")
	if !ok {
		return nil, fmt.Errorf("模板缺少 initial_password 列")
	}
	rolesColumn, hasRoles := findHeaderColumn(headerColumns, "roles")

	rows := make([]BuiltinUserImportRow, 0, len(sheet.Rows)-headerIndex-1)
	seenUsernames := make(map[string]int)
	validationErrors := &BuiltinUserImportValidationError{}

	for i := headerIndex + 1; i < len(sheet.Rows); i++ {
		row := sheet.Rows[i]
		rowNumber := effectiveRowNumber(row, i)
		values := make(map[int]string, len(row.Cells))
		for _, cell := range row.Cells {
			columnIndex, ok := cellColumnIndex(cell.Ref)
			if !ok {
				continue
			}
			values[columnIndex] = strings.TrimSpace(cellValue(cell, sharedStrings))
		}

		username := values[usernameColumn]
		password := values[passwordColumn]
		rolesRaw := ""
		if hasRoles {
			rolesRaw = values[rolesColumn]
		}
		if username == "" && password == "" && rolesRaw == "" {
			continue
		}

		if username == "" {
			validationErrors.Details = append(validationErrors.Details, fmt.Sprintf("第 %d 行用户名不能为空", rowNumber))
			continue
		}
		if password == "" {
			validationErrors.Details = append(validationErrors.Details, fmt.Sprintf("第 %d 行初始密码不能为空", rowNumber))
			continue
		}
		if prevRow, exists := seenUsernames[username]; exists {
			validationErrors.Details = append(validationErrors.Details, fmt.Sprintf("用户名 %q 在第 %d 行和第 %d 行重复", username, prevRow, rowNumber))
			continue
		}
		seenUsernames[username] = rowNumber

		rows = append(rows, BuiltinUserImportRow{
			Row:             rowNumber,
			Username:        username,
			InitialPassword: password,
			Roles:           parseRoleList(rolesRaw),
		})
	}

	if len(validationErrors.Details) > 0 {
		return nil, validationErrors
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("从工作表 %q 中未找到可导入的用户记录（表头位于第 %d 行）", builtinUsersImportSheetName, headerRowNumber)
	}
	if len(rows) > builtinUsersImportMaxRows {
		return nil, fmt.Errorf("单次最多导入 %d 个用户，当前为 %d", builtinUsersImportMaxRows, len(rows))
	}
	return rows, nil
}

func findHeaderColumn(columns map[int]string, expected string) (int, bool) {
	for index, name := range columns {
		if name == expected {
			return index, true
		}
	}
	return 0, false
}

func effectiveRowNumber(row worksheetRowXML, fallbackIndex int) int {
	if row.Number > 0 {
		return row.Number
	}
	return fallbackIndex + 1
}

func parseRoleList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	roles := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		role := strings.TrimSpace(part)
		if role == "" {
			continue
		}
		if _, exists := seen[role]; exists {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	return roles
}

func normalizeHeaderName(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func cellColumnIndex(ref string) (int, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, false
	}
	col := 0
	for _, ch := range ref {
		switch {
		case ch >= 'A' && ch <= 'Z':
			col = col*26 + int(ch-'A'+1)
		case ch >= 'a' && ch <= 'z':
			col = col*26 + int(ch-'a'+1)
		default:
			if col == 0 {
				return 0, false
			}
			return col - 1, true
		}
	}
	if col == 0 {
		return 0, false
	}
	return col - 1, true
}

func cellValue(cell worksheetCellXML, sharedStrings []string) string {
	switch cell.Type {
	case "inlineStr":
		return richTextValue(cell.InlineString.Text, cell.InlineString.Runs)
	case "s":
		index, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err != nil || index < 0 || index >= len(sharedStrings) {
			return ""
		}
		return sharedStrings[index]
	default:
		if strings.TrimSpace(cell.Value) != "" {
			return cell.Value
		}
		return richTextValue(cell.InlineString.Text, cell.InlineString.Runs)
	}
}

func richTextValue(text string, runs []richTextRunXML) string {
	if len(runs) == 0 {
		return text
	}
	var builder strings.Builder
	for _, run := range runs {
		builder.WriteString(run.Text)
	}
	return builder.String()
}

func parseSharedStrings(data []byte) ([]string, error) {
	var shared sharedStringsXML
	if err := xml.Unmarshal(data, &shared); err != nil {
		return nil, err
	}
	values := make([]string, 0, len(shared.Items))
	for _, item := range shared.Items {
		values = append(values, richTextValue(item.Text, item.Runs))
	}
	return values, nil
}

func readZipEntry(reader *zip.Reader, name string) ([]byte, error) {
	const maxEntrySize = 2 << 20
	target := path.Clean(strings.TrimPrefix(name, "/"))
	for _, file := range reader.File {
		if path.Clean(file.Name) != target {
			continue
		}
		if file.UncompressedSize64 > maxEntrySize {
			return nil, fmt.Errorf("压缩包条目 %s 过大", name)
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxEntrySize+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxEntrySize {
			return nil, fmt.Errorf("压缩包条目 %s 过大", name)
		}
		return data, nil
	}
	return nil, fmt.Errorf("缺少条目 %s", name)
}

type workbookXML struct {
	Sheets []workbookSheetXML `xml:"sheets>sheet"`
}

type workbookSheetXML struct {
	Name  string `xml:"name,attr"`
	RelID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
}

type workbookRelsXML struct {
	Relationships []workbookRelationshipXML `xml:"Relationship"`
}

type workbookRelationshipXML struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
}

type worksheetXML struct {
	Rows []worksheetRowXML `xml:"sheetData>row"`
}

type worksheetRowXML struct {
	Number int                `xml:"r,attr"`
	Cells  []worksheetCellXML `xml:"c"`
}

type worksheetCellXML struct {
	Ref          string          `xml:"r,attr"`
	Type         string          `xml:"t,attr"`
	Value        string          `xml:"v"`
	InlineString inlineStringXML `xml:"is"`
}

type inlineStringXML struct {
	Text string           `xml:"t"`
	Runs []richTextRunXML `xml:"r"`
}

type richTextRunXML struct {
	Text string `xml:"t"`
}

type sharedStringsXML struct {
	Items []sharedStringItemXML `xml:"si"`
}

type sharedStringItemXML struct {
	Text string           `xml:"t"`
	Runs []richTextRunXML `xml:"r"`
}
