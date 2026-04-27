package sopchat

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8BytesKeepsValueWithinLimit(t *testing.T) {
	value := strings.Repeat("多字节属性预览", 12)

	got := truncateUTF8Bytes(value, threadAttributeValueMaxBytes)
	if len(got) > threadAttributeValueMaxBytes {
		t.Fatalf("expected at most %d bytes, got %d: %q", threadAttributeValueMaxBytes, len(got), got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("expected valid utf8, got %q", got)
	}
	if !strings.HasSuffix(got, truncatedAttributeSuffix) {
		t.Fatalf("expected truncated suffix, got %q", got)
	}
}

func TestSanitizeThreadAttributesDropsEmptyAndNonStringValues(t *testing.T) {
	attrs := sanitizeThreadAttributes(map[string]interface{}{
		"firstUserQuestion": strings.Repeat("问题", 60),
		"empty":             "   ",
		"number":            1,
	})

	if _, ok := attrs["empty"]; ok {
		t.Fatal("expected empty string attribute to be dropped")
	}
	if _, ok := attrs["number"]; ok {
		t.Fatal("expected non-string attribute to be dropped")
	}

	value := attrs["firstUserQuestion"]
	if value == nil {
		t.Fatal("expected firstUserQuestion attribute")
	}
	if len(*value) > threadAttributeValueMaxBytes {
		t.Fatalf("expected sanitized value within limit, got %d bytes", len(*value))
	}
	if !utf8.ValidString(*value) {
		t.Fatalf("expected valid utf8, got %q", *value)
	}
}
