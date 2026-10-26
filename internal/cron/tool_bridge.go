package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// tool_bridge.go 让 cronjob 工具（internal/tool.CronJobTool）直接复用本
// Manager 的内存态 + 调度器 + 持久化，杜绝"工具自己写一份 map 格式的
// cron_jobs.json、Manager 按数组解析失败后所有任务静默丢失"的双实现冲突。
//
// 接口 CronJobManager 定义在 tool 包（tool 不能 import cron，会成环），
// 本文件是它的实现侧。

var _ tool.CronJobManager = (*Manager)(nil)

// CreateToolJob 由 cronjob 工具创建任务：校验表达式、默认启用、入库并挂上调度器。
// 返回任务 ID。
func (m *Manager) CreateToolJob(name, schedule, prompt, platform string) (string, error) {
	if schedule == "" {
		return "", fmt.Errorf("schedule is required")
	}
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if err := ValidateSchedule(schedule); err != nil {
		return "", err
	}

	job := &Job{
		ID:       uuid.New().String(),
		Name:     name,
		Schedule: schedule,
		Prompt:   prompt,
		Platform: platform,
		Enabled:  true,
	}
	if job.Name == "" {
		job.Name = "job-" + time.Now().Format("0102-150405")
	}
	if err := m.Add(job); err != nil {
		return "", err
	}
	return job.ID, nil
}

// ListToolJobs 返回工具侧的只读任务快照。
func (m *Manager) ListToolJobs() []tool.ToolJobInfo {
	jobs := m.List()
	result := make([]tool.ToolJobInfo, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, tool.ToolJobInfo{
			ID:         job.ID,
			Name:       job.Name,
			Schedule:   job.Schedule,
			Prompt:     job.Prompt,
			Platform:   job.Platform,
			Enabled:    job.Enabled,
			NextRun:    job.NextRun,
			LastRun:    job.LastRun,
			LastStatus: job.LastStatus,
		})
	}
	return result
}

// GetToolJob 按 ID 返回单个任务快照；未找到返回错误。
func (m *Manager) GetToolJob(id string) (*tool.ToolJobInfo, error) {
	job := m.GetByID(id)
	if job == nil {
		// 兼容工具按名字查询的用法
		if byName := m.Get(id); byName != nil {
			job = byName
		} else {
			return nil, fmt.Errorf("job not found: %s", id)
		}
	}
	return &tool.ToolJobInfo{
		ID:         job.ID,
		Name:       job.Name,
		Schedule:   job.Schedule,
		Prompt:     job.Prompt,
		Platform:   job.Platform,
		Enabled:    job.Enabled,
		NextRun:    job.NextRun,
		LastRun:    job.LastRun,
		LastStatus: job.LastStatus,
	}, nil
}

// RemoveToolJob 删除任务。
func (m *Manager) RemoveToolJob(id string) error {
	// GetByID/Get 只读，先确认存在以便给工具返回可读错误
	if m.GetByID(id) == nil && m.Get(id) == nil {
		return fmt.Errorf("job not found: %s", id)
	}
	return m.Remove(id)
}

// RunToolJob 立即触发一次执行（异步）。
func (m *Manager) RunToolJob(id string) error {
	job := m.GetByID(id)
	if job == nil {
		if byName := m.Get(id); byName != nil {
			job = byName
		} else {
			return fmt.Errorf("job not found: %s", id)
		}
	}
	// ctx 传 Background：RunJob 目前只按 ID 异步执行，m.ctx 在 Start 前
	// 可能还是 nil，别把 nil context 传出去。
	return m.RunJob(context.Background(), job)
}
