package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/internal/cortex"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// 人设（profile）的文件约定：
//
//	<magicHome>/cortex/{SOUL.md,USER.md,USER.json}       ← 活动人格（运行时唯一读写的地方）
//	<magicHome>/profiles/<name>/{SOUL.md,USER.md,USER.json}  ← 预设模板
//
// 预设不再是"第二个身份目录"：应用预设 = 把模板内容写进活动 cortex（走 live
// SoulManager / UserProfile，立即生效，不需要重启）。凭据与会话历史不再随
// 预设变化 —— 一套部署一份凭据。
//
// 路径一律由 appconfig.ProfileDir 裁决（见 pkg/config/profile.go），不要在
// 这里自己拼 "profiles/<name>" —— 历史 bug 就是这么来的：面板写
// profiles/<p>/user.md，运行时却读 cortex/USER.md，保存成功但对话毫无变化。

// profileRoot 返回所有分身目录的父目录。
func (s *Server) profileRoot() string {
	return filepath.Join(s.magicHome, "profiles")
}

// cleanupLegacyDefaultProfile 处理旧布局遗留的 <magicHome>/profiles/default。
//
// 早期版本的"创建分身(克隆 default)"会在这个位置建出一棵
// cache/logs/memory/plugins/sessions/skills 的空骨架。它不参与运行，也不在
// 列表里（否则会出现两个 default），但会一直占着目录、让用户以为有份数据。
//
// 处理原则是**只删完全空的树**：整棵子树里只要存在任何一个普通文件或符号
// 链接，就原样保留并打日志 —— 里面可能是用户真实的历史会话/记忆，自动迁移
// 或删除都可能造成不可逆的数据丢失，必须由人确认后再搬。
func (s *Server) cleanupLegacyDefaultProfile() {
	dir := filepath.Join(s.profileRoot(), appconfig.DefaultProfileName)

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}

	empty, err := isEmptyTree(dir)
	if err != nil {
		log.Warnf("[profiles] cannot inspect legacy profiles/default, skipping cleanup dir=%s: %v", dir, err)
		return
	}
	if !empty {
		log.Warnf("[profiles] legacy profiles/default contains real files; it is not used at runtime, inspect it before migrating or deleting dir=%s", dir)
		return
	}

	if err := os.RemoveAll(dir); err != nil {
		log.Warnf("[profiles] failed to remove empty legacy profiles/default dir=%s: %v", dir, err)
		return
	}
	log.Infof("[profiles] removed empty legacy profiles/default dir=%s", dir)
}

// isEmptyTree 判断 root 这棵树里是否只有目录：只要出现普通文件、符号链接
// 或其它非常规条目，就返回 false（视为"有内容"，绝不动）。
func isEmptyTree(root string) (bool, error) {
	empty := true
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		empty = false
		return filepath.SkipAll
	})
	if err != nil {
		return false, err
	}
	return empty, nil
}

// activeProfile 返回**当前已应用**的人设名（可能是 "default" = 不属于
// 任何预设，就用 cortex 里这份）。
//
// 应用预设会立刻写入活动 cortex，所以这里和配置里的选择总是同步的；维护
// 独立字段只为了让"进程内视角"有单一出处（配置可能被外部改写）。
func (s *Server) activeProfile() string {
	if s.runtimeProfile == "" {
		return appconfig.DefaultProfileName
	}
	return s.runtimeProfile
}

// profilePaths 解析分身的人设文件路径；名字非法时返回 error（调用方回 400）。
func (s *Server) profilePaths(name string) (dir, soul, userMD, userJSON string, err error) {
	dir = appconfig.ProfileDir(s.magicHome, name)
	if dir == "" {
		return "", "", "", "", fmt.Errorf("invalid profile name: %q", name)
	}
	return dir,
		filepath.Join(dir, "SOUL.md"),
		filepath.Join(dir, "USER.md"),
		filepath.Join(dir, "USER.json"),
		nil
}

