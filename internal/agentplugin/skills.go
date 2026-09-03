package agentplugin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/skills/parser"
)

// SkillSourceAgentPlugin 标记来自 Agent Plugin 的 skill,便于集成层区分来源。
const SkillSourceAgentPlugin skills.SkillSource = "agentplugin"

// discoverSkills 扫描 pluginRoot/skills/ 的直接子目录,加载每个含常规 SKILL.md 的子目录。
//
// 规范发现规则:
//   - skills/ 缺失不是错误。
//   - skills/ 类型错误(非目录)→ 仅 skills 组件类型无效,返回错误让调用方记录。
//   - 仅扫描直接子目录,不递归后代。
//   - 单个 skill 不合规(无/非常规 SKILL.md、SKILL.md 逃逸根、解析失败)→ 跳过该 skill 继续。
func discoverSkills(pluginRoot string) ([]SkillRef, []string, error) {
	skillsDir := filepath.Join(pluginRoot, skillsDirName)

	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil // 缺失固定位置:不是错误
		}
		return nil, nil, err
	}
	if !info.IsDir() {
		// skills/ 存在但不是目录:该组件类型无效。
		return nil, nil, fmt.Errorf("%s is not a directory", skillsDirName)
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", skillsDirName, err)
	}

	var refs []SkillRef
	var warnings []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue // 仅直接子目录
		}
		ref := loadOneSkill(filepath.Join(skillsDir, entry.Name()), pluginRoot)
		if ref.Error != "" {
			warnings = append(warnings, fmt.Sprintf("skill %q skipped: %s", entry.Name(), ref.Error))
		}
		refs = append(refs, ref)
	}
	return refs, warnings, nil
}

// loadOneSkill 加载单个 skill 子目录。
func loadOneSkill(skillDir, pluginRoot string) SkillRef {
	name := filepath.Base(skillDir)
	ref := SkillRef{Name: name, Dir: skillDir}

	skillMdPath := filepath.Join(skillDir, skillMdName)
	info, err := os.Stat(skillMdPath)
	if err != nil {
		ref.Error = fmt.Sprintf("no %s: %v", skillMdName, err)
		return ref
	}
	if !info.Mode().IsRegular() {
		ref.Error = fmt.Sprintf("%s is not a regular file", skillMdName)
		return ref
	}

	// 解析符号链接后校验 SKILL.md 不逃逸插件根。
	resolved, err := filepath.EvalSymlinks(skillMdPath)
	if err != nil {
		ref.Error = fmt.Sprintf("resolve %s: %v", skillMdName, err)
		return ref
	}
	if err := ensureWithin(resolved, pluginRoot); err != nil {
		ref.Error = fmt.Sprintf("%s escapes plugin root", skillMdName)
		return ref
	}

	// 复用统一 parser 解析(支持 Hermes/OpenClaw 等格式)。
	p := parser.NewParser()
	result, err := p.Parse(skillDir)
	if err != nil || result == nil {
		// parser 失败时退化为原始 markdown 加载,避免因格式未识别而丢弃整个 skill。
		ref.Error = ""
		if data, rerr := os.ReadFile(skillMdPath); rerr == nil {
			ref.Skill = &skills.Skill{
				SkillMeta: skills.SkillMeta{Name: name, Source: SkillSourceAgentPlugin},
				Content:   string(data),
				Dir:       skillDir,
				Metadata:  make(map[string]any),
			}
		} else {
			ref.Error = fmt.Sprintf("parse and fallback read failed: %v", err)
		}
		return ref
	}

	sk := &skills.Skill{
		SkillMeta: skills.SkillMeta{
			Name:        firstNonEmpty(result.Name, name),
			Description: extractStr(result.Data, "description", ""),
			Version:     extractStr(result.Data, "version", ""),
			Author:      extractStr(result.Data, "author", ""),
			License:     extractStr(result.Data, "license", ""),
			Tags:        extractStrSlice(result.Data, "tags"),
			Source:      SkillSourceAgentPlugin,
		},
		Tools:    extractStrSlice(result.Data, "tools"),
		Dir:      skillDir,
		Metadata: make(map[string]any),
	}
	// 读取原始 SKILL.md 内容作为 Content(parser 不直接返回原始正文)。
	if data, rerr := os.ReadFile(skillMdPath); rerr == nil {
		sk.Content = string(data)
	}
	// 透传 parser 解析出的额外字段。
	for k, v := range result.Data {
		switch k {
		case "description", "version", "author", "license", "tags", "tools":
		default:
			sk.Metadata[k] = v
		}
	}
	// 扫描 scripts/ 子目录(与 skills.Manager 行为一致)。
	scriptsDir := filepath.Join(skillDir, "scripts")
	if sEntries, serr := os.ReadDir(scriptsDir); serr == nil {
		for _, e := range sEntries {
			if !e.IsDir() {
				sk.Scripts = append(sk.Scripts, filepath.Join("scripts", e.Name()))
			}
		}
	}
	ref.Skill = sk
	return ref
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func extractStr(data map[string]any, key, def string) string {
	if data == nil {
		return def
	}
	if v, ok := data[key].(string); ok && v != "" {
		return v
	}
	return def
}

func extractStrSlice(data map[string]any, key string) []string {
	if data == nil {
		return nil
	}
	switch v := data[key].(type) {
	case []string:
		return v
	case []any:
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
