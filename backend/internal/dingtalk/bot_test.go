package dingtalk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"

	"sop-chat/internal/config"
	"sop-chat/internal/session"
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

func TestEvictThreadForRouteDeletesCachedThread(t *testing.T) {
	bot := &Bot{threads: session.NewThreadStore("[test]")}
	route := resolvedRoute{
		employeeName:   "employee-1",
		cloudAccountID: "uat",
		project:        "project-1",
	}
	cacheKey := threadCacheKeyForRoute("conversation-1", "sender-1", route)
	bot.threads.Store(cacheKey, "thread-1")

	if got, ok := bot.threads.Load(cacheKey); !ok || got != "thread-1" {
		t.Fatalf("expected cached thread before eviction, got %q ok=%v", got, ok)
	}

	bot.evictThreadForRoute("conversation-1", "sender-1", route)
	if got, ok := bot.threads.Load(cacheKey); ok {
		t.Fatalf("expected cached thread to be evicted, got %q", got)
	}
}

func TestEvictConversationThreadsDeletesCloudAccountRouteThread(t *testing.T) {
	bot := &Bot{
		dtConfig: &config.DingTalkConfig{
			EmployeeName:   "default-employee",
			CloudAccountID: "default",
			CloudAccountRoutes: []config.CloudAccountRoute{
				{
					CloudAccountID: "uat",
					EmployeeName:   "employee-uat",
				},
			},
		},
		threads: session.NewThreadStore("[test]"),
	}
	route := resolvedRoute{
		employeeName:   "employee-uat",
		cloudAccountID: "uat",
	}
	cacheKey := threadCacheKeyForRoute("conversation-1", "sender-1", route)
	bot.threads.Store(cacheKey, "thread-uat")

	bot.evictConversationThreads("conversation-1", "sender-1")
	if got, ok := bot.threads.Load(cacheKey); ok {
		t.Fatalf("expected cloud account route thread to be evicted, got %q", got)
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

func TestDetectPostgreSQLAutoHandoff(t *testing.T) {
	reply := `
根因证据状态: 需联动
下一步路径: 联动 PostgreSQL
核心结论: HikariCP 返回 Connection is closed，需要数据库侧补证。
`
	decision, ok := detectPostgreSQLAutoHandoff(reply)
	if !ok {
		t.Fatalf("expected PostgreSQL auto handoff to be detected")
	}
	if decision.targetModule != "postgresql" {
		t.Fatalf("unexpected target module %q", decision.targetModule)
	}
}

func TestDetectPostgreSQLAutoHandoffRequiresPostgreSQLTarget(t *testing.T) {
	reply := `
根因证据状态: 需联动
下一步路径: 联动 gateway
`
	if _, ok := detectPostgreSQLAutoHandoff(reply); ok {
		t.Fatalf("expected non-PostgreSQL handoff to be ignored")
	}
}

func TestBuildPostgreSQLAutoHandoffPromptIsCompactAndActionable(t *testing.T) {
	decision := autoHandoffDecision{
		targetModule: "postgresql",
		reason:       "根因证据状态=需联动，目标模块=postgresql",
	}
	prompt := buildPostgreSQLAutoHandoffPrompt(
		"分析最近一个小时日志，pod=backend-abc",
		"根因证据状态: 需联动\n核心结论: HikariCP Connection is closed\n下一步路径: 联动 PostgreSQL",
		decision,
	)
	for _, want := range []string{
		"继续执行 PostgreSQL 模块",
		"postgresql_stat_activity_log",
		"分析链",
		"1200 字以内",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got %q", want, prompt)
		}
	}
}

func TestFormatAutoHandoffFinalReplyLimitsLength(t *testing.T) {
	longReply := strings.Repeat("数据库连接异常", 1000)
	got := formatAutoHandoffFinalReply(longReply)
	if !strings.HasPrefix(got, "已自动联动 PostgreSQL 补证。") {
		t.Fatalf("expected auto handoff prefix, got %q", got[:min(len(got), 40)])
	}
	if !strings.Contains(got, "自动联动报告已按钉钉阅读长度压缩") {
		t.Fatalf("expected compression notice")
	}
}
