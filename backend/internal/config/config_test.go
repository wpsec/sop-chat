package config

import (
	"strings"
	"testing"
)

func TestResolveClientConfigUsesFirstCloudAccountWhenDefaultMissing(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			AccessKeyId:     "global-ak",
			AccessKeySecret: "global-sk",
			Endpoint:        "cms.cn-hangzhou.aliyuncs.com",
		},
		CloudAccounts: []CloudAccountConfig{
			{
				ID:              "prod",
				AccessKeyId:     "prod-ak",
				AccessKeySecret: "prod-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
			{
				ID:              "uat",
				AccessKeyId:     "uat-ak",
				AccessKeySecret: "uat-sk",
				Endpoint:        "cms.cn-beijing.aliyuncs.com",
			},
		},
	}

	resolved, err := cfg.ResolveClientConfig(DefaultCloudAccountID)
	if err != nil {
		t.Fatalf("ResolveClientConfig(default) returned error: %v", err)
	}

	if resolved.CloudAccountID != "prod" {
		t.Fatalf("expected first cloud account to be used, got %q", resolved.CloudAccountID)
	}
	if resolved.AccessKeyId != "prod-ak" {
		t.Fatalf("expected prod access key, got %q", resolved.AccessKeyId)
	}
	if resolved.AccessKeySecret != "prod-sk" {
		t.Fatalf("expected prod access secret, got %q", resolved.AccessKeySecret)
	}
	if resolved.Endpoint != "cms.cn-shanghai.aliyuncs.com" {
		t.Fatalf("expected prod endpoint, got %q", resolved.Endpoint)
	}
}

func TestResolveMessageCloudAccountIDMatchesAlias(t *testing.T) {
	cfg := &Config{
		CloudAccounts: []CloudAccountConfig{
			{
				ID:              "prod",
				Aliases:         []string{"prod", "生产环境", "subscription-prod"},
				AccessKeyId:     "prod-ak",
				AccessKeySecret: "prod-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
			{
				ID:              "uat",
				Aliases:         []string{"uat", "测试环境", "subscription-uat"},
				AccessKeyId:     "uat-ak",
				AccessKeySecret: "uat-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
		},
	}

	accountID, matched, ambiguous := cfg.ResolveMessageCloudAccountID("帮我看下 subscription-uat 的 WAF 告警", "prod")
	if !matched {
		t.Fatalf("expected alias to be matched")
	}
	if len(ambiguous) != 0 {
		t.Fatalf("expected no ambiguous results, got %v", ambiguous)
	}
	if accountID != "uat" {
		t.Fatalf("expected uat to be selected, got %q", accountID)
	}
}

func TestMatchCloudAccountIDsByTextDoesNotMatchInsideWords(t *testing.T) {
	cfg := &Config{
		CloudAccounts: []CloudAccountConfig{
			{
				ID:              "prod",
				Aliases:         []string{"prod"},
				AccessKeyId:     "prod-ak",
				AccessKeySecret: "prod-sk",
				Endpoint:        "cms.cn-shanghai.aliyuncs.com",
			},
		},
	}

	matches := cfg.MatchCloudAccountIDsByText("帮我看下 product 维度的监控", nil)
	if len(matches) != 0 {
		t.Fatalf("expected no matches for product text, got %v", matches)
	}

	matches = cfg.MatchCloudAccountIDsByText("帮我看下 prod 环境的监控", nil)
	if len(matches) != 1 || matches[0] != "prod" {
		t.Fatalf("expected prod to match as a standalone token, got %v", matches)
	}
}

func TestFindCloudAccountRouteMatchesNormalizedID(t *testing.T) {
	routes := []CloudAccountRoute{
		{
			CloudAccountID: "default",
			EmployeeName:   "employee-default",
		},
		{
			CloudAccountID: "uat",
			EmployeeName:   "employee-uat",
		},
	}

	route := FindCloudAccountRoute(routes, "uat")
	if route == nil {
		t.Fatalf("expected route to be found")
	}
	if route.EmployeeName != "employee-uat" {
		t.Fatalf("expected employee-uat, got %q", route.EmployeeName)
	}
}

func TestApplyCompatibilityDefaultsMigratesLegacyGlobal(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			Host:            "127.0.0.1",
			Port:            9090,
			TimeZone:        "Asia/Shanghai",
			Language:        "zh",
			AccessKeyId:     "legacy-ak",
			AccessKeySecret: "legacy-sk",
			Endpoint:        "cms.cn-shanghai.aliyuncs.com",
		},
	}

	cfg.applyCompatibilityDefaults()

	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 9090 {
		t.Fatalf("expected server settings to migrate from legacy global, got %+v", cfg.Server)
	}
	if len(cfg.CloudAccounts) != 1 {
		t.Fatalf("expected one migrated cloud account, got %d", len(cfg.CloudAccounts))
	}
	if cfg.CloudAccounts[0].ID != DefaultCloudAccountID {
		t.Fatalf("expected migrated default cloud account, got %q", cfg.CloudAccounts[0].ID)
	}
	if cfg.CloudAccounts[0].AccessKeyId != "legacy-ak" || cfg.CloudAccounts[0].Endpoint != "cms.cn-shanghai.aliyuncs.com" {
		t.Fatalf("unexpected migrated cloud account: %+v", cfg.CloudAccounts[0])
	}
}

