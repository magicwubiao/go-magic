package cortex

import (
	"strings"
	"testing"
)

// TestGetUserContextNeverInjectsPlaceholderProfile
//
// 空用户画像不得注入任何东西。此前 GetUserContext() 读的是原始 USER.md 文件，
// 而 cortex.UserProfile 在磁盘上写的是带 [Not set] 占位符的可读视图 —— 结果每个
// 会话第一轮都往 system prompt 里塞 ~290 字符纯样板（且全部是"未设置"）。
// 现在唯一来源是 UserProfile.GetForPrompt()：无内容返回空串。
func TestGetUserContextNeverInjectsPlaceholderProfile(t *testing.T) {
	mgr := NewManagerWithProfileAndConfig(t.TempDir(), nil, "", &ManagerConfig{Enabled: true})

	if got := mgr.GetUserContext(); got != "" {
		t.Fatalf("空画像必须注入空串，得到 %q（placeholder boilerplate 不得进提示词）", got)
	}

	if err := mgr.UserProfile.SetPreference("Language", "Go"); err != nil {
		t.Fatalf("SetPreference: %v", err)
	}
	if err := mgr.UserProfile.AddTech("Go"); err != nil {
		t.Fatalf("AddTech: %v", err)
	}

	got := mgr.GetUserContext()
	if !strings.Contains(got, "Language: Go") {
		t.Fatalf("GetUserContext 未反映 UserProfile 内容: %q", got)
	}
	if !strings.Contains(got, "Tech stack: Go") {
		t.Fatalf("GetUserContext 未反映 tech stack: %q", got)
	}
	if strings.Contains(got, "[Not set]") {
		t.Fatalf("占位符泄漏进提示词: %q", got)
	}
}

// TestUserProfileGetForPromptDeterministic 前缀缓存友好：同一份画像两次拼出的
// 文本必须逐字节一致（map 遍历顺序随机 ⇒ 必须排序输出）。
func TestUserProfileGetForPromptDeterministic(t *testing.T) {
	up := NewUserProfile(t.TempDir())
	for _, kv := range [][2]string{{"Zebra", "1"}, {"Apple", "2"}, {"Mango", "3"}} {
		if err := up.SetPreference(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	first := up.GetForPrompt()
	if first == "" {
		t.Fatal("GetForPrompt returned empty for a non-empty profile")
	}
	for i := 0; i < 20; i++ {
		if got := up.GetForPrompt(); got != first {
			t.Fatalf("GetForPrompt 输出不稳定（map 顺序）:\n  first=%q\n  got  =%q", first, got)
		}
	}
}
