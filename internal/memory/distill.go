package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// DistillerConfig 控制每日蒸馏行为（P1-3）。
type DistillerConfig struct {
	// LogDir 每日日志目录（<cortex>/daily）
	LogDir string
	// StateFile 记录「上次蒸馏日期」的状态文件，保证每日至多跑一次
	StateFile string
	// MemoryMDPath 蒸馏结果的目标 MEMORY.md（分节合并写入）
	MemoryMDPath string
	// Retention 日志保留期，超过该天数的日志参与蒸馏（默认 30 天）
	Retention time.Duration
	// MinChars 触发 LLM 摘要的最小原始文本量，低于该值用基础摘要（默认 200）
	MinChars int
}

// DefaultDistillerConfig fills defaults for empty fields.
func (c DistillerConfig) withDefaults() DistillerConfig {
	if c.Retention <= 0 {
		c.Retention = 30 * 24 * time.Hour
	}
	if c.MinChars <= 0 {
		c.MinChars = 200
	}
	if c.StateFile == "" {
		c.StateFile = filepath.Join(c.LogDir, ".last_distill")
	}
	return c
}

// Distiller 把超过保留期的每日日志蒸馏成摘要（P1-3）：
//
//	旧日志 → LLM 摘要（失败回退基础摘要）→ 「## Conversation Digest」
//	分节合并进 MEMORY.md → 同时写一条 agent 记忆 → 删除旧日志。
//
// 每日至多执行一次（.last_distill 状态文件）。LLM 不可用、记忆库为空
// 均可独立运行——这是把 AutoSummarize 从「配置死置」变成真实维护循环
// 的关键组件。
type Distiller struct {
	cfg   DistillerConfig
	log   *DailyLog
	store *Store            // 可为 nil
	fts   *FTSStore         // 可为 nil
	prov  provider.Provider // 可为 nil（回退基础摘要）
}

// NewDistiller creates a distiller. store/fts/prov are all optional;
// each missing component degrades the corresponding step.
func NewDistiller(cfg DistillerConfig, store *Store, fts *FTSStore, prov provider.Provider) *Distiller {
	cfg = cfg.withDefaults()
	return &Distiller{
		cfg:   cfg,
		log:   NewDailyLog(cfg.LogDir),
		store: store,
		fts:   fts,
		prov:  prov,
	}
}

// RunIfNeeded runs the distillation at most once per calendar day.
// Safe to call from a background ticker; all failures are logged, not
// propagated (maintenance must never break the host process).
func (d *Distiller) RunIfNeeded() error {
	if d.alreadyRanToday() {
		return nil
	}

	oldFiles, err := d.log.FilesOlderThan(d.cfg.Retention)
	if err != nil {
		log.Warnf("[Memory] distiller list old logs failed: %v", err)
		return nil
	}

	if len(oldFiles) == 0 {
		d.markDone()
		return nil
	}

	content, err := d.log.ReadFiles(oldFiles)
	if err != nil {
		log.Warnf("[Memory] distiller read logs failed: %v", err)
		return nil
	}
	content = strings.TrimSpace(content)

	var digest string
	switch {
	case content == "":
		digest = ""
	case len([]rune(content)) >= d.cfg.MinChars && d.prov != nil:
		digest = d.llmSummarize(content)
		if digest == "" {
			digest = d.basicDigest(content)
		}
	default:
		digest = d.basicDigest(content)
	}

	if digest != "" {
		if !d.writeDigest(digest) {
			// 摘要未能持久化到任何介质：保留旧日志，等下轮重试，
			// 避免删了原始日志后摘要永久丢失。
			log.Warnf("[Memory] distiller digest not persisted anywhere, keeping %d old log(s) for retry", len(oldFiles))
			d.markDone()
			return nil
		}
	}

	if err := d.log.DeleteFiles(oldFiles); err != nil {
		log.Warnf("[Memory] distiller delete old logs failed: %v", err)
	}

	d.markDone()
	log.Infof("[Memory] distilled %d daily log file(s) into conversation digest", len(oldFiles))
	return nil
}