func TestDingTalkProgressFeedbackEnabledDefaultsToTrue(t *testing.T) {
	enabled := true
	disabled := false

	if !(*DingTalkConfig)(nil).ProgressFeedbackEnabled() {
		t.Fatalf("expected nil dingtalk config to default progress feedback to true")
	}
	if !(&DingTalkConfig{}).ProgressFeedbackEnabled() {
		t.Fatalf("expected missing progressFeedback to default to true")
	}
	if !(&DingTalkConfig{ProgressFeedback: &enabled}).ProgressFeedbackEnabled() {
		t.Fatalf("expected explicit true progressFeedback to stay enabled")
	}
	if (&DingTalkConfig{ProgressFeedback: &disabled}).ProgressFeedbackEnabled() {
		t.Fatalf("expected explicit false progressFeedback to disable stage feedback")
	}
}

func TestResolveProductUsesWorkspaceAndProjectHints(t *testing.T) {
	if got := ResolveProduct("", "", "workspace-a"); got != "cms" {
		t.Fatalf("expected workspace to imply cms, got %q", got)
	}
	if got := ResolveProduct("", "project-a", ""); got != "sls" {
		t.Fatalf("expected project to imply sls, got %q", got)
	}
	if got := ResolveProduct("cms", "", ""); got != "cms" {
		t.Fatalf("expected explicit cms to be kept, got %q", got)
	}
}

func TestApplyReplyStyleInstruction(t *testing.T) {
	full := ApplyReplyStyleInstruction("请分析今天的告警", false, "sls")
	if full == "请分析今天的告警" {
		t.Fatalf("expected full SOP instruction to be appended for sls when conciseReply=false")
	}
	if !strings.Contains(full, "当前没有足够依据确认") {
		t.Fatalf("expected anti-hallucination instruction to be appended, got %q", full)
	}
	if !strings.Contains(full, "SOP") {
		t.Fatalf("expected SOP guidance in full reply instruction, got %q", full)
	}
	if !strings.Contains(full, "结论 / 依据 / 不确定项 / 下一步建议") {
		t.Fatalf("expected high-risk structured instruction, got %q", full)
	}
	if !strings.Contains(full, "Markdown 表格必须独立成段") {
		t.Fatalf("expected report markdown formatting instruction, got %q", full)
	}

	concise := ApplyReplyStyleInstruction("请分析今天的告警", true, "sls")
	if !strings.Contains(concise, "简洁") {
		t.Fatalf("expected concise instruction to be appended, got %q", concise)
	}
	if !strings.Contains(concise, ConciseSOPReplyInstruction) {
		t.Fatalf("expected SLS concise mode to preserve full SOP execution, got %q", concise)
	}
	if !strings.Contains(concise, "简洁只影响最终表达") {
		t.Fatalf("expected concise SOP instruction to say concise output must not reduce checks, got %q", concise)
	}
	if !strings.Contains(concise, "结论 / 依据 / 不确定项 / 下一步建议") {
		t.Fatalf("expected concise high-risk instruction to be appended, got %q", concise)
	}

	cms := ApplyReplyStyleInstruction("请分析今天的告警", false, "cms")
	if cms == "请分析今天的告警" {
		t.Fatalf("expected cms non-concise message to include anti-hallucination instruction, got %q", cms)
	}
}

func TestApplyReplyStyleInstructionUsesPerMessageReplyStyleDirective(t *testing.T) {
	concise := ApplyReplyStyleInstruction("简洁回复：请分析今天的告警", false, "sls")
	if !strings.Contains(concise, ConciseReplyInstruction) {
		t.Fatalf("expected concise directive to enable concise reply, got %q", concise)
	}
	if !strings.Contains(concise, ConciseSOPReplyInstruction) {
		t.Fatalf("expected concise directive to keep SLS SOP execution constraints, got %q", concise)
	}
	if strings.Contains(concise, StandardSOPReplyInstruction) {
		t.Fatalf("expected concise directive not to append full SOP instruction, got %q", concise)
	}
	if strings.Contains(concise, ReportMarkdownFormattingInstruction) {
		t.Fatalf("expected concise directive not to append markdown report formatting instruction, got %q", concise)
	}

	full := ApplyReplyStyleInstruction("请详细分析今天的告警", true, "sls")
	if strings.Contains(full, ConciseReplyInstruction) {
		t.Fatalf("expected full directive to disable concise reply, got %q", full)
	}
	if !strings.Contains(full, StandardSOPReplyInstruction) {
		t.Fatalf("expected full directive to append SOP instruction, got %q", full)
	}
	if !strings.Contains(full, ReportMarkdownFormattingInstruction) {
		t.Fatalf("expected full directive to append report formatting instruction, got %q", full)
	}
}

