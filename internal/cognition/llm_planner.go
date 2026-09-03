package cognition

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/perception"
	"github.com/magicwubiao/go-magic/internal/provider"
)

// LLMPlanner uses LLM for intelligent task planning
type LLMPlanner struct {
	provider provider.Provider
}

// NewLLMPlanner creates a new LLM-based planner
func NewLLMPlanner(prov provider.Provider) *LLMPlanner {
	return &LLMPlanner{
		provider: prov,
	}
}

// planningPrompt is the prompt for task decomposition
const planningPrompt = `You are an expert task planner. Decompose complex tasks into clear, actionable steps.

Given a user task, create a detailed execution plan in JSON format.

Task: %s

Respond ONLY with valid JSON in this exact format (no markdown, no explanation):
{
  "title": "Brief task title",
  "description": "What this plan accomplishes",
  "complexity": "simple|medium|advanced",
  "max_turns": number,
  "enable_sub_agents": true|false,
  "steps": [
    {
      "id": 1,
      "description": "Step description",
      "tools": ["tool1", "tool2"],
      "priority": 1-3,
      "estimated_turns": number,
      "dependencies": []
    }
  ],
  "retrieval_hints": ["hint1", "hint2"],
  "clarification_needed": false,
  "clarification_question": ""
}

Guidelines:
- Break down complex tasks into 3-8 steps
- Each step should be independently verifiable
- Consider dependencies between steps
- Estimate turns realistically (1-3 per step)
- Identify tools needed for each step`

// CreatePlan uses LLM to create an execution plan
func (p *LLMPlanner) CreatePlan(ctx context.Context, task string) (*Decision, error) {
	// Call LLM for planning
	messages := []provider.Message{
		{Role: "system", Content: planningPrompt},
		{Role: "user", Content: fmt.Sprintf(planningPrompt, task)},
	}

	type openAIlike interface {
		Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error)
	}

	var resp *provider.ChatResponse
	var err error

	if oa, ok := p.provider.(openAIlike); ok {
		resp, err = oa.Chat(ctx, messages)
	} else {
		return nil, fmt.Errorf("provider does not support chat")
	}

	if err != nil {
		return nil, fmt.Errorf("LLM planning failed: %w", err)
	}

	// Parse JSON response
	return p.parseLLMResponse(resp.Content, task)
}

