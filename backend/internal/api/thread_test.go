package api

import (
	"strings"
	"testing"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
)

func TestExtractThreadQuestionPreview(t *testing.T) {
	roleAssistant := "assistant"
	roleUser := "user"

	preview := extractThreadQuestionPreview([]*cmsclient.GetThreadDataResponseBodyData{
		{
			Messages: []*cmsclient.GetThreadDataResponseBodyDataMessages{
				{
					Role: &roleAssistant,
					Contents: []map[string]interface{}{
						{"type": "text", "value": "这是回答"},
					},
				},
				{
					Role: &roleUser,
					Contents: []map[string]interface{}{
						{"type": "text", "value": "  帮我检查   昨晚   WAF   日志  "},
					},
				},
			},
		},
	})

	if preview != "帮我检查 昨晚 WAF 日志" {
		t.Fatalf("unexpected preview: %q", preview)
	}
}

func TestNormalizeThreadQuestionPreview(t *testing.T) {
	longText := strings.Repeat("问题", 50)
	preview := normalizeThreadQuestionPreview(longText)
	if preview == "" {
		t.Fatal("expected preview to be generated")
	}
	if !strings.HasSuffix(preview, "...") {
		t.Fatalf("expected preview to end with ellipsis, got %q", preview)
	}
}
