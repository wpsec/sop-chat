package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sop-chat/internal/auth"
	"sop-chat/internal/config"

	"github.com/gin-gonic/gin"
)

type setupStatusTestStore struct {
	users []*auth.StoredUser
}

func (s *setupStatusTestStore) CreateUser(username, password, email string) error {
	return nil
}

func (s *setupStatusTestStore) GetUser(username string) (*auth.StoredUser, error) {
	for _, user := range s.users {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, nil
}

func (s *setupStatusTestStore) UpdateUser(username string, updates map[string]interface{}) error {
	return nil
}

func (s *setupStatusTestStore) DeleteUser(username string) error {
	return nil
}

func (s *setupStatusTestStore) ListUsers() ([]*auth.StoredUser, error) {
	return s.users, nil
}

func (s *setupStatusTestStore) ValidatePassword(username, password string) (bool, error) {
	return true, nil
}

func TestHandleGetSetupStatusReportsLoginReadySeparately(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := &Server{
		globalConfig: &config.Config{
			Auth: config.AuthConfig{
				Builtin: &config.BuiltinAuthConfig{
					Storage:    "sqlite",
					SQLitePath: "data/builtin-users.db",
				},
			},
		},
		authModes: []auth.AuthMode{auth.AuthModeBuiltin},
		userStore: &setupStatusTestStore{
			users: []*auth.StoredUser{
				{Username: "sqlite.admin"},
			},
		},
	}

	router := gin.New()
	router.GET("/api/system/setup-status", server.handleGetSetupStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/system/setup-status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Configured       bool   `json:"configured"`
		LoginReady       bool   `json:"loginReady"`
		CredConfigured   bool   `json:"credConfigured"`
		BuiltinStorage   string `json:"builtinStorage"`
		BuiltinUserCount int    `json:"builtinUserCount"`
		BuiltinAvailable bool   `json:"builtinAvailable"`
		Version          string `json:"version"`
		VersionDisplay   string `json:"versionDisplay"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.LoginReady {
		t.Fatalf("expected loginReady=true, got %+v", resp)
	}
	if resp.Configured {
		t.Fatalf("expected configured=false without cloud credentials, got %+v", resp)
	}
	if resp.CredConfigured {
		t.Fatalf("expected credConfigured=false, got %+v", resp)
	}
	if !resp.BuiltinAvailable || resp.BuiltinStorage != "sqlite" || resp.BuiltinUserCount != 1 {
		t.Fatalf("unexpected builtin status: %+v", resp)
	}
	if resp.Version != "v0.3.0-beta.2" {
		t.Fatalf("expected base version to be returned, got %+v", resp)
	}
	if resp.VersionDisplay == "" {
		t.Fatalf("expected version display to be returned, got %+v", resp)
	}
}
