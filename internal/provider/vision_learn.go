package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 视觉能力运行时自学习。
//
// 名称启发式与 catalog 判定天然滞后于厂商上新：新视觉模型 ID 既不在目录里、
// 命名也常不含 vision/vl 字样（如某代旗舰悄悄支持了图片输入），首次判定就会
// 失真。这里用模型自己的行为当证据源，双向闭环：
//
//   - 正向：带图请求被 API 接受（200 / 流握手成功）→ 记住「支持视觉」。
//     持久化到磁盘且无 TTL —— 厂商几乎不会收回已发布的视觉能力。
//   - 反向：模型显式拒绝图片部件（见 RememberModelNoVision 的调用方）→
//     记住「不支持」。持久化但带 TTL（visionNegativeTTL）：厂商补上视觉
//     支持后旧证据自动失效，不需要手工清缓存。
//
// 两个方向互斥：新证据写入时清掉旧方向的记录，最新证据获胜。
// 用户显式的 vision 配置声明（VisionOverride）在更上层（WithAutoVision）
// 直接短路，不受本缓存影响 —— 人工声明永远赢过机器学习。
const visionNegativeTTL = 30 * 24 * time.Hour

// visionLearnedFile 是持久化文件名，位于 magicHome 下。
const visionLearnedFile = "vision_learned.json"

type visionLearnedStore struct {
	mu       sync.Mutex
	positive map[string]time.Time // model(lower) -> 证据时间
	negative map[string]time.Time
	path     string // 空 = 未启用持久化（纯内存）
	loaded   bool
	dirty    bool // 有未落盘变更时置位，由保存路径消费
}

var visionLearned = &visionLearnedStore{
	positive: map[string]time.Time{},
	negative: map[string]time.Time{},
}

// SetVisionLearnedPath 启用学习结果的持久化（<magicHome>/vision_learned.json）。
// 由 pkg/config.CreateProviderFor 在首次创建 provider 时调用；重复调用是
// no-op（首个路径获胜），path 为空同样忽略。调用时会同步加载已有文件。
func SetVisionLearnedPath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	visionLearned.mu.Lock()
	defer visionLearned.mu.Unlock()
	if visionLearned.loaded {
		return
	}
	visionLearned.loaded = true
	visionLearned.path = path
	visionLearned.loadLocked()
}

// RememberModelVision 记录 modelName 在运行时接受了图片部件（硬证据：
// 支持视觉）。返回是否改变了学习状态，供调用方决定是否打日志。
func RememberModelVision(modelName string) bool {
	model := normalizeVisionModel(modelName)
	if model == "" {
		return false
	}
	visionLearned.mu.Lock()
	defer visionLearned.mu.Unlock()
	changed := false
	if _, ok := visionLearned.positive[model]; !ok {
		changed = true
	}
	visionLearned.positive[model] = time.Now()
	delete(visionLearned.negative, model)
	visionLearned.saveLocked()
	return changed
}

// RememberModelNoVision records that modelName rejected image parts, so
// subsequent ModelSupportsVision calls return false for it. 返回是否改变
// 了学习状态。
func RememberModelNoVision(modelName string) bool {
	model := normalizeVisionModel(modelName)
	if model == "" {
		return false
	}
	visionLearned.mu.Lock()
	defer visionLearned.mu.Unlock()
	changed := false
	if _, ok := visionLearned.negative[model]; !ok {
		changed = true
	}
	visionLearned.negative[model] = time.Now()
	delete(visionLearned.positive, model)
	visionLearned.saveLocked()
	return changed
}

// learnedVisionVerdict 查询运行时学习结果。known=false 表示无有效证据
// （未学过，或反向证据已过 TTL），交回给 catalog / 启发式判定。
func learnedVisionVerdict(modelLower string) (vision, known bool) {
	visionLearned.mu.Lock()
	defer visionLearned.mu.Unlock()
	if t, ok := visionLearned.negative[modelLower]; ok {
		if time.Since(t) < visionNegativeTTL {
			return false, true
		}
		// 过期的反向证据就地清除：厂商补上视觉支持后自然回到正常判定链。
		delete(visionLearned.negative, modelLower)
		visionLearned.dirty = true
		go visionLearned.flushIfDirty()
	}
	if _, ok := visionLearned.positive[modelLower]; ok {
		return true, true
	}
	return false, false
}

// resetLearnedVisionForTest clears the runtime learning cache (memory only;
// the persisted file is intentionally left alone so cross-test pollution
// via disk cannot happen).
func resetLearnedVisionForTest() {
	visionLearned.mu.Lock()
	defer visionLearned.mu.Unlock()
	visionLearned.positive = map[string]time.Time{}
	visionLearned.negative = map[string]time.Time{}
}

// reloadVisionLearnedForTest re-reads the persistence file into memory
// (simulating a process restart). Test-only.
func reloadVisionLearnedForTest() {
	visionLearned.mu.Lock()
	defer visionLearned.mu.Unlock()
	visionLearned.positive = map[string]time.Time{}
	visionLearned.negative = map[string]time.Time{}
	if visionLearned.loaded {
		visionLearned.loadLocked()
	}
}

func normalizeVisionModel(modelName string) string {
	return strings.ToLower(strings.TrimSpace(modelName))
}

// ---- persistence ----

type visionLearnedFileData struct {
	Version  int                  `json:"version"`
	Positive map[string]time.Time `json:"positive"`
	Negative map[string]time.Time `json:"negative"`
}

// loadLocked 读入持久化文件。加载时即丢弃过期的反向证据。文件缺失或损坏
// 一律静默降级为空表 —— 学习缓存只影响判定精度，不允许它拖垮启动。
func (s *visionLearnedStore) loadLocked() {
	s.positive = map[string]time.Time{}
	s.negative = map[string]time.Time{}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var parsed visionLearnedFileData
	if err := json.Unmarshal(data, &parsed); err != nil {
		return
	}
	now := time.Now()
	for m, t := range parsed.Positive {
		if m != "" {
			s.positive[m] = t
		}
	}
	for m, t := range parsed.Negative {
		if m == "" || now.Sub(t) >= visionNegativeTTL {
			continue
		}
		s.negative[m] = t
	}
}

// saveLocked 原子落盘（tmp + rename）。调用方必须已持有 mu。
func (s *visionLearnedStore) saveLocked() {
	s.dirty = true
	s.flushIfDirtyLocked()
}

func (s *visionLearnedStore) flushIfDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushIfDirtyLocked()
}

func (s *visionLearnedStore) flushIfDirtyLocked() {
	if !s.dirty || s.path == "" {
		return
	}
	payload := visionLearnedFileData{
		Version:  1,
		Positive: s.positive,
		Negative: s.negative,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return
	}
	s.dirty = false
}
