package session

import "testing"

func TestThreadStoreDeleteEvictsCachedThread(t *testing.T) {
	store := NewThreadStore("[test]")
	store.Store("cache-key", "thread-1")

	if got, ok := store.Load("cache-key"); !ok || got != "thread-1" {
		t.Fatalf("expected cached thread before delete, got %q ok=%v", got, ok)
	}

	store.Delete("cache-key")
	if got, ok := store.Load("cache-key"); ok {
		t.Fatalf("expected cached thread to be deleted, got %q", got)
	}
}
