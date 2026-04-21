package sharetoken

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateAndValidate(t *testing.T) {
	manager := NewManager("unit-test-secret", DefaultExpiresIn)

	token, expiresAt, err := manager.Generate("assistant", "thread-123", "prod")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if token == "" {
		t.Fatal("expected token to be generated")
	}
	if expiresAt.IsZero() {
		t.Fatal("expected expiresAt to be set")
	}

	claims, err := manager.Validate(token)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if claims.EmployeeName != "assistant" || claims.ThreadID != "thread-123" || claims.CloudAccountID != "prod" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestValidateExpiredToken(t *testing.T) {
	manager := &Manager{
		secretKey: []byte("share:unit-test-secret"),
		expiresIn: -1 * time.Minute,
	}

	token, _, err := manager.Generate("assistant", "thread-123", "prod")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	_, err = manager.Validate(token)
	if err == nil {
		t.Fatal("expected Validate to return error for expired token")
	}
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestBuildURL(t *testing.T) {
	url := BuildURL("https://example.com/", "assistant", "thread-123", "token-value")
	if !strings.Contains(url, "#/share/assistant/thread-123") {
		t.Fatalf("unexpected share path: %s", url)
	}
	if !strings.Contains(url, QueryParam+"=token-value") {
		t.Fatalf("expected share token in url, got %s", url)
	}
}
