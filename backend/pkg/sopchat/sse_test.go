package sopchat

import (
	"strings"
	"testing"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
)

func TestTextContentValuesWithDoneInSameBody(t *testing.T) {
	body := &cmsclient.CreateChatResponseBody{
		Messages: []*cmsclient.CreateChatResponseBodyMessages{
			{
				Role: tea.String("assistant"),
				Contents: []map[string]interface{}{
					{"type": "text", "value": "结论"},
					{"type": "spin_text", "value": "处理中"},
				},
			},
			{Type: tea.String("done")},
		},
	}

	var textParts []string
	for _, msg := range body.Messages {
		if IsDoneChatMessage(msg) {
			continue
		}
		textParts = append(textParts, TextContentValues(msg)...)
	}

	if !IsDoneMessage(body) {
		t.Fatal("expected body to contain done message")
	}
	if got := strings.Join(textParts, ""); got != "结论" {
		t.Fatalf("expected text content before done to be kept, got %q", got)
	}
}

func TestTextContentValuesSkipsNonStringValues(t *testing.T) {
	msg := &cmsclient.CreateChatResponseBodyMessages{
		Contents: []map[string]interface{}{
			{"type": "text", "value": 123},
			{"type": "text", "value": "可读文本"},
		},
	}

	got := strings.Join(TextContentValues(msg), "")
	if got != "可读文本" {
		t.Fatalf("expected only string text content, got %q", got)
	}
}
