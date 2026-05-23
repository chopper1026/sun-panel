package assets

import (
	"strings"
	"testing"
)

func TestAssetReadsBundledConfigWithBindataCompatiblePath(t *testing.T) {
	content, err := Asset("assets/conf.example.ini")
	if err != nil {
		t.Fatalf("read embedded config: %v", err)
	}
	if !strings.Contains(string(content), "[base]") {
		t.Fatalf("expected embedded config to contain [base] section")
	}
}

func TestAssetReadsBundledLanguageWithoutPrefix(t *testing.T) {
	content, err := Asset("lang/zh-cn.ini")
	if err != nil {
		t.Fatalf("read embedded language file: %v", err)
	}
	if len(content) == 0 {
		t.Fatalf("expected embedded language file content")
	}
}