// StartMaintenance starts a background ticker running RunIfNeeded every
// interval (recommended: 1h). Stops when the stop channel is closed.
func (d *Distiller) StartMaintenance(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = d.RunIfNeeded()
			case <-stop:
				return
			}
		}
	}()
}

// alreadyRanToday checks the state file for today's date.
func (d *Distiller) alreadyRanToday() bool {
	data, err := os.ReadFile(d.cfg.StateFile)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == time.Now().Format("2006-01-02")
}

// markDone records today's date as distilled.
func (d *Distiller) markDone() {
	os.MkdirAll(filepath.Dir(d.cfg.StateFile), 0755)
	_ = os.WriteFile(d.cfg.StateFile, []byte(time.Now().Format("2006-01-02")), 0644)
}

// llmSummarize asks the LLM for a compact digest. Returns "" on any failure
// (caller falls back to basicDigest).
func (d *Distiller) llmSummarize(content string) string {
	// 蒸馏输入限制：日志可能很大，喂给 LLM 的部分截到前 6000 字符
	input := content
	if r := []rune(input); len(r) > 6000 {
		input = string(r[:6000])
	}

	prompt := "Summarize the following conversation log into a concise digest for long-term memory. " +
		"Keep durable facts, decisions, preferences and open questions; drop chit-chat and transient details. " +
		"Write 3-8 short bullet lines in the same language as the log. Output ONLY the bullets.\n\n" +
		"--- conversation log ---\n" + input

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := d.prov.Chat(ctx, []provider.Message{{Role: "user", Content: prompt}})
	if err != nil {
		log.Warnf("[Memory] distiller LLM summarize failed, falling back: %v", err)
		return ""
	}
	digest := strings.TrimSpace(resp.Content)
	if digest == "" {
		return ""
	}
	return digest
}

// basicDigest produces a crude digest without an LLM: keep the newest
// portion of the log, cut at a rune-safe boundary.
func (d *Distiller) basicDigest(content string) string {
	const maxDigestChars = 1200
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	// 取末尾最多 40 行（越新越相关）
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	digest := "## Conversation Digest\n\n" + strings.Join(lines, "\n")
	if r := []rune(digest); len(r) > maxDigestChars {
		digest = string(r[:maxDigestChars]) + "..."
	}
	return digest
}

// writeDigest merges the digest into MEMORY.md (section-aware) and
// best-effort persists it into the structured store / FTS.
// Returns true if the digest was persisted to at least one medium.
func (d *Distiller) writeDigest(digest string) bool {
	persisted := false
	entry := "## Conversation Digest\n\n" + digest

	if d.cfg.MemoryMDPath != "" {
		var existing []byte
		if data, err := os.ReadFile(d.cfg.MemoryMDPath); err == nil {
			existing = data
		}
		merged := mergeIntoMarkdown(string(existing), entry, MemoryLimitChars, (&MemoryCompressor{}).CompressMemory)
		os.MkdirAll(filepath.Dir(d.cfg.MemoryMDPath), 0755)
		if err := os.WriteFile(d.cfg.MemoryMDPath, []byte(merged), 0644); err != nil {
			log.Warnf("[Memory] distiller write MEMORY.md failed: %v", err)
		} else {
			persisted = true
		}
	}

	if d.store != nil {
		mem := &Memory{
			Type:       TypeKnowledge,
			Content:    "Conversation digest (" + time.Now().Format("2006-01-02") + "): " + digest,
			Importance: 0.8,
			Source:     "distiller",
		}
		if err := d.store.Store(mem); err != nil {
			log.Warnf("[Memory] distiller store digest failed: %v", err)
		} else {
			persisted = true
		}
	}
	if d.fts != nil {
		if err := d.fts.AddInsight("distiller", digest, 8); err != nil {
			log.Warnf("[Memory] distiller FTS insight failed: %v", err)
		} else {
			persisted = true
		}
	}
	return persisted
}
