# Magic AI Agent 规范

## 项目概述
- **项目名称**: Magic AI (go-magic)
- **技术栈**: Go, Vue.js, SQLite
- **语言**: 中文
- **运行时**: Go 1.26+

## 核心模块

### 记忆系统 (Cortex)
- **路径**: `internal/cortex/`
- **功能**: 用户画像、上下文管理、记忆提取
- **配置**: `Memory.Enabled`, `CortexEnabled` (默认启用)

### 审批系统 (Approval)
- **路径**: `internal/approval/`
- **策略**: Smart (记录审批历史), AutoApprove, RequireApproval
- **触发**: 高风险操作 (execute_command, write_file, file_edit)
- **配置**: `approval.Strategy` (默认 Smart)

### 技能系统 (Skills)
- **路径**: `internal/skills/`
- **功能**: 技能管理、自动创建、嵌入向量
- **配置**: `Skills.AutoSkillCreation` (默认启用)

### 看板系统 (Kanban)
- **路径**: `internal/kanban/`
- **功能**: 任务管理、子任务拆分、AI 充实
- **前端**: `web/src/views/KanbanView.vue`
- **翻译**: `web/src/locales/{en,zh}.ts`

### Agent 系统
- **路径**: `internal/agent/`
- **功能**: 对话、工具执行、Hook 系统
- **Cortex 集成**: 自动注入记忆上下文

## 关键配置

```json
{
  "memory": { "enabled": true },
  "cortex_enabled": true,
  "approval": {
    "strategy": "smart"
  },
  "skills": {
    "auto_skill_creation": true
  }
}
```

## 常见问题

### 记忆不生效
检查配置文件中 `memory.enabled` 和 `cortex_enabled` 是否为 true

### Approval 为空
确认 `approval.strategy` 不是 `auto_approve`

## 提交规范
- feat: 新功能
- fix: 修复
- refactor: 重构
- docs: 文档
- test: 测试
