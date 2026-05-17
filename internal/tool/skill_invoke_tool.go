package tool

import (
	"context"
	"fmt"
)

// SkillInfoProvider 技能信息提供者接口（用于解耦循环依赖）
type SkillInfoProvider interface {
	ListSkills() []string
	GetSkillInfo(name string) (description string, tools []string, content string, err error)
}

// SkillInvokeTool 技能调用工具
type SkillInvokeTool struct {
	BaseTool
	provider SkillInfoProvider
}

// NewSkillInvokeTool 创建技能调用工具
func NewSkillInvokeTool(provider SkillInfoProvider) *SkillInvokeTool {
	return &SkillInvokeTool{
		BaseTool: *NewBaseTool(
			"skill",
			"Invoke a skill to get specialized capabilities. Each skill provides domain-specific knowledge and tool access.",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action: list, invoke, info",
						"enum":        []string{"list", "invoke", "info"},
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Skill name to invoke",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "Input to pass to the skill",
					},
				},
				"required": []string{"action"},
			},
		),
		provider: provider,
	}
}

// Execute 执行技能调用
func (t *SkillInvokeTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	action, _ := params["action"].(string)
	if action == "" {
		action = "list"
	}

	switch action {
	case "list":
		return t.listSkills()
	case "info":
		return t.getSkillInfo(params)
	case "invoke":
		return t.invokeSkill(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (t *SkillInvokeTool) listSkills() (interface{}, error) {
	if t.provider == nil {
		return map[string]interface{}{
			"skills": []interface{}{},
			"note":   "Skill provider not initialized",
		}, nil
	}

	skillNames := t.provider.ListSkills()
	skills := make([]map[string]interface{}, 0, len(skillNames))

	for _, name := range skillNames {
		desc, tools, _, err := t.provider.GetSkillInfo(name)
		if err != nil {
			continue
		}
		skills = append(skills, map[string]interface{}{
			"name":        name,
			"description": desc,
			"has_tools":   len(tools) > 0,
		})
	}

	return map[string]interface{}{
		"skills": skills,
		"count":  len(skills),
	}, nil
}

func (t *SkillInvokeTool) getSkillInfo(params map[string]interface{}) (interface{}, error) {
	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("skill name is required for info action")
	}

	if t.provider == nil {
		return nil, fmt.Errorf("skill provider not initialized")
	}

	desc, tools, content, err := t.provider.GetSkillInfo(name)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"name":        name,
		"description": desc,
		"content":     content,
		"tools":       tools,
	}, nil
}

func (t *SkillInvokeTool) invokeSkill(params map[string]interface{}) (interface{}, error) {
	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("skill name is required for invoke action")
	}

	input, _ := params["input"].(string)

	if t.provider == nil {
		return nil, fmt.Errorf("skill provider not initialized")
	}

	desc, tools, content, err := t.provider.GetSkillInfo(name)
	if err != nil {
		return nil, err
	}

	// 返回技能内容和工具信息
	result := map[string]interface{}{
		"skill_name":  name,
		"skill_desc":  desc,
		"content":     content,
		"tools":       tools,
		"input_given": input,
	}

	return result, nil
}

// ValidateParams 实现 ParamValidator 接口
func (t *SkillInvokeTool) ValidateParams(params map[string]interface{}) error {
	return nil
}
