package environments

import (
	"encoding/json"
	"fmt"
	"os"
)

// Environment RL 训练环境接口
type Environment interface {
	Reset() (map[string]interface{}, error)
	Step(action map[string]interface{}) (map[string]interface{}, error)
	Observation() map[string]interface{}
	Close() error
}

// ToolEnv 基于工具的RL环境
type ToolEnv struct {
	tools []string
	state map[string]interface{}
}

// NewToolEnv 创建工具使用环境
func NewToolEnv(tools []string) *ToolEnv {
	return &ToolEnv{
		tools: tools,
		state: make(map[string]interface{}),
	}
}

func (e *ToolEnv) Reset() (map[string]interface{}, error) {
	e.state = map[string]interface{}{
		"step": 0,
		"task": "Use tools to complete the task",
	}
	return e.state, nil
}

func (e *ToolEnv) Step(action map[string]interface{}) (map[string]interface{}, error) {
	// 执行动作（工具调用）
	if toolName, ok := action["tool"].(string); ok {
		e.state["last_tool"] = toolName
		e.state["step"] = e.state["step"].(int) + 1

		// 计算奖励
		reward := 0.0
		for _, t := range e.tools {
			if t == toolName {
				reward = 1.0
				break
			}
		}

		return map[string]interface{}{
			"observation": fmt.Sprintf("Executed: %s", toolName),
			"reward":      reward,
			"done":        e.state["step"].(int) >= 10,
		}, nil
	}

	return map[string]interface{}{
		"observation": "No tool specified",
		"reward":      -0.1,
		"done":        false,
	}, nil
}

func (e *ToolEnv) Observation() map[string]interface{} {
	return e.state
}

func (e *ToolEnv) Close() error {
	e.state = nil
	return nil
}

// Trajectory 轨迹
type Trajectory struct {
	TaskID      string           `json:"task_id"`
	Steps       []TrajectoryStep `json:"steps"`
	FinalReward float64          `json:"final_reward"`
}

// TrajectoryStep 轨迹步骤
type TrajectoryStep struct {
	Observation map[string]interface{} `json:"observation"`
	Action      map[string]interface{} `json:"action"`
	Reward      float64                `json:"reward"`
}

// SaveTrajectory 保存轨迹
func SaveTrajectory(traj *Trajectory, path string) error {
	data, err := json.MarshalIndent(traj, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	return nil
}
