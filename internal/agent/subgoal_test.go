package agent

import (
	"testing"
)

func TestAddSubGoal_NoActiveGoal(t *testing.T) {
	gm := NewGoalManager(nil, "")
	subGoal, err := gm.AddSubGoal("add tests")
	if err == nil {
		t.Error("expected error when no active goal")
	}
	if subGoal != nil {
		t.Error("expected nil subgoal")
	}
}

func TestAddSubGoal_WithActiveGoal(t *testing.T) {
	gm := NewGoalManager(nil, "")
	
	// Set an active goal
	gm.SetGoal("implement feature X")
	
	subGoal, err := gm.AddSubGoal("add unit tests")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subGoal == nil {
		t.Fatal("expected non-nil subgoal")
	}
	if subGoal.Text != "add unit tests" {
		t.Errorf("expected text 'add unit tests', got '%s'", subGoal.Text)
	}
	if subGoal.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestGetSubGoals(t *testing.T) {
	gm := NewGoalManager(nil, "")
	
	// Initially no subgoals
	goals := gm.GetSubGoals()
	if goals != nil {
		t.Error("expected nil subgoals initially")
	}
	
	// Set goal and add subgoals
	gm.SetGoal("main goal")
	gm.AddSubGoal("sub 1")
	gm.AddSubGoal("sub 2")
	
	goals = gm.GetSubGoals()
	if len(goals) != 2 {
		t.Errorf("expected 2 subgoals, got %d", len(goals))
	}
}

func TestClearSubGoals(t *testing.T) {
	gm := NewGoalManager(nil, "")
	gm.SetGoal("main goal")
	gm.AddSubGoal("sub 1")
	gm.AddSubGoal("sub 2")
	
	gm.ClearSubGoals()
	
	goals := gm.GetSubGoals()
	if len(goals) != 0 {
		t.Errorf("expected 0 subgoals after clear, got %d", len(goals))
	}
}

func TestRemoveSubGoal(t *testing.T) {
	gm := NewGoalManager(nil, "")
	gm.SetGoal("main goal")
	
	sg, _ := gm.AddSubGoal("to be removed")
	gm.AddSubGoal("keep this")
	
	removed := gm.RemoveSubGoal(sg.ID)
	if !removed {
		t.Error("expected successful removal")
	}
	
	goals := gm.GetSubGoals()
	if len(goals) != 1 {
		t.Errorf("expected 1 subgoal after removal, got %d", len(goals))
	}
	
	// Try removing non-existent
	removed = gm.RemoveSubGoal("non-existent-id")
	if removed {
		t.Error("expected false for non-existent ID")
	}
}

func TestBuildSubGoalPrompt(t *testing.T) {
	gm := NewGoalManager(nil, "")
	gm.SetGoal("main goal")
	gm.AddSubGoal("criterion 1")
	gm.AddSubGoal("criterion 2")
	
	prompt := gm.BuildSubGoalPrompt()
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !contains(prompt, "criterion 1") {
		t.Error("prompt should include subgoal 1")
	}
	if !contains(prompt, "criterion 2") {
		t.Error("prompt should include subgoal 2")
	}
}

func TestBuildSubGoalPrompt_NoSubGoals(t *testing.T) {
	gm := NewGoalManager(nil, "")
	gm.SetGoal("main goal")
	
	prompt := gm.BuildSubGoalPrompt()
	if prompt != "" {
		t.Errorf("expected empty prompt, got: %s", prompt)
	}
}
