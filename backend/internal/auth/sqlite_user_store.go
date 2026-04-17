package auth

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sop-chat/internal/config"

	_ "modernc.org/sqlite"
)

// SQLiteUserStore 基于 SQLite 的 builtin 用户存储。
type SQLiteUserStore struct {
	db           *sql.DB
	path         string
	passwordSalt string
}

// NewSQLiteUserStore 创建 SQLite 用户存储，并自动初始化表结构。
func NewSQLiteUserStore(sqlitePath, passwordSalt string) (*SQLiteUserStore, error) {
	if strings.TrimSpace(sqlitePath) == "" {
		return nil, fmt.Errorf("sqlite 路径不能为空")
	}

	cleanPath := filepath.Clean(strings.TrimSpace(sqlitePath))
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0755); err != nil {
		return nil, fmt.Errorf("创建 sqlite 目录失败: %w", err)
	}

	db, err := sql.Open("sqlite", cleanPath)
	if err != nil {
		return nil, fmt.Errorf("打开 sqlite 失败: %w", err)
	}

	store := &SQLiteUserStore{
		db:           db,
		path:         cleanPath,
		passwordSalt: passwordSalt,
	}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteUserStore) initSchema() error {
	stmts := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS builtin_users (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS builtin_roles (
			name TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS builtin_user_roles (
			username TEXT NOT NULL,
			role_name TEXT NOT NULL,
			PRIMARY KEY (username, role_name),
			FOREIGN KEY (username) REFERENCES builtin_users(username) ON DELETE CASCADE,
			FOREIGN KEY (role_name) REFERENCES builtin_roles(name) ON DELETE CASCADE
		);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("初始化 sqlite 表结构失败: %w", err)
		}
	}
	return nil
}

