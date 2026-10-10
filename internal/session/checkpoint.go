package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Checkpoint represents a session checkpoint for recovery
type Checkpoint struct {
	SessionID   string          `json:"session_id"`
	Platform    string          `json:"platform"`
	ChannelID   string          `json:"channel_id"`
	UserID      string          `json:"user_id"`
	Messages    []types.Message `json:"messages"`
	AgentState  json.RawMessage `json:"agent_state,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Interrupted bool            `json:"interrupted"` // true if gateway was shutdown while session was active
}

// checkpointFileLine 是 checkpoint 文件（JSONL，每行一条）里的一行。
//
// 与旧的"整包 JSON 重写"相比，这里只在 `Full=true` 的那一行携带完整历史；
// 之后的常规行只携带**自上次落盘以来新增的消息**。delta 之和恰好等于一份完整
// 快照，所以磁盘占用不增，但每次落盘从"重新序列化整段历史 + 整包重写"降到
// "只写新增部分"。历史被压缩/裁剪（前缀对不上）时退化为 Full 行重设基线。
type checkpointFileLine struct {
	Full        bool            `json:"full,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Platform    string          `json:"platform,omitempty"`
	ChannelID   string          `json:"channel_id,omitempty"`
	UserID      string          `json:"user_id,omitempty"`
	AgentState  json.RawMessage `json:"agent_state,omitempty"`
	Interrupted *bool           `json:"interrupted,omitempty"`
	Messages    []types.Message `json:"messages,omitempty"`
	CreatedAt   time.Time       `json:"created_at,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// checkpointCompactMinLines / checkpointCompactMinBytes 是 compaction 的触发门槛。
// delta 行数达到下限**且**文件体量达到下限时才整理成单行 Full。
// 用 var 而非 const：测试需要把它们调小才能覆盖这条路径。
var (
	checkpointCompactMinLines = 64
	checkpointCompactMinBytes = 1 << 20 // 1 MiB
)

// CheckpointManager manages session checkpoints
type CheckpointManager struct {
	dir string // ~/.magic/checkpoints/
	mu  sync.RWMutex
	// persisted 缓存每个会话"已落盘"的重建结果，供 Save 计算增量（否则每次保存
	// 都要回读整个文件才能知道前缀）。只在 mu 内访问；进程重启后为空 ⇒ 首次保存
	// 写 Full 行。
	persisted map[string]*Checkpoint
	// lineCount / lineBytes 记录各会话当前文件里的行数与最后一行字节数，用于触发 compaction。
	lineCount map[string]int
	lineBytes map[string]int
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager() (*CheckpointManager, error) {
	dir := filepath.Join(config.GetMagicHome(), "checkpoints")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	return &CheckpointManager{
		dir:       dir,
		persisted: make(map[string]*Checkpoint),
		lineCount: make(map[string]int),
		lineBytes: make(map[string]int),
	}, nil
}

// safeCheckpointID 把 sessionID 规整为文件系统安全名。
func safeCheckpointID(sessionID string) string {
	return strings.ReplaceAll(sessionID, "/", "_")
}

// checkpointPath returns the JSONL file path for a session checkpoint.
func (cm *CheckpointManager) checkpointPath(sessionID string) string {
	return filepath.Join(cm.dir, safeCheckpointID(sessionID)+".jsonl")
}

// legacyCheckpointPath 是旧版（整包 JSON 对象）的路径，只用于读取与迁移。
func (cm *CheckpointManager) legacyCheckpointPath(sessionID string) string {
	return filepath.Join(cm.dir, safeCheckpointID(sessionID)+".json")
}

// cloneCheckpoint 深拷贝 Messages 切片，避免缓存与调用方共享底层数组。
func cloneCheckpoint(cp *Checkpoint) *Checkpoint {
	if cp == nil {
		return nil
	}
	out := *cp
	out.Messages = append([]types.Message(nil), cp.Messages...)
	return &out
}

// sameCheckpointMessage 比较两条消息的**持久化相关字段**。
// 不比较 Timestamp/FileOps 等展示字段：它们不参与"前缀是否未变"的判定。
func sameCheckpointMessage(a, b types.Message) bool {
	return a.ID == b.ID &&
		a.Role == b.Role &&
		a.Content == b.Content &&
		a.ToolCallID == b.ToolCallID &&
		len(a.ToolCalls) == len(b.ToolCalls) &&
		len(a.ContentParts) == len(b.ContentParts)
}

// isCheckpointPrefix 判断 prev 是否为 cur 的前缀。为真时只需要落盘 cur 的尾部。
func isCheckpointPrefix(prev, cur []types.Message) bool {
	if len(prev) > len(cur) {
		return false
	}
	for i := range prev {
		if !sameCheckpointMessage(prev[i], cur[i]) {
			return false
		}
	}
	return true
}

// replayCheckpointLines 逐行重放 JSONL，重建完整 checkpoint。
// 损坏行（进程在写入中途被杀留下的半行）直接跳过，不影响其余行。
func replayCheckpointLines(s string) (*Checkpoint, error) {
	var (
		cp   Checkpoint
		msgs []types.Message
		seen bool
	)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var l checkpointFileLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			continue
		}
		seen = true
		if l.Full {
			msgs = msgs[:0]
		}
		msgs = append(msgs, l.Messages...)
		if l.SessionID != "" {
			cp.SessionID = l.SessionID
		}
		if l.Platform != "" {
			cp.Platform = l.Platform
		}
		if l.ChannelID != "" {
			cp.ChannelID = l.ChannelID
		}
		if l.UserID != "" {
			cp.UserID = l.UserID
		}
		if len(l.AgentState) > 0 {
			cp.AgentState = l.AgentState
		}
		if l.Interrupted != nil {
			cp.Interrupted = *l.Interrupted
		}
		if !l.CreatedAt.IsZero() {
			cp.CreatedAt = l.CreatedAt
		}
		if !l.UpdatedAt.IsZero() {
			cp.UpdatedAt = l.UpdatedAt
		}
	}
	if !seen {
		return nil, nil
	}
	cp.Messages = msgs
	return &cp, nil
}

// readFromDisk 读取某个会话的 checkpoint：优先 JSONL，回退旧的整包 JSON。
// 都不存在时返回 (nil, nil)。
func (cm *CheckpointManager) readFromDisk(sessionID string) (*Checkpoint, error) {
	if data, err := os.ReadFile(cm.checkpointPath(sessionID)); err == nil {
		cp, perr := replayCheckpointLines(string(data))
		if perr != nil {
			return nil, perr
		}
		if cp != nil {
			cp.SessionID = sessionID
			return cp, nil
		}
		// 空文件：视作无 checkpoint。
		return nil, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// 旧格式回退。
	data, err := os.ReadFile(cm.legacyCheckpointPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// loadNoLock 读 checkpoint（调用方必须已持有 cm.mu）。命中内存缓存时直接返回。
func (cm *CheckpointManager) loadNoLock(sessionID string) (*Checkpoint, error) {
	if cp, ok := cm.persisted[sessionID]; ok && cp != nil {
		return cloneCheckpoint(cp), nil
	}
	return cm.readFromDisk(sessionID)
}

// appendLine 以 JSONL 追加一行。
func (cm *CheckpointManager) appendLine(sessionID string, line *checkpointFileLine) (int, error) {
	data, err := json.Marshal(line)
	if err != nil {
		return 0, err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(cm.checkpointPath(sessionID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return 0, err
	}
	return len(data), nil
}

// compactLocked 把 JSONL 整理成单行 Full（delta 行数过多时）。
// 调用方必须已持有 cm.mu 写锁。
func (cm *CheckpointManager) compactLocked(sessionID string, cp *Checkpoint) error {
	line := checkpointFileLine{
		Full:       true,
		SessionID:  cp.SessionID,
		Platform:   cp.Platform,
		ChannelID:  cp.ChannelID,
		UserID:     cp.UserID,
		AgentState: cp.AgentState,
		Messages:   cp.Messages,
		CreatedAt:  cp.CreatedAt,
		UpdatedAt:  cp.UpdatedAt,
	}
	interrupted := cp.Interrupted
	line.Interrupted = &interrupted

	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(cm.checkpointPath(sessionID), data, 0644); err != nil {
		return err
	}
	cm.lineCount[sessionID] = 1
	cm.lineBytes[sessionID] = len(data)
	return nil
}

// Save saves a checkpoint to disk.
//
// 写盘策略：常规路径只追加"自上次落盘以来新增的消息"（见 checkpointFileLine）。
// 历史被压缩/裁剪、或进程刚启动（无缓存基线）时写 Full 行重设基线。
func (cm *CheckpointManager) Save(cp *Checkpoint) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.saveLocked(cp)
}

// saveLocked 是 Save 的实现（调用方必须已持有 cm.mu 写锁）。
func (cm *CheckpointManager) saveLocked(cp *Checkpoint) error {
	cp.UpdatedAt = time.Now()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = cp.UpdatedAt
	}

	prev := cm.persisted[cp.SessionID]

	var (
		line    checkpointFileLine
		msgs    []types.Message
		created time.Time
	)
	if prev != nil && isCheckpointPrefix(prev.Messages, cp.Messages) {
		// 增量：前缀未变，只落盘新增部分。
		msgs = cp.Messages[len(prev.Messages):]
	} else {
		line.Full = true
		msgs = cp.Messages
		created = cp.CreatedAt
	}

	interrupted := cp.Interrupted
	line.SessionID = cp.SessionID
	line.Platform = cp.Platform
	line.ChannelID = cp.ChannelID
	line.UserID = cp.UserID
	line.AgentState = cp.AgentState
	line.Interrupted = &interrupted
	line.Messages = msgs
	line.CreatedAt = created
	line.UpdatedAt = cp.UpdatedAt

	n, err := cm.appendLine(cp.SessionID, &line)
	if err != nil {
		return err
	}

	// 更新缓存基线（深拷贝，避免调用方后续改动影响前缀判定）。
	cm.persisted[cp.SessionID] = cloneCheckpoint(cp)
	if line.Full {
		cm.lineCount[cp.SessionID] = 1
	} else {
		cm.lineCount[cp.SessionID]++
	}
	cm.lineBytes[cp.SessionID] = n

	if cm.lineCount[cp.SessionID] >= checkpointCompactMinLines && cm.estimatedFileBytesLocked(cp.SessionID) >= checkpointCompactMinBytes {
		if err := cm.compactLocked(cp.SessionID, cp); err != nil {
			log.Warnf("[Checkpoint] compaction failed for %s: %v", cp.SessionID, err)
		}
	}

	// 首次以 JSONL 落盘成功后，清掉可能存在的旧格式文件（迁移）。
	if legacy := cm.legacyCheckpointPath(cp.SessionID); legacy != "" {
		if _, statErr := os.Stat(legacy); statErr == nil {
			if rmErr := os.Remove(legacy); rmErr != nil {
				log.Warnf("[Checkpoint] failed to remove legacy checkpoint %s: %v", legacy, rmErr)
			}
		}
	}
	return nil
}

// estimatedFileBytesLocked 估算当前文件体量（调用方须持写锁）。
func (cm *CheckpointManager) estimatedFileBytesLocked(sessionID string) int {
	if info, err := os.Stat(cm.checkpointPath(sessionID)); err == nil {
		return int(info.Size())
	}
	return 0
}

// Load loads a checkpoint from disk
func (cm *CheckpointManager) Load(sessionID string) (*Checkpoint, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.loadNoLock(sessionID)
}

// Delete removes a checkpoint from disk（同时清理旧格式文件与内存基线）
func (cm *CheckpointManager) Delete(sessionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.persisted, sessionID)
	delete(cm.lineCount, sessionID)
	delete(cm.lineBytes, sessionID)

	err := os.Remove(cm.checkpointPath(sessionID))
	// 旧格式文件可能仍在（尚未迁移），一并删除；删掉任一个都算成功。
	if legacyErr := os.Remove(cm.legacyCheckpointPath(sessionID)); legacyErr == nil && os.IsNotExist(err) {
		err = nil
	}
	return err
}

// listCheckpointsLocked 扫描目录，重建所有 checkpoint（.jsonl 优先，兼容 .json）。
// 调用方必须已持有 cm.mu（读锁即可）。
func (cm *CheckpointManager) listCheckpointsLocked() []*Checkpoint {
	entries, err := os.ReadDir(cm.dir)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool, len(entries))
	var checkpoints []*Checkpoint
	// 先收 JSONL，再收尚未迁移的旧 .json（避免同名会话重复）。
	for _, suffix := range []string{".jsonl", ".json"} {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, suffix) {
				continue
			}
			id := strings.TrimSuffix(name, suffix)
			if seen[id] {
				continue
			}
			path := filepath.Join(cm.dir, name)
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				log.Warnf("[Checkpoint] Failed to read %s: %v", path, rerr)
				continue
			}
			var cp *Checkpoint
			if suffix == ".jsonl" {
				cp, rerr = replayCheckpointLines(string(data))
			} else {
				var legacy Checkpoint
				if rerr = json.Unmarshal(data, &legacy); rerr == nil {
					cp = &legacy
				}
			}
			if rerr != nil {
				log.Warnf("[Checkpoint] Failed to parse %s: %v", path, rerr)
				continue
			}
			if cp == nil {
				continue
			}
			seen[id] = true
			if cp.SessionID == "" {
				cp.SessionID = id
			}
			checkpoints = append(checkpoints, cp)
		}
	}
	return checkpoints
}

// ListInterrupted returns all checkpoints marked as interrupted
func (cm *CheckpointManager) ListInterrupted() ([]*Checkpoint, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var out []*Checkpoint
	for _, cp := range cm.listCheckpointsLocked() {
		if cp.Interrupted {
			out = append(out, cp)
		}
	}
	return out, nil
}

// ListAll returns all checkpoints
func (cm *CheckpointManager) ListAll() ([]*Checkpoint, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.listCheckpointsLocked(), nil
}

// Prune removes checkpoints older than maxAge (default 7 days)
func (cm *CheckpointManager) Prune(maxAge time.Duration) error {
	if maxAge == 0 {
		maxAge = 7 * 24 * time.Hour // Default 7 days
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	entries, err := os.ReadDir(cm.dir)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".jsonl")) {
			continue
		}

		path := filepath.Join(cm.dir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				log.Warnf("[Checkpoint] Failed to prune %s: %v", path, err)
			} else {
				log.Infof("[Checkpoint] Pruned old checkpoint: %s", name)
				id := strings.TrimSuffix(strings.TrimSuffix(name, ".jsonl"), ".json")
				delete(cm.persisted, id)
				delete(cm.lineCount, id)
				delete(cm.lineBytes, id)
			}
		}
	}

	return nil
}

// MarkInterrupted marks a session as interrupted (gateway shutdown)
// Uses atomic operation to avoid race conditions
func (cm *CheckpointManager) MarkInterrupted(sessionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cp, err := cm.loadNoLock(sessionID)
	if err != nil {
		// No checkpoint exists, nothing to mark
		return nil
	}
	if cp == nil {
		return nil
	}

	cp.Interrupted = true
	return cm.saveLocked(cp)
}

// ClearInterrupted clears the interrupted flag after successful recovery
// Uses atomic operation to avoid race conditions
func (cm *CheckpointManager) ClearInterrupted(sessionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cp, err := cm.loadNoLock(sessionID)
	if err != nil {
		return err
	}
	if cp == nil {
		return nil
	}

	cp.Interrupted = false
	return cm.saveLocked(cp)
}