// handleProfiles 处理 /api/profiles（列表 / 创建）。
func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, map[string]interface{}{"profiles": s.scanProfiles()})
	case http.MethodPost:
		s.createProfile(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string `json:"name"`
		CloneFromDefault bool   `json:"clone_from_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(req.Name)
	if !appconfig.ValidProfileName(name) {
		http.Error(w, "Invalid profile name: use letters, digits, '-', '_' or '.' (no path separators)", http.StatusBadRequest)
		return
	}
	if name == appconfig.DefaultProfileName {
		http.Error(w, `Profile "default" is built in and cannot be created`, http.StatusBadRequest)
		return
	}

	dir, _, _, _, err := s.profilePaths(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 原子创建：os.Mkdir 对已存在目录必然报错，并发创建时只有一个请求成功，
	// 409 语义不会丢。此前的"stat 检查 + MkdirAll"窗口内两个请求都能通过，
	// MkdirAll 对已存在目录又不报错，最终双方都返回成功。
	if err := os.MkdirAll(s.profileRoot(), 0755); err != nil {
		http.Error(w, "Failed to create profiles dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		if os.IsExist(err) {
			http.Error(w, "Profile already exists: "+name, http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create profile dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if req.CloneFromDefault {
		// 语义是"从 default 克隆"，源必须是 default，而不是"当前已应用的预设"：
		// 用户在别处点"新建 + 从默认克隆"，期望拿到的是 default 的人设，
		// 此前会悄悄把当前预设的人设复制过去。
		if err := s.cloneProfile(appconfig.DefaultProfileName, name); err != nil {
			// 目录已经建出来了，但克隆失败：保留空分身并明确报错，比假装成功好。
			http.Error(w, "Profile created but clone failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "path": dir})
}

// cloneProfile 把 src 预设的**人设与画像**复制到 dst。
//
// 刻意不复制 .env 与 skills/：
//   - 凭据一套部署只有一份，复制会在磁盘上多一份明文 API key，切预设也不该换 key；
//   - 技能是全局的（skillMgr 只扫 <magicHome>/skills），预设目录里的 skills/
//     运行时不加载，复制过去只会让面板显示一个假的技能数。
func (s *Server) cloneProfile(src, dst string) error {
	srcDir, srcSoul, _, _, err := s.profilePaths(src)
	if err != nil {
		return err
	}
	_, dstSoul, dstUserMD, dstUserJSON, err := s.profilePaths(dst)
	if err != nil {
		return err
	}

	// 人设三件套。旧版本曾把 SOUL.md 写成小写 soul.md，读取时一并兜住。
	if err := copyFileIfExists(readableSoulPath(srcSoul), dstSoul, 0644); err != nil {
		return err
	}
	if err := copyFileIfExists(filepath.Join(srcDir, "USER.md"), dstUserMD, 0644); err != nil {
		return err
	}
	if err := copyFileIfExists(filepath.Join(srcDir, "USER.json"), dstUserJSON, 0644); err != nil {
		return err
	}
	return nil
}

// handleProfileByName 处理 /api/profiles/<name>[...]。
func (s *Server) handleProfileByName(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	name, action, _ := strings.Cut(rel, "/")

	if !appconfig.ValidProfileName(name) {
		http.Error(w, "Invalid profile name", http.StatusBadRequest)
		return
	}

	dir, soulPath, userMDPath, _, err := s.profilePaths(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch action {
	case "setup-command":
		// default 就是活动人格，不需要 --profile；其它预设启动时会被应用一次。
		cmd := "magic chat"
		if name != appconfig.DefaultProfileName {
			cmd = fmt.Sprintf("magic --profile %s chat", name)
		}
		jsonResponse(w, map[string]string{"command": cmd})
		return

	case "apply", "switch":
		// "switch" 保留为别名：旧前端 / 已部署的书签仍然打这个路径。
		// 语义已从"写配置、等重启"变成"立即把人设写进活动 cortex"。
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.applyProfile(w, name, dir)
		return

	case "soul":
		s.handleProfileSoul(w, r, name, dir, soulPath)
		return

	case "user":
		s.handleProfileUser(w, r, dir, userMDPath)
		return

	case "preferences":
		s.handleProfilePreferences(w, r, dir)
		return
	}

	if strings.HasPrefix(action, "preferences/") && strings.HasSuffix(action, "/feedback") {
		key := strings.TrimSuffix(strings.TrimPrefix(action, "preferences/"), "/feedback")
		s.handlePreferenceFeedback(w, r, name, dir, key)
		return
	}

	if action != "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		s.deleteProfile(w, name, dir)
	case http.MethodPatch:
		s.renameProfile(w, r, name, dir)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// applyProfile 把人设**立刻应用**到活动 cortex。
//
// 旧实现（switchProfile）只把人选写进 config.json，人设/记忆要等重启才换，
// 于是面板必须同时表达"生效中 / 待重启"两种状态，逻辑和 UI 都被这份
// 中间态拖累。降级后预设只是内容模板：把它抄进活动目录，改完即生效。
//
// 不碰凭据：一套部署只有一份 .env。预设目录里历史的 .env 原样留在磁盘上，
// 既不读取也不复制 —— 让"换人格"和"换账号"彻底分开。
func (s *Server) applyProfile(w http.ResponseWriter, name, dir string) {
	if name == appconfig.DefaultProfileName {
		http.Error(w, `Profile "default" is the active persona itself, nothing to apply`, http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Profile not found: "+name, http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to inspect profile: "+err.Error(), http.StatusInternalServerError)
		return
	}

	applied, err := s.applyPersonaFiles(name)
	if err != nil {
		http.Error(w, "Failed to apply profile: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !applied.hasSoul && !applied.hasUser {
		http.Error(w, "Profile is empty: add a SOUL.md or user profile before applying", http.StatusBadRequest)
		return
	}

	// 记录"最近应用的是哪套预设"：只用于重启后仍能显示来源，以及面板标记。
	// 失败不影响本次已生效的内容，所以只记日志、不回滚。
	if err := s.persistProfileSelection(name); err != nil {
		log.Warnf("[profiles] applied %q but failed to record selection: %v", name, err)
	}
	s.runtimeProfile = name

	jsonResponse(w, map[string]interface{}{
		"ok":               true,
		"name":             name,
		"applied":          true,
		"has_soul":         applied.hasSoul,
		"has_user":         applied.hasUser,
		"restart_required": false,
	})
}

// personaApplyResult 描述一次应用实际写入了什么。
type personaApplyResult struct {
	hasSoul bool
	hasUser bool
}

// applyPersonaFiles 读预设目录里的人设文件，写进活动 cortex。
//
// 必须走 live SoulManager / UserProfile：它们内存里各有一份内容，直接覆盖
// 磁盘会被下一次保存冲掉（丢更新），也会绕过大小闸与快照保护。
func (s *Server) applyPersonaFiles(name string) (personaApplyResult, error) {
	var res personaApplyResult

	presetDir, presetSoul, presetUserMD, presetUserJSON, err := s.profilePaths(name)
	if err != nil {
		return res, err
	}

	soulData, _, soulErr := readFirstExisting(presetSoul, legacyLowerName(presetSoul))
	if soulErr != nil && !os.IsNotExist(soulErr) {
		return res, fmt.Errorf("read soul: %w", soulErr)
	}
	if soulErr == nil {
		res.hasSoul = true
	}

	doc, jsonErr := readUserJSON(presetUserJSON)
	if jsonErr != nil && !os.IsNotExist(jsonErr) {
		return res, jsonErr
	}
	mdData, _, mdErr := readFirstExisting(presetUserMD, legacyLowerName(presetUserMD))
	if mdErr != nil && !os.IsNotExist(mdErr) {
		return res, fmt.Errorf("read user profile: %w", mdErr)
	}
	if doc != nil || mdErr == nil {
		res.hasUser = true
	}

	// 画像的取值优先级与读接口一致：USER.json 是权威，缺失时退回解析 USER.md。
	var info cortex.UserProfileInfo
	switch {
	case doc != nil:
		prefValue := func(key string) string {
			if p := doc.Preferences[key]; p != nil {
				return p.Value
			}
			return ""
		}
		info = cortex.UserProfileInfo{
			Name:               prefValue("Name"),
			Role:               prefValue("Role"),
			CommunicationStyle: prefValue("Communication style"),
			CodeStyle:          prefValue("Code style"),
			TechStack:          doc.TechStack,
			Interests:          doc.Interests,
		}
	case mdErr == nil:
		data := s.parseUserMD(string(mdData))
		info = cortex.UserProfileInfo{
			Name:               mapString(data, "name"),
			Role:               mapString(data, "role"),
			CommunicationStyle: mapString(data, "communication_style"),
			CodeStyle:          mapString(data, "code_style"),
			TechStack:          mapStringSlice(data, "tech_stack"),
			Interests:          mapStringSlice(data, "interests"),
		}
	}

	activeDir := appconfig.ProfileDir(s.magicHome, appconfig.DefaultProfileName)

	if res.hasSoul {
		soul := s.liveSoulManager(activeDir)
		if soul == nil {
			// cortex 被配置关掉时没有 SoulManager：人设无处可写，明确拒绝，
			// 不要假装应用成功（那会让"应用了却没变化"变成一个悬案）。
			return res, fmt.Errorf("cortex is disabled; enable cortex to apply personas")
		}
		if err := soul.SetSoul(string(soulData)); err != nil {
			return res, fmt.Errorf("save soul: %w", err)
		}
	}
	if res.hasUser {
		up, err := s.userProfileForWrite(activeDir)
		if err != nil {
			return res, fmt.Errorf("load user profile: %w", err)
		}
		if err := up.UpdateProfileInfo(info); err != nil {
			return res, fmt.Errorf("save user profile: %w", err)
		}
	}

	_ = presetDir // 路径已由 profilePaths 校验；保留变量便于日后诊断
	return res, nil
}

// persistProfileSelection 把 cfg.Profile 落盘（与其它配置写入走同一套锁）。
func (s *Server) persistProfileSelection(name string) error {
	s.acquireServerMu("")
	defer s.releaseServerMu()
	s.lockCfgForWrite()
	defer s.unlockCfgForWrite()
	s.cfg.Profile = name
	return s.persistConfig(true)
}

func (s *Server) deleteProfile(w http.ResponseWriter, name, dir string) {
	if name == appconfig.DefaultProfileName {
		http.Error(w, `Profile "default" is built in and cannot be deleted`, http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Profile not found: "+name, http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to inspect profile: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 删除预设**不会**改变当前人格：应用预设时内容已经抄进 cortex 了，模板
	// 现在只是历史记录。唯一要处理的是配置里的"最近应用"标记别指向一个
	// 已不存在的目录（下次面板会拿它当来源显示）。
	wasApplied := s.activeProfile() == name
	if wasApplied {
		if err := s.persistProfileSelection(appconfig.DefaultProfileName); err != nil {
			http.Error(w, "Failed to reset profile selection: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.runtimeProfile = appconfig.DefaultProfileName
	}

	if err := os.RemoveAll(dir); err != nil {
		http.Error(w, "Failed to delete profile: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"ok": true, "name": name, "was_applied": wasApplied})
}

func (s *Server) renameProfile(w http.ResponseWriter, r *http.Request, name, dir string) {
	// default 不是 profiles/<name> 这样的普通目录，而是正在运行的 <magicHome>/cortex。
	// 对它做 os.Rename 等于把整个运行时数据目录（SOUL/USER/记忆/日构建）搬到
	// profiles/<newname>，进程立刻读不到自己的数据；而且旧名 "default" 是内置的，
	// 改完也回不来。删除接口已有同样的闸，这里必须一致地拦住。
	if name == appconfig.DefaultProfileName {
		http.Error(w, `Profile "default" is built in and cannot be renamed`, http.StatusBadRequest)
		return
	}

	var req struct {
		Name    string `json:"name"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	newName := strings.TrimSpace(req.NewName)
	if newName == "" {
		newName = strings.TrimSpace(req.Name)
	}
	if !appconfig.ValidProfileName(newName) || newName == appconfig.DefaultProfileName {
		http.Error(w, "Invalid new profile name", http.StatusBadRequest)
		return
	}
	if newName == name {
		jsonResponse(w, map[string]interface{}{"ok": true, "name": newName, "path": dir})
		return
	}

	newDir, _, _, _, err := s.profilePaths(newName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, statErr := os.Stat(newDir); statErr == nil {
		http.Error(w, "Profile already exists: "+newName, http.StatusConflict)
		return
	} else if !os.IsNotExist(statErr) {
		http.Error(w, "Failed to inspect target: "+statErr.Error(), http.StatusInternalServerError)
		return
	}

	// 配置里若记着"最近应用的是它"，改名后要跟着变，否则标记指向一个不存在
	// 的目录。仅此而已：当前人格内容早已在 cortex 里，不受改名影响。
	needConfigUpdate := s.activeProfile() == name
	if err := os.Rename(dir, newDir); err != nil {
		http.Error(w, "Failed to rename profile: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if needConfigUpdate {
		if err := s.persistProfileSelection(newName); err != nil {
			// 目录已搬走但配置没跟上：改回去比留一个悬空引用安全。回滚也失败时
			// 明确告诉用户新位置，别让人对着旧路径找文件。
			if rbErr := os.Rename(newDir, dir); rbErr != nil {
				http.Error(w, fmt.Sprintf(
					"Renamed to %q but failed to update profile selection (%s); rollback also failed (%s), the directory now lives at %s",
					newName, err.Error(), rbErr.Error(), newDir), http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to update profile selection, rename rolled back: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.runtimeProfile = newName
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "name": newName, "path": newDir})
}

// handleProfileSoul 读写预设的 SOUL.md。
//
// default 预设就是活动人格，必须走 live SoulManager：内存里有一份内容，直接
// 写盘会被它下一次保存覆盖（丢更新），而且绕过大小闸/快照/区块淘汰这些保护。
// 其它预设是纯模板文件，直接读写磁盘即可。
func (s *Server) handleProfileSoul(w http.ResponseWriter, r *http.Request, name, dir, soulPath string) {
	live := s.liveSoulManager(dir)

	switch r.Method {
	case http.MethodGet:
		if live != nil {
			jsonResponse(w, map[string]interface{}{
				"content": live.GetSoul(),
				"exists":  true,
				"source":  "cortex",
			})
			return
		}
		data, path, err := readFirstExisting(soulPath, legacyLowerName(soulPath))
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "Failed to read soul: "+err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"content": string(data),
			"exists":  path != "",
			"source":  path,
		})
	case http.MethodPut:
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if live != nil {
			if err := live.SetSoul(req.Content); err != nil {
				// 内容超限且没有可回收区块时 SetSoul 会拒绝写入，保留原内容。
				http.Error(w, "Failed to save soul: "+err.Error(), http.StatusBadRequest)
				return
			}
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}
		if err := writeFileAtomic(soulPath, []byte(req.Content), 0644); err != nil {
			http.Error(w, "Failed to save soul: "+err.Error(), http.StatusInternalServerError)
			return
		}
		removeLegacyIfDistinct(legacyLowerName(soulPath), soulPath)
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// liveSoulManager 当 dir 就是活动 cortex 的目录时返回其 SoulManager。
//
// 活动目录恒为 <magicHome>/cortex（与"当前应用了哪个预设"无关），所以这里
// 必须按 default 解析，不能再用 activeProfile() —— 否则应用过 work 预设后，
// 编辑 default 卡片反而拿不到 live 实例，写盘会被下一次内存保存冲掉。
func (s *Server) liveSoulManager(dir string) *cortex.SoulManager {
	if s.cortexMgr == nil || s.cortexMgr.Soul == nil {
		return nil
	}
	activeDir := appconfig.ProfileDir(s.magicHome, appconfig.DefaultProfileName)
	if filepath.Clean(activeDir) != filepath.Clean(dir) {
		return nil
	}
	return s.cortexMgr.Soul
}

// userJSONDoc 是 USER.json 的磁盘格式（与 cortex.UserProfile 的导出结构一致）。
type userJSONDoc struct {
	Preferences map[string]*cortex.UserPreference `json:"preferences"`
	Interests   []string                          `json:"interests"`
	TechStack   []string                          `json:"tech_stack"`
}

// readUserJSON 读取分身的 USER.json；不存在时返回 (nil, os.ErrNotExist)。
func readUserJSON(path string) (*userJSONDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc userJSONDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("corrupt %s: %w", filepath.Base(path), err)
	}
	return &doc, nil
}

// handleProfileUser 读写分身的用户画像。
//
// 读：USER.json（权威）优先，缺失时退回解析 USER.md。
// 写：data 走 cortex.UserProfile（同时更新 JSON 与 md），content 仅覆盖 md。
func (s *Server) handleProfileUser(w http.ResponseWriter, r *http.Request, dir, userMDPath string) {
	switch r.Method {
	case http.MethodGet:
		content, _, err := readFirstExisting(userMDPath, legacyLowerName(userMDPath))
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "Failed to read user profile: "+err.Error(), http.StatusInternalServerError)
			return
		}

		doc, jsonErr := readUserJSON(filepath.Join(dir, "USER.json"))
		if jsonErr != nil && !os.IsNotExist(jsonErr) {
			http.Error(w, jsonErr.Error(), http.StatusInternalServerError)
			return
		}

		data := s.parseUserMD(string(content))
		if doc != nil {
			// JSON 是权威存储：面板表单以它为准，md 只是人类可读视图。
			data = userDataFromDoc(doc)
		}
		jsonResponse(w, map[string]interface{}{
			"content": string(content),
			"exists":  len(content) > 0 || doc != nil,
			"data":    data,
		})
	case http.MethodPut:
		var req struct {
			Content string                 `json:"content"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Data == nil {
			// 兼容旧客户端：整段覆盖 md。注意它不会更新 USER.json，
			// 因此不会影响 prompt —— 面板走的是下面 data 分支。
			if err := writeFileAtomic(userMDPath, []byte(req.Content), 0644); err != nil {
				http.Error(w, "Failed to save user profile: "+err.Error(), http.StatusInternalServerError)
				return
			}
			removeLegacyIfDistinct(legacyLowerName(userMDPath), userMDPath)
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}

		up, err := s.userProfileForWrite(dir)
		if err != nil {
			http.Error(w, "Failed to load user profile: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := up.UpdateProfileInfo(cortex.UserProfileInfo{
			Name:               mapString(req.Data, "name"),
			Role:               mapString(req.Data, "role"),
			CommunicationStyle: mapString(req.Data, "communication_style"),
			CodeStyle:          mapString(req.Data, "code_style"),
			TechStack:          mapStringSlice(req.Data, "tech_stack"),
			Interests:          mapStringSlice(req.Data, "interests"),
		}); err != nil {
			http.Error(w, "Failed to save user profile: "+err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleProfilePreferences 返回分身的偏好列表（真实数据，不再是 mock）。
func (s *Server) handleProfilePreferences(w http.ResponseWriter, r *http.Request, dir string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	up, err := s.userProfileForRead(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			jsonResponse(w, map[string]interface{}{"preferences": []interface{}{}})
			return
		}
		http.Error(w, "Failed to read preferences: "+err.Error(), http.StatusInternalServerError)
		return
	}

	prefs := up.GetAllPreferences()
	out := make([]map[string]interface{}, 0, len(prefs))
	for _, p := range prefs {
		out = append(out, map[string]interface{}{
			"key":        p.Key,
			"value":      p.Value,
			"context":    p.Context,
			"confidence": p.Confidence,
			"source":     p.Source,
			"updated_at": p.UpdatedAt.Unix(),
		})
	}
	jsonResponse(w, map[string]interface{}{"preferences": out})
}

// handlePreferenceFeedback 记录"准 / 不准"反馈并真正调整置信度。
func (s *Server) handlePreferenceFeedback(w http.ResponseWriter, r *http.Request, name, dir, key string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if key == "" {
		http.Error(w, "Missing preference key", http.StatusBadRequest)
		return
	}
	var req struct {
		Accurate bool `json:"accurate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Profile not found: "+name, http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to inspect profile: "+err.Error(), http.StatusInternalServerError)
		return
	}

	up, err := s.userProfileForWrite(dir)
	if err != nil {
		http.Error(w, "Failed to load user profile: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := up.ApplyFeedback(key, req.Accurate); err != nil {
		if strings.Contains(err.Error(), "preference not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to save feedback: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "key": key, "accurate": req.Accurate})
}

// userProfileForRead 返回只读的画像视图：正在生效的分身直接复用 cortex 的
// 内存实例（避免读到写了一半的磁盘状态），其它分身从磁盘只读加载
// （缺文件时不会写默认模板）。
func (s *Server) userProfileForRead(dir string) (*cortex.UserProfile, error) {
	if up := s.liveUserProfile(dir); up != nil {
		return up, nil
	}
	up := cortex.NewUserProfile(dir)
	if err := up.LoadExisting(); err != nil {
		return nil, err
	}
	return up, nil
}

// userProfileForWrite 返回可写实例；优先复用 cortex 的活动实例，否则从磁盘加载
// （缺失时生成默认模板）。
func (s *Server) userProfileForWrite(dir string) (*cortex.UserProfile, error) {
	if up := s.liveUserProfile(dir); up != nil {
		return up, nil
	}
	up := cortex.NewUserProfile(dir)
	if err := up.Load(); err != nil {
		return nil, err
	}
	return up, nil
}

// liveUserProfile 当 dir 就是 cortex 正在使用的目录时返回其实例。
func (s *Server) liveUserProfile(dir string) *cortex.UserProfile {
	if s.cortexMgr == nil || s.cortexMgr.UserProfile == nil {
		return nil
	}
	if filepath.Clean(s.cortexMgr.UserProfile.Dir()) != filepath.Clean(dir) {
		return nil
	}
	return s.cortexMgr.UserProfile
}

// userDataFromDoc 把 USER.json 映射成面板表单结构。
func userDataFromDoc(doc *userJSONDoc) map[string]interface{} {
	prefValue := func(key string) string {
		if doc == nil || doc.Preferences == nil {
			return ""
		}
		if p, ok := doc.Preferences[key]; ok && p != nil {
			return p.Value
		}
		return ""
	}
	return map[string]interface{}{
		"name":                prefValue("Name"),
		"role":                prefValue("Role"),
		"communication_style": prefValue("Communication style"),
		"code_style":          prefValue("Code style"),
		"tech_stack":          nonNilStrings(doc.TechStack),
		"interests":           nonNilStrings(doc.Interests),
	}
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func mapString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func mapStringSlice(m map[string]interface{}, key string) []string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// legacyLowerName 返回同一目录下的小写文件名（soul.md / user.md）。
//
// 老版本的服务器把 SOUL.md / USER.md 写成小写：Linux 上大小写敏感，不兜住
// 老文件会让用户看到"人格/画像丢了"。Windows 上两者其实是同一个文件，
// 写回后 removeLegacyIfDistinct 会用 os.SameFile 判断，不会误删。
func legacyLowerName(path string) string {
	return filepath.Join(filepath.Dir(path), strings.ToLower(filepath.Base(path)))
}

// readableSoulPath 返回实际存在的 SOUL.md 路径（优先大写，其次旧的小写）。
func readableSoulPath(soulPath string) string {
	if _, err := os.Stat(soulPath); err == nil {
		return soulPath
	}
	return legacyLowerName(soulPath)
}

// readFirstExisting 依次尝试 candidates，返回第一个读成功的路径。
func readFirstExisting(candidates ...string) ([]byte, string, error) {
	var lastErr error
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, p, nil
		}
		if !os.IsNotExist(err) {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", os.ErrNotExist
}

// removeLegacyIfDistinct 删除旧的小写文件，但仅当它与新文件不是同一个文件
// （大小写不敏感的文件系统上它们是同一个 inode）。
func removeLegacyIfDistinct(legacy, current string) {
	if legacy == current {
		return
	}
	legacyInfo, err := os.Stat(legacy)
	if err != nil {
		return
	}
	currentInfo, err := os.Stat(current)
	if err != nil {
		return
	}
	if os.SameFile(legacyInfo, currentInfo) {
		return
	}
	_ = os.Remove(legacy)
}

// copyFileIfExists 复制文件；源不存在时静默跳过。
func copyFileIfExists(src, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return writeFileAtomic(dst, data, perm)
}

// writeFileAtomic 先写同目录临时文件再 rename，避免崩溃留下半截文件。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
