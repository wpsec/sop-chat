package chatflow

import (
	"strings"
	"testing"

	"sop-chat/internal/config"
)

func TestPrepareAppendsConfiguredReportTimeZoneInstruction(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			TimeZone:       "Asia/Tokyo",
			ReportTimeZone: "Asia/Shanghai",
		},
	}

	prepared, err := Prepare(
		cfg,
		"请分析 SLS 日志",
		false,
		cfg.GetTimeZone(),
		"zh",
		config.NewProductContext("sls", "demo-project", "", ""),
	)
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if prepared == nil {
		t.Fatalf("expected prepared request")
	}
	for _, want := range []string{"报告时间约束", "Asia/Shanghai", "YYYY-MM-DD HH:mm:ss（北京时间）", "不要重复加减时差"} {
		if !strings.Contains(prepared.Message, want) {
			t.Fatalf("expected prepared message to contain %q, got %q", want, prepared.Message)
		}
	}
	if got := prepared.Variables["timeZone"]; got != "Asia/Tokyo" {
		t.Fatalf("expected chat variable timeZone to remain request time zone, got %v", got)
	}
	if got := prepared.Variables["reportTimeZone"]; got != "Asia/Shanghai" {
		t.Fatalf("expected reportTimeZone variable to use configured report time zone, got %v", got)
	}
}