// Path 返回 sqlite 文件路径。
func (s *SQLiteUserStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Close 关闭底层数据库连接。
func (s *SQLiteUserStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// CreateUser 创建用户，password 传入明文密码。
func (s *SQLiteUserStore) CreateUser(username, password, email string) error {
	now := getCurrentTime()
	_, err := s.db.Exec(
		`INSERT INTO builtin_users(username, password_hash, email, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		username,
		HashPassword(s.passwordSalt, password),
		strings.TrimSpace(email),
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("创建 sqlite 用户失败: %w", err)
	}
	return nil
}

// GetUser 获取用户。
func (s *SQLiteUserStore) GetUser(username string) (*StoredUser, error) {
	row := s.db.QueryRow(`SELECT username, password_hash, email, created_at, updated_at FROM builtin_users WHERE username = ?`, username)

	user := &StoredUser{}
	if err := row.Scan(&user.Username, &user.PasswordHash, &user.Email, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("用户 %s 不存在", username)
		}
		return nil, fmt.Errorf("读取 sqlite 用户失败: %w", err)
	}

	roles, err := s.loadUserRoles(username)
	if err != nil {
		return nil, err
	}
	user.Roles = roles
	return user, nil
}

// UpdateUser 更新用户信息。
func (s *SQLiteUserStore) UpdateUser(username string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	email := ""
	if value, ok := updates["email"].(string); ok {
		email = strings.TrimSpace(value)
	}

	passwordHash := ""
	if value, ok := updates["passwordHash"].(string); ok {
		passwordHash = strings.TrimSpace(value)
	}
	if passwordHash == "" {
		if value, ok := updates["password"].(string); ok && strings.TrimSpace(value) != "" {
			passwordHash = HashPassword(s.passwordSalt, value)
		}
	}

	if email != "" || passwordHash != "" {
		queryParts := make([]string, 0, 3)
		args := make([]interface{}, 0, 4)
		if email != "" {
			queryParts = append(queryParts, "email = ?")
			args = append(args, email)
		}
		if passwordHash != "" {
			queryParts = append(queryParts, "password_hash = ?")
			args = append(args, passwordHash)
		}
		queryParts = append(queryParts, "updated_at = ?")
		args = append(args, getCurrentTime(), username)
		if _, err := tx.Exec(`UPDATE builtin_users SET `+strings.Join(queryParts, ", ")+` WHERE username = ?`, args...); err != nil {
			return fmt.Errorf("更新 sqlite 用户失败: %w", err)
		}
	}

	if rawRoles, ok := updates["roles"]; ok {
		roles, convErr := normalizeStoredRoles(rawRoles)
		if convErr != nil {
			return convErr
		}
		if err := s.replaceUserRolesTx(tx, username, roles); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交更新事务失败: %w", err)
	}
	return nil
}

// DeleteUser 删除用户。
func (s *SQLiteUserStore) DeleteUser(username string) error {
	if _, err := s.db.Exec(`DELETE FROM builtin_users WHERE username = ?`, username); err != nil {
		return fmt.Errorf("删除 sqlite 用户失败: %w", err)
	}
	return nil
}

// ListUsers 列出所有用户。
func (s *SQLiteUserStore) ListUsers() ([]*StoredUser, error) {
	rows, err := s.db.Query(`SELECT username, password_hash, email, created_at, updated_at FROM builtin_users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("列出 sqlite 用户失败: %w", err)
	}
	defer rows.Close()

	roleMap, err := s.loadAllRoleAssignments()
	if err != nil {
		return nil, err
	}

	users := make([]*StoredUser, 0)
	for rows.Next() {
		user := &StoredUser{}
		if err := rows.Scan(&user.Username, &user.PasswordHash, &user.Email, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取 sqlite 用户失败: %w", err)
		}
		user.Roles = defaultBuiltinRoles(roleMap[user.Username])
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 sqlite 用户失败: %w", err)
	}
	return users, nil
}

// ValidatePassword 校验密码。
func (s *SQLiteUserStore) ValidatePassword(username, password string) (bool, error) {
	user, err := s.GetUser(username)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(user.PasswordHash, HashPassword(s.passwordSalt, password)), nil
}

// ListRoles 列出所有角色及其成员。
func (s *SQLiteUserStore) ListRoles() ([]*StoredRole, error) {
	rows, err := s.db.Query(`SELECT name, created_at, updated_at FROM builtin_roles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("列出 sqlite 角色失败: %w", err)
	}
	defer rows.Close()

	assignments, err := s.loadRoleUsers()
	if err != nil {
		return nil, err
	}

	roles := make([]*StoredRole, 0)
	for rows.Next() {
		role := &StoredRole{}
		if err := rows.Scan(&role.Name, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取 sqlite 角色失败: %w", err)
		}
		role.Users = append([]string(nil), assignments[role.Name]...)
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 sqlite 角色失败: %w", err)
	}
	return roles, nil
}

// ReplaceAll 用给定用户和角色全集替换 sqlite 中的 builtin 数据。
func (s *SQLiteUserStore) ReplaceAll(users []*StoredUser, roles []*StoredRole) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		`DELETE FROM builtin_user_roles`,
		`DELETE FROM builtin_users`,
		`DELETE FROM builtin_roles`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("清空 sqlite builtin 数据失败: %w", err)
		}
	}

	if err := insertBuiltinUsersAndRolesTx(tx, users, roles); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 sqlite 替换事务失败: %w", err)
	}
	return nil
}

// ApplyImport 将 Excel 导入记录写入 sqlite。
func (s *SQLiteUserStore) ApplyImport(rows []BuiltinUserImportRow, mode BuiltinUserImportMode) (*BuiltinUserImportReport, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("没有可导入的用户记录")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	report := &BuiltinUserImportReport{
		Mode:      mode,
		TotalRows: len(rows),
	}

	existingUsers, err := s.listUsersTx(tx)
	if err != nil {
		return nil, err
	}
	existingRoles, err := s.listRolesTx(tx)
	if err != nil {
		return nil, err
	}

	userIndex := make(map[string]*StoredUser, len(existingUsers))
	for _, user := range existingUsers {
		userIndex[user.Username] = user
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

	roleSet := make(map[string]struct{}, len(existingRoles))
	for _, role := range existingRoles {
		roleSet[role.Name] = struct{}{}
	}

	for _, row := range rows {
		now := getCurrentTime()
		email := fmt.Sprintf("%s@localhost", row.Username)
		if existing, exists := userIndex[row.Username]; exists {
			if strings.TrimSpace(existing.Email) != "" {
				email = existing.Email
			}
			if mode == BuiltinUserImportModeOverwrite {
				if _, err := tx.Exec(`DELETE FROM builtin_user_roles WHERE username = ?`, row.Username); err != nil {
					return nil, fmt.Errorf("清理用户旧角色失败: %w", err)
				}
			}
			if _, err := tx.Exec(
				`UPDATE builtin_users SET password_hash = ?, email = ?, updated_at = ? WHERE username = ?`,
				HashPassword(s.passwordSalt, row.InitialPassword),
				email,
				now,
				row.Username,
			); err != nil {
				return nil, fmt.Errorf("更新 sqlite 用户失败: %w", err)
			}
			report.UpdatedUsers++
		} else {
			if _, err := tx.Exec(
				`INSERT INTO builtin_users(username, password_hash, email, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
				row.Username,
				HashPassword(s.passwordSalt, row.InitialPassword),
				email,
				now,
				now,
			); err != nil {
				return nil, fmt.Errorf("创建 sqlite 用户失败: %w", err)
			}
			userIndex[row.Username] = &StoredUser{Username: row.Username, Email: email, CreatedAt: now, UpdatedAt: now}
			report.CreatedUsers++
		}

		for _, roleName := range row.Roles {
			if _, exists := roleSet[roleName]; !exists {
				if _, err := tx.Exec(
					`INSERT INTO builtin_roles(name, created_at, updated_at) VALUES(?, ?, ?)`,
					roleName,
					now,
					now,
				); err != nil {
					return nil, fmt.Errorf("创建 sqlite 角色失败: %w", err)
				}
				roleSet[roleName] = struct{}{}
				report.CreatedRoles++
			}
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO builtin_user_roles(username, role_name) VALUES(?, ?)`,
				row.Username,
				roleName,
			); err != nil {
				return nil, fmt.Errorf("写入 sqlite 用户角色关系失败: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交导入事务失败: %w", err)
	}
	return report, nil
}

// IsEmpty 返回 sqlite 中是否没有 builtin 用户和角色。
func (s *SQLiteUserStore) IsEmpty() (bool, error) {
	var usersCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM builtin_users`).Scan(&usersCount); err != nil {
		return false, fmt.Errorf("统计 sqlite 用户失败: %w", err)
	}
	var rolesCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM builtin_roles`).Scan(&rolesCount); err != nil {
		return false, fmt.Errorf("统计 sqlite 角色失败: %w", err)
	}
	return usersCount == 0 && rolesCount == 0, nil
}

// BootstrapFromConfig 将配置文件中的 builtin 用户迁移到 sqlite。
func (s *SQLiteUserStore) BootstrapFromConfig(users []config.UserConfig, roles []config.RoleConfig) error {
	storedUsers, storedRoles := BuildStoredBuiltinData(users, roles)
	return s.ReplaceAll(storedUsers, storedRoles)
}

// BuildStoredBuiltinData 将配置文件中的 builtin 用户与角色转换为统一存储结构。
func BuildStoredBuiltinData(users []config.UserConfig, roles []config.RoleConfig) ([]*StoredUser, []*StoredRole) {
	roleUsers := make(map[string][]string, len(roles))
	userRoles := make(map[string][]string, len(users))
	storedRoles := make([]*StoredRole, 0, len(roles))
	now := getCurrentTime()

	for _, role := range roles {
		cleanUsers := dedupeAndSortStrings(role.Users)
		roleUsers[role.Name] = cleanUsers
		storedRoles = append(storedRoles, &StoredRole{
			Name:      role.Name,
			Users:     cleanUsers,
			CreatedAt: now,
			UpdatedAt: now,
		})
		for _, username := range cleanUsers {
			userRoles[username] = append(userRoles[username], role.Name)
		}
	}

	storedUsers := make([]*StoredUser, 0, len(users))
	for _, user := range users {
		rolesForUser := dedupeAndSortStrings(userRoles[user.Name])
		storedUsers = append(storedUsers, &StoredUser{
			Username:     user.Name,
			PasswordHash: user.Password,
			Email:        fmt.Sprintf("%s@localhost", user.Name),
			Roles:        defaultBuiltinRoles(rolesForUser),
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}

	return storedUsers, storedRoles
}

func insertBuiltinUsersAndRolesTx(tx *sql.Tx, users []*StoredUser, roles []*StoredRole) error {
	now := getCurrentTime()
	userSet := make(map[string]struct{}, len(users))
	for _, role := range roles {
		if role == nil || strings.TrimSpace(role.Name) == "" {
			continue
		}
		createdAt := role.CreatedAt
		if createdAt == "" {
			createdAt = now
		}
		updatedAt := role.UpdatedAt
		if updatedAt == "" {
			updatedAt = now
		}
		if _, err := tx.Exec(
			`INSERT INTO builtin_roles(name, created_at, updated_at) VALUES(?, ?, ?)`,
			role.Name,
			createdAt,
			updatedAt,
		); err != nil {
			return fmt.Errorf("写入 sqlite 角色失败: %w", err)
		}
	}

	for _, user := range users {
		if user == nil || strings.TrimSpace(user.Username) == "" {
			continue
		}
		userSet[user.Username] = struct{}{}
		createdAt := user.CreatedAt
		if createdAt == "" {
			createdAt = now
		}
		updatedAt := user.UpdatedAt
		if updatedAt == "" {
			updatedAt = now
		}
		if _, err := tx.Exec(
			`INSERT INTO builtin_users(username, password_hash, email, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
			user.Username,
			user.PasswordHash,
			user.Email,
			createdAt,
			updatedAt,
		); err != nil {
			return fmt.Errorf("写入 sqlite 用户失败: %w", err)
		}
		for _, roleName := range dedupeAndSortStrings(user.Roles) {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO builtin_roles(name, created_at, updated_at) VALUES(?, ?, ?)`, roleName, now, now); err != nil {
				return fmt.Errorf("补写 sqlite 角色失败: %w", err)
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO builtin_user_roles(username, role_name) VALUES(?, ?)`, user.Username, roleName); err != nil {
				return fmt.Errorf("写入 sqlite 用户角色关系失败: %w", err)
			}
		}
	}

	for _, role := range roles {
		if role == nil || strings.TrimSpace(role.Name) == "" {
			continue
		}
		for _, username := range dedupeAndSortStrings(role.Users) {
			if _, exists := userSet[username]; !exists {
				continue
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO builtin_user_roles(username, role_name) VALUES(?, ?)`, username, role.Name); err != nil {
				return fmt.Errorf("写入 sqlite 角色成员失败: %w", err)
			}
		}
	}
	return nil
}

func (s *SQLiteUserStore) replaceUserRolesTx(tx *sql.Tx, username string, roles []string) error {
	if _, err := tx.Exec(`DELETE FROM builtin_user_roles WHERE username = ?`, username); err != nil {
		return fmt.Errorf("清理 sqlite 用户角色失败: %w", err)
	}
	now := getCurrentTime()
	for _, roleName := range dedupeAndSortStrings(roles) {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO builtin_roles(name, created_at, updated_at) VALUES(?, ?, ?)`, roleName, now, now); err != nil {
			return fmt.Errorf("写入 sqlite 角色失败: %w", err)
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO builtin_user_roles(username, role_name) VALUES(?, ?)`, username, roleName); err != nil {
			return fmt.Errorf("写入 sqlite 用户角色关系失败: %w", err)
		}
	}
	return nil
}

func (s *SQLiteUserStore) loadUserRoles(username string) ([]string, error) {
	rows, err := s.db.Query(`SELECT role_name FROM builtin_user_roles WHERE username = ? ORDER BY role_name`, username)
	if err != nil {
		return nil, fmt.Errorf("读取 sqlite 用户角色失败: %w", err)
	}
	defer rows.Close()

	roles := make([]string, 0)
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("读取 sqlite 用户角色失败: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 sqlite 用户角色失败: %w", err)
	}
	return defaultBuiltinRoles(roles), nil
}

func (s *SQLiteUserStore) loadAllRoleAssignments() (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT username, role_name FROM builtin_user_roles ORDER BY username, role_name`)
	if err != nil {
		return nil, fmt.Errorf("读取 sqlite 用户角色关系失败: %w", err)
	}
	defer rows.Close()

	roleMap := make(map[string][]string)
	for rows.Next() {
		var username, roleName string
		if err := rows.Scan(&username, &roleName); err != nil {
			return nil, fmt.Errorf("读取 sqlite 用户角色关系失败: %w", err)
		}
		roleMap[username] = append(roleMap[username], roleName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 sqlite 用户角色关系失败: %w", err)
	}
	for username := range roleMap {
		roleMap[username] = defaultBuiltinRoles(roleMap[username])
	}
	return roleMap, nil
}

func (s *SQLiteUserStore) loadRoleUsers() (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT role_name, username FROM builtin_user_roles ORDER BY role_name, username`)
	if err != nil {
		return nil, fmt.Errorf("读取 sqlite 角色成员失败: %w", err)
	}
	defer rows.Close()

	assignments := make(map[string][]string)
	for rows.Next() {
		var roleName, username string
		if err := rows.Scan(&roleName, &username); err != nil {
			return nil, fmt.Errorf("读取 sqlite 角色成员失败: %w", err)
		}
		assignments[roleName] = append(assignments[roleName], username)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 sqlite 角色成员失败: %w", err)
	}
	for roleName := range assignments {
		assignments[roleName] = dedupeAndSortStrings(assignments[roleName])
	}
	return assignments, nil
}

func (s *SQLiteUserStore) listUsersTx(tx *sql.Tx) ([]*StoredUser, error) {
	rows, err := tx.Query(`SELECT username, password_hash, email, created_at, updated_at FROM builtin_users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("列出 sqlite 用户失败: %w", err)
	}
	defer rows.Close()

	users := make([]*StoredUser, 0)
	for rows.Next() {
		user := &StoredUser{}
		if err := rows.Scan(&user.Username, &user.PasswordHash, &user.Email, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取 sqlite 用户失败: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 sqlite 用户失败: %w", err)
	}
	return users, nil
}

func (s *SQLiteUserStore) listRolesTx(tx *sql.Tx) ([]*StoredRole, error) {
	rows, err := tx.Query(`SELECT name, created_at, updated_at FROM builtin_roles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("列出 sqlite 角色失败: %w", err)
	}
	defer rows.Close()

	roles := make([]*StoredRole, 0)
	for rows.Next() {
		role := &StoredRole{}
		if err := rows.Scan(&role.Name, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取 sqlite 角色失败: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 sqlite 角色失败: %w", err)
	}
	return roles, nil
}

func normalizeStoredRoles(raw interface{}) ([]string, error) {
	switch value := raw.(type) {
	case []string:
		return value, nil
	case []interface{}:
		result := make([]string, 0, len(value))
		for _, item := range value {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("roles 字段包含非字符串值")
			}
			result = append(result, str)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("不支持的 roles 更新类型")
	}
}

func defaultBuiltinRoles(roles []string) []string {
	clean := dedupeAndSortStrings(roles)
	if len(clean) == 0 {
		return []string{"user"}
	}
	return clean
}

func dedupeAndSortStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
