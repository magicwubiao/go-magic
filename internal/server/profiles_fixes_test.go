package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/cortex"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 删除"当前已应用"的预设：只清掉记录，凭据一概不动。
//
// 旧实现会在删除时清 env 再加载默认凭据（因为那时凭据跟着分身走）。降级后
// 一套部署只有一份 .env，删除模板不该影响进程环境里的任何 key。
func TestDeleteAppliedProfileOnlyResetsSelection(t *testing.T) {
	s := newProfileTestServer(t)
	home := s.magicHome
	s.runtimeProfile = "work"
	s.setCfg(&appconfig.Config{Profile: "work"})

	workDir := appconfig.ProfileDir(home, "work")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}

	const keepKey = "PROFILE_FIX_TEST_KEEP_KEY"
	appconfig.ProcessEnv().Clear()
	if err := appconfig.WriteEnvFile(appconfig.EnvFilePath(home, appconfig.DefaultProfileName), map[string]string{keepKey: "kept"}); err != nil {
		t.Fatal(err)
	}
	appconfig.LoadEnvFor(home)
	t.Cleanup(func() {
		appconfig.ProcessEnv().Clear()
		_ = os.Unsetenv(keepKey)
	})

	rec := doProfileReq(t, s, http.MethodDelete, "/api/profiles/work", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete work: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("work dir should be gone, stat err = %v", err)
	}
	if got := s.cfgSnapshot().Profile; got != appconfig.DefaultProfileName {
		t.Errorf("config profile = %q, want %q", got, appconfig.DefaultProfileName)
	}
	if got := s.activeProfile(); got != appconfig.DefaultProfileName {
		t.Errorf("activeProfile = %q, want %q", got, appconfig.DefaultProfileName)
	}
	// 凭据与预设无关：删除模板不能把进程里的 key 撤掉。
	if got := os.Getenv(keepKey); got != "kept" {
		t.Errorf("%s = %q, want kept (credentials must not follow presets)", keepKey, got)
	}
}

// 改名"当前已应用"的预设时，配置里的记录必须跟着走，否则指着不存在的目录。
func TestRenameAppliedProfileUpdatesSelection(t *testing.T) {
	s := newProfileTestServer(t)
	home := s.magicHome
	s.runtimeProfile = "work"
	s.setCfg(&appconfig.Config{Profile: "work"})

	workDir := appconfig.ProfileDir(home, "work")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}

	rec := doProfileReq(t, s, http.MethodPatch, "/api/profiles/work", map[string]interface{}{
		"name": "work", "new_name": "team",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename work: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(appconfig.ProfileDir(home, "work")); !os.IsNotExist(err) {
		t.Errorf("old dir should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "profiles", "team")); err != nil {
		t.Errorf("new dir missing: %v", err)
	}
	if got := s.cfgSnapshot().Profile; got != "team" {
		t.Errorf("config profile = %q, want team", got)
	}
	if got := s.activeProfile(); got != "team" {
		t.Errorf("activeProfile = %q, want team", got)
	}
}

// 应用预设 = 立即把人设写进活动 cortex，并在配置里记下来；返回体不再有
// restart_required 的"待重启"语义。
func TestApplyProfileWritesUserProfileIntoActiveDir(t *testing.T) {
	s := newProfileTestServer(t)
	home := s.magicHome
	s.setCfg(&appconfig.Config{})

	presetDir := appconfig.ProfileDir(home, "work")
	if err := os.MkdirAll(presetDir, 0755); err != nil {
		t.Fatal(err)
	}
	// 只放 USER.json：SOUL 走 live SoulManager，测试服务器没有 cortex，
	// 但画像走 userProfileForWrite（磁盘实例），足以验证"内容被抄进活动目录"。
	doc := map[string]interface{}{
		"preferences": map[string]map[string]string{
			"Name":  {"value": "Alice"},
			"Role":  {"value": "Backend Engineer"},
			"Email": {"value": "should-not-be-copied@example.com"},
		},
		"interests":  []string{"compilers"},
		"tech_stack": []string{"Go"},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(presetDir, "USER.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	rec := doProfileReq(t, s, http.MethodPost, "/api/profiles/work/apply", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply work: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["restart_required"] != false {
		t.Errorf("restart_required = %v, want false (apply is immediate)", resp["restart_required"])
	}
	if got := s.cfgSnapshot().Profile; got != "work" {
		t.Errorf("config profile = %q, want work", got)
	}

	// 活动目录的 USER.json 必须已经带上预设里的画像。
	activeDoc, err := readUserJSON(filepath.Join(appconfig.ProfileDir(home, appconfig.DefaultProfileName), "USER.json"))
	if err != nil {
		t.Fatalf("read active USER.json: %v", err)
	}
	if p := activeDoc.Preferences["Name"]; p == nil || p.Value != "Alice" {
		t.Errorf("active user name not applied: %+v", activeDoc.Preferences["Name"])
	}
	if p := activeDoc.Preferences["Role"]; p == nil || p.Value != "Backend Engineer" {
		t.Errorf("active user role not applied: %+v", activeDoc.Preferences["Role"])
	}
}

// 空预设（既无 SOUL 也无画像）应用时必须是明确的 400，而不是静默成功。
func TestApplyEmptyProfileRejected(t *testing.T) {
	s := newProfileTestServer(t)
	home := s.magicHome
	if err := os.MkdirAll(appconfig.ProfileDir(home, "empty"), 0755); err != nil {
		t.Fatal(err)
	}

	rec := doProfileReq(t, s, http.MethodPost, "/api/profiles/empty/apply", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("apply empty: status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

// default 就是活动人格本身，应用它没有可写的内容。
func TestApplyDefaultProfileRejected(t *testing.T) {
	s := newProfileTestServer(t)
	rec := doProfileReq(t, s, http.MethodPost, "/api/profiles/default/apply", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("apply default: status = %d, want 400", rec.Code)
	}
}

// 人设走 live SoulManager：应用后活动目录的 SOUL.md 必须是预设内容，
// 且不需要重启（旧实现要等下次启动才换目录）。
func TestApplyProfileWritesSoulIntoActiveDir(t *testing.T) {
	s := newProfileTestServer(t)
	home := s.magicHome
	s.setCfg(&appconfig.Config{})

	activeDir := appconfig.ProfileDir(home, appconfig.DefaultProfileName)
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatal(err)
	}
	s.cortexMgr = cortex.NewManagerWithProfileAndConfig(activeDir, nil, "", &cortex.ManagerConfig{Enabled: true})

	presetDir := appconfig.ProfileDir(home, "work")
	if err := os.MkdirAll(presetDir, 0755); err != nil {
		t.Fatal(err)
	}
	const want = "# work persona\nbe terse\n"
	if err := os.WriteFile(filepath.Join(presetDir, "SOUL.md"), []byte(want), 0644); err != nil {
		t.Fatal(err)
	}

	rec := doProfileReq(t, s, http.MethodPost, "/api/profiles/work/apply", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply work: status = %d, body=%s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(filepath.Join(activeDir, "SOUL.md"))
	if err != nil {
		t.Fatalf("read active SOUL.md: %v", err)
	}
	if !strings.Contains(string(data), "be terse") {
		t.Fatalf("active SOUL.md not updated: %q", string(data))
	}
	if got := s.liveSoulManager(activeDir).GetSoul(); !strings.Contains(got, "be terse") {
		t.Fatalf("live soul not updated: %q", got)
	}
}
