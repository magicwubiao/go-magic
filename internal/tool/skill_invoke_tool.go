package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/safepath"
)

// SkillInfoProvider 技能信息提供者接口（用于解耦循环依赖）
type SkillInfoProvider interface {
	ListSkills() []string
	GetSkillInfo(name string) (description string, tools []string, content string, err error)
}

// SkillInvokeTool 技能调用工具
// 参考她的 hermes-agent skills 工具集（skills_list / skill_view / skill_manage），
// 这里合并为单个 skill 工具：list / info / invoke / create / delete。
type SkillInvokeTool struct {
	BaseTool
	provider SkillInfoProvider
}

// NewSkillInvokeTool 创建技能调用工具
func NewSkillInvokeTool(provider SkillInfoProvider) *SkillInvokeTool {
	return &SkillInvokeTool{
		BaseTool: *NewBaseTool(
			"skill",
			"Invoke or manage skills. Skills provide domain-specific knowledge and tool access. "+
				"Actions: list (show available skills), info (view full skill content), invoke (activate a skill), "+
				"create (create or update a skill), delete (remove a skill).",
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action: list, invoke, info, create, delete",
						"enum":        []string{"list", "invoke", "info", "create", "delete"},
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Skill name to invoke / view / create / delete",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "Input to pass to the skill (for invoke)",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Skill description (for create)",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Skill content / instructions in Markdown (for create)",
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

	log.Infof("[skill] Action: %s", action)

	switch action {
	case "list":
		return t.listSkills()
	case "info":
		return t.getSkillInfo(params)
	case "invoke":
		return t.invokeSkill(params)
	case "create":
		return t.createSkill(params)
	case "delete":
		return t.deleteSkill(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// skillManager 从 provider 中提取 *skills.Manager（用于 create/delete）。
func (t *SkillInvokeTool) skillManager() (*skills.Manager, error) {
	if t.provider == nil {
		return nil, fmt.Errorf("skill provider not initialized")
	}
	mgr, ok := t.provider.(*skills.Manager)
	if !ok {
		return nil, fmt.Errorf("skill provider does not support management operations")
	}
	return mgr, nil
}

func (t *SkillInvokeTool) listSkills() (interface{}, error) {
	if t.provider == nil {
		return map[string]interface{}{
			"skills": []interface{}{},
			"note":   "Skill provider not initialized",
		}, nil
	}

	skillNames := t.provider.ListSkills()
	skillList := make([]map[string]interface{}, 0, len(skillNames))

	for _, name := range skillNames {
		desc, tools, _, err := t.provider.GetSkillInfo(name)
		if err != nil {
			continue
		}
		skillList = append(skillList, map[string]interface{}{
			"name":        name,
			"description": desc,
			"has_tools":   len(tools) > 0,
		})
	}

	return map[string]interface{}{
		"skills": skillList,
		"count":  len(skillList),
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

	log.Infof("[skill] Invoking skill: %s, input: %s", name, input)

	if t.provider == nil {
		return nil, fmt.Errorf("skill provider not initialized")
	}

	desc, tools, content, err := t.provider.GetSkillInfo(name)
	if err != nil {
		log.Warnf("[skill] Failed to get skill %s: %v", name, err)
		return nil, err
	}

	log.Infof("[skill] Skill %s invoked successfully, content length: %d", name, len(content))

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

// createSkill 创建或更新技能（hermes skill_manage 对齐）
func (t *SkillInvokeTool) createSkill(params map[string]interface{}) (interface{}, error) {
	mgr, err := t.skillManager()
	if err != nil {
		return nil, err
	}

	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("skill name is required for create action")
	}
	if err := safepath.SanitizeName(name); err != nil {
		return nil, fmt.Errorf("invalid skill name: %w", err)
	}

	content, _ := params["content"].(string)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("content is required for create action")
	}

	description, _ := params["description"].(string)
	if description == "" {
		// 取内容首行作为兜底描述
		firstLine := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
		description = firstLine
	}

	skill := &skills.Skill{
		SkillMeta: skills.SkillMeta{
			Name:        name,
			Description: description,
			Source:      skills.SkillSourceAuto,
		},
		Content: content,
	}

	existed := false
	if _, getErr := mgr.Get(name); getErr == nil {
		existed = true
	}

	if err := mgr.Add(skill); err != nil {
		return nil, fmt.Errorf("failed to save skill: %w", err)
	}

	log.Infof("[skill] Skill %s created/updated", name)
	return map[string]interface{}{
		"status":      "saved",
		"name":        name,
		"updated":     existed,
		"description": description,
	}, nil
}

// deleteSkill 删除技能（hermes skill_manage 对齐）
func (t *SkillInvokeTool) deleteSkill(params map[string]interface{}) (interface{}, error) {
	mgr, err := t.skillManager()
	if err != nil {
		return nil, err
	}

	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("skill name is required for delete action")
	}

	if err := mgr.Remove(name); err != nil {
		return nil, fmt.Errorf("failed to delete skill: %w", err)
	}

	log.Infof("[skill] Skill %s deleted", name)
	return map[string]interface{}{
		"status": "deleted",
		"name":   name,
	}, nil
}

// ValidateParams 实现 ParamValidator 接口
func (t *SkillInvokeTool) ValidateParams(params map[string]interface{}) error {
	return nil
}
