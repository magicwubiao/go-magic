package server

import (
	"net/http"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/provider"
)

// 会话上下文用量接口。
//
//	GET /api/sessions/{id}/context
//
// chat 页输入框的"上下文用量"浮层数据源：返回当前会话发往模型时上下文
// 的分类 token 估算（与 agent 压缩触发同口径：字节/4）与模型窗口上限，
// 前端据此渲染总百分比与各分类明细。
//
// 会话还没有 agent（从未发过消息 / 服务重启后未恢复）时按全 0 返回，
// 前端照常展示 0% —— 新会话打开浮层是正常操作，不该是一次可见的报错。
// 窗口上限与 /api/model/info 同一口径：Modeler → pkg/catalog → 128K 兜底。
func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 与 handleModelInfo 相同的模型解析路径：配置里的当前模型，再被共享
	// provider 的 Modeler 覆盖（provider 是进程级共享的，所有 agent 同模型）。
	s.syncConfigFromDisk()
	cfg := s.cfgSnapshot()
	modelName := ""
	if cfg != nil {
		modelName = cfg.GetCurrentModel()
	}
	if s.provider != nil {
		if modeler, ok := provider.GetModeler(s.provider); ok && modeler.GetModel() != "" {
			modelName = modeler.GetModel()
		}
	}

	contextLen := provider.DefaultModelContextLen
	if n := provider.ModelContextLen(modelName); n > 0 {
		contextLen = n
	}

	var breakdown agent.ContextBreakdown
	if a := s.lookupAgent(sessionID); a != nil {
		breakdown = a.ContextBreakdown()
	}

	jsonResponse(w, map[string]interface{}{
		"session_id":   sessionID,
		"model":        modelName,
		"context_len":  contextLen,
		"total_tokens": breakdown.Total,
		"breakdown":    breakdown,
	})
}
