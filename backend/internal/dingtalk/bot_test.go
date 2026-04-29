package dingtalk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
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

func TestFinishEmployeeStreamRejectsEmptyText(t *testing.T) {
	replyText, threadID, err := finishEmployeeStream(nil, "thread-1", chatStreamDiagnostics{
		requestID:     "req-1",
		responseCount: 1,
		messageCount:  1,
		done:          true,
	})
	if !errors.Is(err, errEmptyEmployeeReply) {
		t.Fatalf("expected empty reply error, got %v", err)
	}
	if replyText != "" {
		t.Fatalf("expected empty reply text, got %q", replyText)
	}
	if threadID != "thread-1" {
		t.Fatalf("expected thread id to be preserved, got %q", threadID)
	}
}

func TestFinishEmployeeStreamKeepsText(t *testing.T) {
	replyText, threadID, err := finishEmployeeStream([]string{"结论", "正常"}, "thread-1", chatStreamDiagnostics{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if replyText != "结论正常" {
		t.Fatalf("unexpected reply text %q", replyText)
	}
	if threadID != "thread-1" {
		t.Fatalf("expected thread id to be preserved, got %q", threadID)
	}
}

func TestChatResponseErrorFromStatusDetail(t *testing.T) {
	response := &cmsclient.CreateChatResponse{
		StatusCode: tea.Int32(500),
		Body: &cmsclient.CreateChatResponseBody{
			Messages: []*cmsclient.CreateChatResponseBodyMessages{
				{Detail: tea.String("backend failed")},
			},
		},
	}

	err := chatResponseError(response)
	if err == nil {
		t.Fatal("expected response error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "backend failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatResponseErrorFromMessageType(t *testing.T) {
	response := &cmsclient.CreateChatResponse{
		StatusCode: tea.Int32(200),
		Body: &cmsclient.CreateChatResponseBody{
			Messages: []*cmsclient.CreateChatResponseBodyMessages{
				{Type: tea.String("failed"), Detail: tea.String("tool failed")},
			},
		},
	}

	err := chatResponseError(response)
	if err == nil {
		t.Fatal("expected message error")
	}
	if err.Error() != "tool failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}
