package skills

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RecommendationContext 推荐上下文
type RecommendationContext struct {
	UserInput      string               // 用户输入
	TaskType       string               // 任务类型 (coding, research, writing, etc.)
	AvailableTools []string             // 可用工具列表
	UserProfile    UserProfileInterface // 用户画像接口
	SessionHistory []string             // 本次会话已使用的技能
	TimeContext    string               // 时间上下文 (morning, afternoon, evening)
}

// SkillRecommendation 技能推荐结果
type SkillRecommendation struct {
	Skill        *Skill   `json:"skill"`
	Score        float64  `json:"score"`         // 推荐分数 0-100
	Reason       string   `json:"reason"`        // 推荐原因
	MatchFactors []string `json:"match_factors"` // 匹配因素
}

// UserProfileInterface 用户画像接口（解耦 cortex 包）
type UserProfileInterface interface {
	GetTechStack() []string
	GetInterests() []string
	GetWorkStyle() string
	GetPreferences() map[string]string
}

// Recommender 推荐器
type Recommender struct {
	manager          *Manager
	effectivenessMgr *EffectivenessManager
	userProfile      UserProfileInterface
}

// NewRecommender 创建推荐器
func NewRecommender(manager *Manager, effMgr *EffectivenessManager, profile UserProfileInterface) *Recommender {
	return &Recommender{
		manager:          manager,
		effectivenessMgr: effMgr,
		userProfile:      profile,
	}
}

// Recommend 推荐技能
func (r *Recommender) Recommend(ctx context.Context, recCtx *RecommendationContext, limit int) []*SkillRecommendation {
	if limit <= 0 {
		limit = 5
	}

	// 获取所有可用技能
	allSkills := r.manager.List()
	if len(allSkills) == 0 {
		return nil
	}

	// 计算每个技能的推荐分数
	recommendations := make([]*SkillRecommendation, 0, len(allSkills))
	for _, skill := range allSkills {
		// 获取技能统计数据
		var stats *SkillStatistics
		if r.effectivenessMgr != nil {
			stats = r.effectivenessMgr.GetSkillStatistics(skill.Name)
		}

		score := r.calculateScore(skill, recCtx, stats)
		if score > 0 {
			recommendations = append(recommendations, &SkillRecommendation{
				Skill:        skill,
				Score:        score,
				Reason:       r.generateReason(skill, recCtx, stats),
				MatchFactors: r.getMatchFactors(skill, recCtx),
			})
		}
	}

	// 按分数降序排序
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})

	// 返回前 limit 个推荐
	if len(recommendations) > limit {
		return recommendations[:limit]
	}
	return recommendations
}

// calculateScore 计算推荐分数
// 评分因素（权重）：
// - 输入匹配度 (30%): 技能名称、描述、标签与用户输入的匹配
// - 用户偏好匹配 (20%): 技能类别与用户技术栈、兴趣的匹配
// - 历史效果 (25%): 该技能的历史成功率、质量评分
// - 新颖性 (10%): 推荐未使用过的技能，避免重复
// - 时间适应性 (5%): 根据时间上下文调整 (如日报技能在傍晚更推荐)
// - 工具依赖 (10%): 技能所需工具是否可用
func (r *Recommender) calculateScore(skill *Skill, recCtx *RecommendationContext, stats *SkillStatistics) float64 {
	var totalScore float64

	// 1. 输入匹配度 (30%)
	inputMatchScore := r.calculateInputMatchScore(skill, recCtx.UserInput)
	totalScore += inputMatchScore * 0.30

	// 2. 用户偏好匹配 (20%)
	preferenceScore := r.calculatePreferenceScore(skill, recCtx.UserProfile)
	totalScore += preferenceScore * 0.20

	// 3. 历史效果 (25%)
	effectivenessScore := r.calculateEffectivenessScore(stats)
	totalScore += effectivenessScore * 0.25

	// 4. 新颖性 (10%)
	noveltyScore := r.calculateNoveltyScore(skill, recCtx.SessionHistory)
	totalScore += noveltyScore * 0.10

	// 5. 时间适应性 (5%)
	timeScore := r.calculateTimeScore(skill, recCtx.TimeContext)
	totalScore += timeScore * 0.05

	// 6. 工具依赖 (10%)
	toolScore := r.calculateToolScore(skill, recCtx.AvailableTools)
	totalScore += toolScore * 0.10

	return totalScore
}

