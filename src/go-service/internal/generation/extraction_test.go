package generation

import (
	"errors"
	"strings"
	"testing"
)

func TestJSONExtractionRejectsTruncatedRoot(t *testing.T) {
	if _, err := extractJSON(`{"pages":[{"panels":[]}`); !errors.Is(err, ErrResponse) {
		t.Fatalf("truncated JSON accepted: %v", err)
	}
	raw, err := extractJSON("```json\n{\"pages\":[]}\n```")
	if err != nil || string(raw) != `{"pages":[]}` {
		t.Fatalf("fenced JSON: %s %v", raw, err)
	}
}
func TestSimplifiedConversionAndGuard(t *testing.T) {
	text := enforceSimplified(enforceSimplified("歡迎回來，這麼說。乾隆與乾坤。"))
	if strings.Count(text, "【ZH_CN_TEXT_V2】") != 1 || strings.Count(text, finalScriptCheck) != 1 {
		t.Fatal("script guards duplicated")
	}
	if !strings.Contains(text, "欢迎回来，这么说。乾隆与乾坤。") {
		t.Fatalf("conversion: %s", text)
	}
}
