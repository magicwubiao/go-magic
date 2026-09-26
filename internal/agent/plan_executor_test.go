package agent

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/cognition"
	"github.com/magicwubiao/go-magic/internal/provider"
)

func newPlanExecutorForTest(estimatedTurns int) *PlanExecutor {
	pe := NewPlanExecutor(nil, PlanExecutorConfig{})
	pe.plan = &cognition.ExecutionPlan{
		Description: "test",
		Steps: []cognition.Step{
			{
				ID:             1,
				Description:    "step one",
				EstimatedTurns: estimatedTurns,
				Status:         cognition.StepPending,
			},
			{
				ID:             2,
				Description:    "step two",
				EstimatedTurns: 1,
				Dependencies:   []int{1},
				Status:         cognition.StepPending,
			},
		},
	}
	return pe
}

func TestDetectStepCompletionOnlyUsesLatestToolBatch(t *testing.T) {
	pe := newPlanExecutorForTest(1)
	history := []provider.Message{
		{Role: "tool", Content: "previous step succeeded"},
		{Role: "assistant", Content: "Now working on the next step"},
	}
	if pe.DetectStepCompletion(history, nil) {
		t.Fatal("a successful tool call from an earlier step must not complete the current step")
	}

	history = append(history,
		provider.Message{Role: "assistant", Content: "tool call"},
		provider.Message{Role: "tool", Content: "Error: failed"},
	)
	if pe.DetectStepCompletion(history, nil) {
		t.Fatal("a failed latest tool batch must not complete the current step")
	}

	history = append(history, provider.Message{Role: "tool", Content: "current step succeeded"})
	if !pe.DetectStepCompletion(history, nil) {
		t.Fatal("a successful tool result in the latest batch should complete a one-turn step")
	}
}

func TestDetectStepCompletionRequiresEstimatedSuccessfulCalls(t *testing.T) {
	pe := newPlanExecutorForTest(2)
	history := []provider.Message{
		{Role: "tool", Content: "one success"},
	}
	if pe.DetectStepCompletion(history, nil) {
		t.Fatal("one successful tool result must not complete a two-turn step")
	}
	history = append(history, provider.Message{Role: "tool", Content: "second success"})
	if !pe.DetectStepCompletion(history, nil) {
		t.Fatal("two successful tool results should complete a two-turn step")
	}
}

func TestMarkStepCompleteIsIdempotent(t *testing.T) {
	pe := newPlanExecutorForTest(1)
	pe.MarkStepComplete(1)
	pe.MarkStepComplete(1)

	if got := pe.StepsCompletedCount(); got != 1 {
		t.Fatalf("expected one completed step after duplicate update, got %d", got)
	}
	if pe.IsPlanComplete() {
		t.Fatal("plan must not be complete after completing only one of two steps")
	}
	if got := pe.GetProgress(); got != 0.5 {
		t.Fatalf("expected progress 0.5 after completing one of two steps, got %v", got)
	}
}