// calculateInputMatchScore 计算输入匹配度分数 (0-100)
func (r *Recommender) calculateInputMatchScore(skill *Skill, userInput string) float64 {
	if userInput == "" {
		return 50.0 // 默认中等分数
	}

	userInput = strings.ToLower(userInput)
	var score float64
	var matchCount int

	// 检查技能名称匹配
	if strings.Contains(strings.ToLower(skill.Name), userInput) {
		score += 40
		matchCount++
	}

	// 检查描述匹配
	if strings.Contains(strings.ToLower(skill.Description), userInput) {
		score += 30
		matchCount++
	}

	// 检查标签匹配
	tags := skill.GetTags()
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), userInput) {
			score += 20
			matchCount++
			break
		}
	}

	// 检查类别匹配
	if strings.Contains(strings.ToLower(skill.Category), userInput) {
		score += 15
		matchCount++
	}

	// 检查内容匹配（降低权重，避免过度匹配）
	if strings.Contains(strings.ToLower(skill.Content), userInput) {
		score += 10
		matchCount++
	}

	// 如果没有匹配，返回基础分数
	if matchCount == 0 {
		return 10.0
	}

	// 根据匹配数量给予额外加分
	score += float64(matchCount) * 5

	if score > 100 {
		return 100
	}
	return score
}

// calculatePreferenceScore 计算用户偏好匹配分数 (0-100)
func (r *Recommender) calculatePreferenceScore(skill *Skill, profile UserProfileInterface) float64 {
	if profile == nil {
		return 50.0 // 默认中等分数
	}

	var score float64
	var matchCount int

	// 检查技术栈匹配
	techStack := profile.GetTechStack()
	tags := skill.GetTags()
	for _, tech := range techStack {
		techLower := strings.ToLower(tech)
		for _, tag := range tags {
			if strings.Contains(strings.ToLower(tag), techLower) {
				score += 25
				matchCount++
				break
			}
		}
		// 检查技能名称和描述
		if strings.Contains(strings.ToLower(skill.Name), techLower) ||
			strings.Contains(strings.ToLower(skill.Description), techLower) {
			score += 20
			matchCount++
		}
	}

	// 检查兴趣匹配
	interests := profile.GetInterests()
	for _, interest := range interests {
		interestLower := strings.ToLower(interest)
		for _, tag := range tags {
			if strings.Contains(strings.ToLower(tag), interestLower) {
				score += 20
				matchCount++
				break
			}
		}
	}

	// 检查工作风格匹配
	workStyle := profile.GetWorkStyle()
	if workStyle != "" {
		// 根据工作风格调整分数
		switch strings.ToLower(workStyle) {
		case "structured", "结构化":
			// 偏好有明确步骤的技能
			if skill.Metadata != nil && skill.Metadata["steps"] != nil {
				score += 15
				matchCount++
			}
		case "creative", "创造性":
			// 偏好灵活、创新类技能
			for _, tag := range tags {
				if strings.Contains(strings.ToLower(tag), "creative") ||
					strings.Contains(strings.ToLower(tag), "innovation") {
					score += 15
					matchCount++
					break
				}
			}
		case "analytical", "分析型":
			// 偏好分析、调试类技能
			for _, tag := range tags {
				if strings.Contains(strings.ToLower(tag), "analysis") ||
					strings.Contains(strings.ToLower(tag), "debug") ||
					strings.Contains(strings.ToLower(tag), "review") {
					score += 15
					matchCount++
					break
				}
			}
		}
	}

	// 基础分数
	baseScore := 30.0
	score += baseScore

	if score > 100 {
		return 100
	}
	if score < baseScore {
		return baseScore
	}
	return score
}

