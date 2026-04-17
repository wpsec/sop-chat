package dingtalk

import (
	"context"
	"testing"
	"time"
)

func TestIsCancelCommand(t *testing.T) {
	testCases := []struct {
		input string
		want  bool
	}{
		{input: "/取消", want: true},
		{input: " /停止 ", want: true},
		{input: "/ABORT", want: true},
		{input: "/继续", want: false},
		{input: "取消", want: false},
	}

	for _, tc := range testCases {
		if got := isCancelCommand(tc.input); got != tc.want {
			t.Fatalf("isCancelCommand(%q)=%v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestConversationTaskKeyPrefersStableSenderIdentity(t *testing.T) {
	keyA := conversationTaskKey("conv-1", "sender-id", "", "昵称A")
	keyB := conversationTaskKey("conv-1", "sender-id", "", "昵称B")
	if keyA != keyB {
		t.Fatalf("expected senderId to keep task key stable, got %q != %q", keyA, keyB)
	}

	staffKeyA := conversationTaskKey("conv-1", "", "staff-id", "昵称A")
	staffKeyB := conversationTaskKey("conv-1", "", "staff-id", "昵称B")
	if staffKeyA != staffKeyB {
		t.Fatalf("expected senderStaffId fallback to keep task key stable, got %q != %q", staffKeyA, staffKeyB)
	}
}

func TestCancelRunningTask(t *testing.T) {
	bot := &Bot{}
	ctx, cancel := context.WithCancel(context.Background())
	task := bot.registerRunningTask("task-key", cancel)
	defer bot.unregisterRunningTask("task-key", task)

	if !bot.cancelRunningTask("task-key") {
		t.Fatalf("expected running task to be cancelled")
	}

	select {
	case <-ctx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected cancel function to be invoked")
	}

	bot.unregisterRunningTask("task-key", task)
	if bot.cancelRunningTask("task-key") {
		t.Fatalf("expected missing task to return false")
	}
}
