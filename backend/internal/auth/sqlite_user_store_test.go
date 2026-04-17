package auth

import (
	"path/filepath"
	"testing"
)

func TestSQLiteUserStoreCreateValidateAndList(t *testing.T) {
	store, err := NewSQLiteUserStore(filepath.Join(t.TempDir(), "builtin-users.db"), "salt-123")
	if err != nil {
		t.Fatalf("NewSQLiteUserStore returned error: %v", err)
	}
	defer store.Close()

	if err := store.CreateUser("admin.local", "Strong@123456", "admin@example.com"); err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if err := store.UpdateUser("admin.local", map[string]interface{}{"roles": []string{"admin", "ops"}}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}

	ok, err := store.ValidatePassword("admin.local", "Strong@123456")
	if err != nil {
		t.Fatalf("ValidatePassword returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected password validation to succeed")
	}

	user, err := store.GetUser("admin.local")
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if len(user.Roles) != 2 || user.Roles[0] != "admin" || user.Roles[1] != "ops" {
		t.Fatalf("unexpected user roles: %+v", user)
	}

	roles, err := store.ListRoles()
	if err != nil {
		t.Fatalf("ListRoles returned error: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %+v", roles)
	}
}

func TestSQLiteUserStoreApplyImport(t *testing.T) {
	store, err := NewSQLiteUserStore(filepath.Join(t.TempDir(), "builtin-users.db"), "salt-123")
	if err != nil {
		t.Fatalf("NewSQLiteUserStore returned error: %v", err)
	}
	defer store.Close()

	report, err := store.ApplyImport([]BuiltinUserImportRow{
		{Row: 2, Username: "ops.user", InitialPassword: "Pass@123456", Roles: []string{"ops"}},
		{Row: 3, Username: "audit.user", InitialPassword: "Pass@654321", Roles: []string{"audit"}},
	}, BuiltinUserImportModeAppend)
	if err != nil {
		t.Fatalf("ApplyImport append returned error: %v", err)
	}
	if report.CreatedUsers != 2 || report.CreatedRoles != 2 {
		t.Fatalf("unexpected append report: %+v", report)
	}

	report, err = store.ApplyImport([]BuiltinUserImportRow{
		{Row: 2, Username: "ops.user", InitialPassword: "Updated@123456", Roles: []string{"admin"}},
	}, BuiltinUserImportModeOverwrite)
	if err != nil {
		t.Fatalf("ApplyImport overwrite returned error: %v", err)
	}
	if report.UpdatedUsers != 1 {
		t.Fatalf("unexpected overwrite report: %+v", report)
	}

	user, err := store.GetUser("ops.user")
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if len(user.Roles) != 1 || user.Roles[0] != "admin" {
		t.Fatalf("expected overwritten roles to be applied, got %+v", user)
	}
}