// parseLLMResponse parses the LLM JSON response into a Decision
func (p *LLMPlanner) parseLLMResponse(content, originalTask string) (*Decision, error) {
	// Clean up markdown code blocks if present
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var planResp struct {
		Title           string `json:"title"`
		Description     string `json:"description"`
		Complexity      string `json:"complexity"`
		MaxTurns        int    `json:"max_turns"`
		EnableSubAgents bool   `json:"enable_sub_agents"`
		Steps           []struct {
			ID             int      `json:"id"`
			Description    string   `json:"description"`
			Tools          []string `json:"tools"`
			Priority       int      `json:"priority"`
			EstimatedTurns int      `json:"estimated_turns"`
			Dependencies   []int    `json:"dependencies"`
		} `json:"steps"`
		RetrievalHints        []string `json:"retrieval_hints"`
		ClarificationNeeded   bool     `json:"clarification_needed"`
		ClarificationQuestion string   `json:"clarification_question"`
	}

	if err := json.Unmarshal([]byte(content), &planResp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Convert to Decision
	decision := &Decision{
		Plan: &ExecutionPlan{
			Description:         planResp.Description,
			OriginalInput:       originalTask,
			Steps:               make([]Step, len(planResp.Steps)),
			TotalEstimatedTurns: planResp.MaxTurns,
			CreatedAt:           time.Now().Unix(),
			ModifiedAt:          time.Now().Unix(),
			IsDynamic:           true,
		},
		MaxTurns:        planResp.MaxTurns,
		EnableSubAgents: planResp.EnableSubAgents,
		RetrievalHints:  make([]RetrievalHint, 0),
		ContextHints:    planResp.RetrievalHints,
	}

	// Set complexity (map string to perception.TaskComplexity)
	switch planResp.Complexity {
	case "advanced":
		decision.Plan.Complexity = perception.ComplexityAdvanced
	case "medium":
		decision.Plan.Complexity = perception.ComplexityMedium
	default:
		decision.Plan.Complexity = perception.ComplexitySimple
	}

	// Convert steps
	for i, s := range planResp.Steps {
		decision.Plan.Steps[i] = Step{
			ID:             s.ID,
			Description:    s.Description,
			Tools:          s.Tools,
			Priority:       s.Priority,
			EstimatedTurns: s.EstimatedTurns,
			Dependencies:   s.Dependencies,
			Status:         StepPending,
		}
	}

	// Add retrieval hints
	for _, hint := range planResp.RetrievalHints {
		decision.RetrievalHints = append(decision.RetrievalHints, RetrievalHint{
			Type:      "history",
			Query:     hint,
			Relevance: 0.8,
		})
	}

	// Set clarification
	decision.ClarificationNeeded = planResp.ClarificationNeeded
	decision.ClarificationQuestion = planResp.ClarificationQuestion

	return decision, nil
}

// AdjustPlanWithFeedback adjusts plan based on execution feedback
func (p *LLMPlanner) AdjustPlanWithFeedback(ctx context.Context, plan *ExecutionPlan, feedback string) (*PlanAdjustment, error) {
	adjustmentPrompt := fmt.Sprintf(`Analyze execution feedback and suggest plan adjustments.

Original Task: %s
Execution Feedback: %s

Current Steps:
%s

Respond with JSON for adjustments:
{
  "reason": "Why adjustments are needed",
  "steps_to_add": [{"description": "", "tools": [], "priority": 1}],
  "steps_to_modify": [{"id": 1, "new_description": ""}],
  "retry_step_id": null or number
}`, plan.OriginalInput, feedback, p.stepsToJSON(plan.Steps))

	messages := []provider.Message{
		{Role: "user", Content: adjustmentPrompt},
	}

	type openAIlike interface {
		Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error)
	}

	var resp *provider.ChatResponse
	var err error

	if oa, ok := p.provider.(openAIlike); ok {
		resp, err = oa.Chat(ctx, messages)
	} else {
		return nil, fmt.Errorf("provider does not support chat")
	}

	if err != nil {
		return nil, err
	}

	return p.parseAdjustment(resp.Content, plan)
}

func (p *LLMPlanner) stepsToJSON(steps []Step) string {
	var parts []string
	for _, s := range steps {
		parts = append(parts, fmt.Sprintf("Step %d: %s (status: %s)", s.ID, s.Description, s.Status))
	}
	return strings.Join(parts, "\n")
}

func (p *LLMPlanner) parseAdjustment(content string, plan *ExecutionPlan) (*PlanAdjustment, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var adjResp struct {
		Reason     string `json:"reason"`
		StepsToAdd []struct {
			Description string   `json:"description"`
			Tools       []string `json:"tools"`
			Priority    int      `json:"priority"`
		} `json:"steps_to_add"`
		RetryStepID *int `json:"retry_step_id"`
	}

	if err := json.Unmarshal([]byte(content), &adjResp); err != nil {
		return nil, err
	}

	adjustment := &PlanAdjustment{
		Reason:     adjResp.Reason,
		AdjustedAt: time.Now().Unix(),
	}

	// Add new steps
	nextID := len(plan.Steps) + 1
	for _, s := range adjResp.StepsToAdd {
		newStep := Step{
			ID:             nextID,
			Description:    s.Description,
			Tools:          s.Tools,
			Priority:       s.Priority,
			EstimatedTurns: 2,
			Dependencies:   []int{},
			Status:         StepPending,
		}
		plan.Steps = append(plan.Steps, newStep)
		adjustment.StepAdded = append(adjustment.StepAdded, newStep)
		nextID++
	}

	plan.ModifiedAt = time.Now().Unix()

	return adjustment, nil
}