func TestApplyReplyStyleInstructionFullDirectiveWinsConflict(t *testing.T) {
	got := ApplyReplyStyleInstruction("简洁回复，但请完整分析今天的告警", false, "sls")
	if strings.Contains(got, ConciseReplyInstruction) {
		t.Fatalf("expected full directive to win over concise directive, got %q", got)
	}
	if !strings.Contains(got, StandardSOPReplyInstruction) {
		t.Fatalf("expected full directive to append SOP instruction, got %q", got)
	}
}

func TestApplyReplyStyleInstructionWithSourceIgnoresEnrichedPromptDirectives(t *testing.T) {
	got := ApplyReplyStyleInstructionWithSource(
		"请分析今天的告警\n\n本地 SOP 提示：详细分析候选模块。",
		"请分析今天的告警",
		true,
		"sls",
	)
	if !strings.Contains(got, ConciseReplyInstruction) {
		t.Fatalf("expected default concise setting to remain effective, got %q", got)
	}
	if !strings.Contains(got, ConciseSOPReplyInstruction) {
		t.Fatalf("expected default concise SLS mode to keep SOP execution constraints, got %q", got)
	}
}

func TestSLSConciseReplyDoesNotPermitSkippingWAFEvidence(t *testing.T) {
	got := ApplyReplyStyleInstruction("最近24小时，uat环境waf告警情况，简要回复", false, "sls")
	for _, want := range []string{
		"简洁只影响最终表达",
		"不允许减少 SOP 查询步骤",
		"未查询 / 查询失败",
		"action、rule_id/attack_type、白名单、bypass_matched_ids",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected SLS concise WAF prompt to contain %q, got %q", want, got)
		}
	}
}

func TestApplyReplyStyleInstructionDoesNotForceHighRiskStructureForLowRiskMessage(t *testing.T) {
	got := ApplyReplyStyleInstruction("帮我润色这段日报", false, "cms")
	if strings.Contains(got, "结论 / 依据 / 不确定项 / 下一步建议") {
		t.Fatalf("expected low-risk message not to include high-risk structure, got %q", got)
	}
	if !strings.Contains(got, "当前没有足够依据确认") {
		t.Fatalf("expected low-risk message to still include baseline anti-hallucination instruction, got %q", got)
	}
}

func TestApplyReportTimeZoneInstruction(t *testing.T) {
	got := ApplyReportTimeZoneInstruction("请分析日志", "Asia/Shanghai")
	for _, want := range []string{"报告时间约束", "Asia/Shanghai", "YYYY-MM-DD HH:mm:ss（北京时间）", "不要重复加减时差", "不要使用 2026-05-08 20:28:08+08:00"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected report time zone instruction to contain %q, got %q", want, got)
		}
	}

	again := ApplyReportTimeZoneInstruction(got, "Asia/Shanghai")
	if again != got {
		t.Fatalf("expected report time zone instruction not to be duplicated")
	}
}

func TestGetReportTimeZoneFallsBackToTimeZone(t *testing.T) {
	cfg := &Config{Server: ServerConfig{TimeZone: "Asia/Tokyo"}}
	if got := cfg.GetReportTimeZone(); got != "Asia/Tokyo" {
		t.Fatalf("expected report time zone to follow server timeZone, got %q", got)
	}

	cfg.Server.ReportTimeZone = "Asia/Shanghai"
	if got := cfg.GetReportTimeZone(); got != "Asia/Shanghai" {
		t.Fatalf("expected explicit report time zone, got %q", got)
	}
}

func TestIsHighRiskQuestion(t *testing.T) {
	if !IsHighRiskQuestion("请检查这个用户是否有 admin 权限") {
		t.Fatalf("expected permission question to be high risk")
	}
	if !IsHighRiskQuestion("这个生产告警是不是配置变更导致的") {
		t.Fatalf("expected production incident question to be high risk")
	}
	if IsHighRiskQuestion("帮我把这句欢迎语改得更口语化一点") {
		t.Fatalf("expected rewriting question not to be high risk")
	}
}