// calculateEffectivenessScore 计算历史效果分数 (0-100)
func (r *Recommender) calculateEffectivenessScore(stats *SkillStatistics) float64 {
	if stats == nil {
		return 50.0 // 默认中等分数
	}

	// 基于成功率计算分数
	successRate := stats.SuccessRate

	// 基于质量评分调整
	qualityBonus := (stats.AvgQuality - 50.0) / 5 // 假设质量评分是0-100

	score := successRate + qualityBonus

	// 根据使用次数给予信任度调整
	if stats.TotalInvocations < 3 {
		// 新技能，稍微降低分数以鼓励探索
		score -= 5
	} else if stats.TotalInvocations > 10 {
		// 经过验证的技能，给予额外加分
		score += 5
	}

	if score > 100 {
		return 100
	}
	if score < 0 {
		return 0
	}
	return score
}

// calculateNoveltyScore 计算新颖性分数 (0-100)
func (r *Recommender) calculateNoveltyScore(skill *Skill, sessionHistory []string) float64 {
	// 检查技能是否在本次会话中已使用
	for _, usedSkill := range sessionHistory {
		if strings.EqualFold(usedSkill, skill.Name) {
			return 10.0 // 已使用过的技能，降低分数
		}
	}

	// 检查技能使用频率（如果有效管理器可用）
	if r.effectivenessMgr != nil {
		stats := r.effectivenessMgr.GetSkillStatistics(skill.Name)
		if stats != nil && stats.TotalInvocations > 0 {
			// 根据使用频率调整
			if stats.TotalInvocations > 20 {
				return 60.0 // 常用技能，中等分数
			} else if stats.TotalInvocations > 5 {
				return 80.0 // 偶尔使用的技能，较高分数
			}
			return 95.0 // 很少使用的技能，很高分数
		}
	}

	// 全新技能，最高分数
	return 100.0
}

// calculateTimeScore 计算时间适应性分数 (0-100)
func (r *Recommender) calculateTimeScore(skill *Skill, timeContext string) float64 {
	if timeContext == "" {
		return 50.0 // 默认中等分数
	}

	// 根据技能类型和时间上下文调整分数
	timeContext = strings.ToLower(timeContext)
	skillName := strings.ToLower(skill.Name)
	tags := skill.GetTags()

	switch timeContext {
	case "morning", "早晨", "上午":
		// 早晨推荐规划、日报类技能
		for _, tag := range tags {
			if strings.Contains(strings.ToLower(tag), "planning") ||
				strings.Contains(strings.ToLower(tag), "daily") {
				return 90.0
			}
		}
		if strings.Contains(skillName, "daily") ||
			strings.Contains(skillName, "plan") {
			return 85.0
		}

	case "afternoon", "下午":
		// 下午推荐专注工作类技能
		for _, tag := range tags {
			if strings.Contains(strings.ToLower(tag), "coding") ||
				strings.Contains(strings.ToLower(tag), "development") ||
				strings.Contains(strings.ToLower(tag), "focus") {
				return 90.0
			}
		}

	case "evening", "晚上":
		// 傍晚/晚上推荐总结、回顾类技能
		for _, tag := range tags {
			if strings.Contains(strings.ToLower(tag), "summary") ||
				strings.Contains(strings.ToLower(tag), "report") ||
				strings.Contains(strings.ToLower(tag), "review") {
				return 90.0
			}
		}
		if strings.Contains(skillName, "report") ||
			strings.Contains(skillName, "summary") {
			return 85.0
		}
	}

	return 50.0
}

// calculateToolScore 计算工具依赖分数 (0-100)
func (r *Recommender) calculateToolScore(skill *Skill, availableTools []string) float64 {
	requiredTools := skill.GetTools()

	// 如果没有工具依赖，满分
	if len(requiredTools) == 0 {
		return 100.0
	}

	if len(availableTools) == 0 {
		return 0.0
	}

	// 计算可用工具匹配率
	availableSet := make(map[string]bool)
	for _, tool := range availableTools {
		availableSet[strings.ToLower(tool)] = true
	}

	matchedCount := 0
	for _, tool := range requiredTools {
		if availableSet[strings.ToLower(tool)] {
			matchedCount++
		}
	}

	matchRate := float64(matchedCount) / float64(len(requiredTools))
	return matchRate * 100
}

