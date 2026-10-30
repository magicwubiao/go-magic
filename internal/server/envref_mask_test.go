package server

import (
	"testing"
)

// 掩码的两条边界：字面量密钥照旧 ***，纯 ${ENV} 引用保留原文。
// 后者被遮掉的话，用户分不清"配置里是引用"还是"值被清空了"。
func TestMaskServerEnvKeepsReferences(t *testing.T) {
	got := maskServerEnv([]string{"GH_TOKEN=${GH_TOKEN}", "OTHER=literal-secret"})
	if got["GH_TOKEN"] != "${GH_TOKEN}" {
		t.Fatalf("reference was masked: %q", got["GH_TOKEN"])
	}
	if got["OTHER"] != maskedEnvValue {
		t.Fatalf("literal must stay masked, got %q", got["OTHER"])
	}
}

func TestMaskServerHeadersKeepsReferences(t *testing.T) {
	got := maskServerHeaders(map[string]string{
		"Authorization": "Bearer ${API_TOKEN}",
		"X-Api-Key":     "${API_TOKEN}",
	})
	if got["X-Api-Key"] != "${API_TOKEN}" {
		t.Fatalf("pure reference must be kept, got %q", got["X-Api-Key"])
	}
	// 混合值仍然算密钥：`Bearer ${TOKEN}` 被遮掉后用户改不动前缀，
	// 但也正因为带字面量前缀，它不值得暴露。
	if got["Authorization"] != maskedEnvValue {
		t.Fatalf("mixed value must stay masked, got %q", got["Authorization"])
	}
}

func TestMaskCLIEnvKeepsReferences(t *testing.T) {
	got := maskCLIEnv([]string{"GH_TOKEN=${GH_TOKEN}", "API_KEY=plain"})
	if len(got) != 2 {
		t.Fatalf("unexpected length %d", len(got))
	}
	if got[0] != "GH_TOKEN=${GH_TOKEN}" {
		t.Fatalf("reference was masked: %q", got[0])
	}
	if got[1] != "API_KEY="+maskedEnvValue {
		t.Fatalf("literal must stay masked, got %q", got[1])
	}
}