// getMatchFactors 获取匹配因素
func (r *Recommender) getMatchFactors(skill *Skill, recCtx *RecommendationContext) []string {
	factors := make([]string, 0)

	// 检查输入匹配
	if recCtx.UserInput != "" {
		userInput := strings.ToLower(recCtx.UserInput)
		if strings.Contains(strings.ToLower(skill.Name), userInput) ||
			strings.Contains(strings.ToLower(skill.Description), userInput) {
			factors = append(factors, "与您的输入关键词匹配")
		}
	}

	// 检查任务类型匹配
	if recCtx.TaskType != "" {
		tags := skill.GetTags()
		for _, tag := range tags {
			if strings.EqualFold(tag, recCtx.TaskType) {
				factors = append(factors, "匹配您的任务类型: "+recCtx.TaskType)
				break
			}
		}
	}

	// 检查技术栈匹配
	if recCtx.UserProfile != nil {
		techStack := recCtx.UserProfile.GetTechStack()
		tags := skill.GetTags()
		for _, tech := range techStack {
			for _, tag := range tags {
				if strings.Contains(strings.ToLower(tag), strings.ToLower(tech)) {
					factors = append(factors, "基于您的技术栈 "+tech+" 推荐")
					break
				}
			}
		}
	}

	// 检查是否是新技能
	if r.effectivenessMgr != nil {
		stats := r.effectivenessMgr.GetSkillStatistics(skill.Name)
		if stats == nil || stats.TotalInvocations == 0 {
			factors = append(factors, "新技能，可能对您有帮助")
		}
	}

	// 检查工具可用性
	if len(skill.GetTools()) > 0 && len(recCtx.AvailableTools) > 0 {
		availableSet := make(map[string]bool)
		for _, tool := range recCtx.AvailableTools {
			availableSet[strings.ToLower(tool)] = true
		}

		allToolsAvailable := true
		for _, tool := range skill.GetTools() {
			if !availableSet[strings.ToLower(tool)] {
				allToolsAvailable = false
				break
			}
		}
		if allToolsAvailable {
			factors = append(factors, "所需工具全部可用")
		}
	}

	return factors
}

// generateReason 生成推荐原因
func (r *Recommender) generateReason(skill *Skill, recCtx *RecommendationContext, stats *SkillStatistics) string {
	reasons := make([]string, 0)

	// 基于输入匹配
	if recCtx.UserInput != "" {
		userInput := strings.ToLower(recCtx.UserInput)
		if strings.Contains(strings.ToLower(skill.Name), userInput) ||
			strings.Contains(strings.ToLower(skill.Description), userInput) {
			reasons = append(reasons, "与您的输入关键词匹配")
		}
	}

	// 基于任务类型
	if recCtx.TaskType != "" {
		tags := skill.GetTags()
		for _, tag := range tags {
			if strings.EqualFold(tag, recCtx.TaskType) {
				reasons = append(reasons, "匹配您的任务类型: "+recCtx.TaskType)
				break
			}
		}
	}

	// 基于历史效果
	if stats != nil && stats.TotalInvocations > 0 {
		reasons = append(reasons, fmt.Sprintf("您经常使用此技能，成功率 %.0f%%", stats.SuccessRate))
	}

	// 基于用户画像
	if recCtx.UserProfile != nil {
		techStack := recCtx.UserProfile.GetTechStack()
		tags := skill.GetTags()
		for _, tech := range techStack {
			for _, tag := range tags {
				if strings.Contains(strings.ToLower(tag), strings.ToLower(tech)) {
					reasons = append(reasons, "基于您的技术栈 "+tech+" 推荐")
					break
				}
			}
		}
	}

	// 基于新颖性
	if stats == nil || stats.TotalInvocations == 0 {
		reasons = append(reasons, "新技能，可能对您有帮助")
	}

	// 选择最重要的原因
	if len(reasons) > 0 {
		return reasons[0]
	}

	return "根据您的使用习惯推荐"
}

// GetCurrentTimeContext 获取当前时间上下文
func GetCurrentTimeContext() string {
	hour := time.Now().Hour()
	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 18:
		return "afternoon"
	default:
		return "evening"
	}
}
