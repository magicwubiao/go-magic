// Package main demonstrates the full Cortex Agent pipeline
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/magicwubiao/go-magic/internal/cognition"
	"github.com/magicwubiao/go-magic/internal/execution"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/internal/perception"
	"github.com/magicwubiao/go-magic/internal/review"
	"github.com/magicwubiao/go-magic/internal/trigger"
)

func main() {
	fmt.Println("=== Cortex Agent Full Pipeline Demo ===\n")
	
	// Initialize all six systems
	demoPipeline()
}

func demoPipeline() {
	ctx := context.Background()
	
	// ============ System 1: Perception ============
	fmt.Println("--- System 1: Perception Layer ---")
	
	parser := perception.NewParser()
	input := "Create a backup of the config directory and compress it"
	
	perceptionResult := parser.Parse(input, nil)
	fmt.Printf("Intent: %s (confidence: %.2f)\n", perceptionResult.Intent, perceptionResult.Confidence)
	fmt.Printf("Complexity: %s\n", perceptionResult.Complexity)
	fmt.Printf("Keywords: %v\n", perceptionResult.Keywords)
	
	// ============ System 2: Trigger ============
	fmt.Println("\n--- System 2: Trigger System ---")
	
	messageTrigger := trigger.NewEnhancedMessageTrigger()
	
	// Simulate conversation turns
	for i := 1; i <= 20; i++ {
		messageTrigger.OnUserMessage(fmt.Sprintf("Turn %d message", i))
	}
	
	fmt.Printf("Turn count: %d\n", messageTrigger.GetTurnCount())
	fmt.Printf("Dynamic threshold: %d\n", messageTrigger.GetDynamicThreshold())
	
	// Register a nudge handler
	messageTrigger.RegisterNudgeHandlerFunc("test", func(n *trigger.Nudge) bool {
		fmt.Printf("Nudge received: %s\n", n.Message)
		return true
	})
	
	// ============ System 3: Background Review ============
	fmt.Println("\n--- System 3: Background Review ---")
	
	reviewer := review.NewEnhancedBackgroundReview("./tmp/reviews")
	if err := reviewer.Start(); err != nil {
		log.Printf("Failed to start reviewer: %v", err)
	}
	
	// Trigger a review
	toolCalls := []string{"glob", "read_file", "write_file", "execute_command"}
	reviewer.TriggerNudgeReview(20, toolCalls, &review.PerformanceMetrics{
		PerceptionLatency: 15 * time.Millisecond,
		PlanningLatency:   25 * time.Millisecond,
		ExecutionLatency:  500 * time.Millisecond,
		SuccessRate:       0.95,
	})
	
	// Wait a bit for async review
	time.Sleep(100 * time.Millisecond)
	
	stats := reviewer.GetStats()
	fmt.Printf("Total reviews: %d\n", stats.TotalReviews)
	fmt.Printf("Total patterns: %d\n", stats.TotalPatterns)
	
	// ============ System 4: Cognition ============
	fmt.Println("\n--- System 4: Cognition Layer ---")
	
	planner := cognition.NewPlanner()
	
	plan := planner.CreatePlan(input, perceptionResult)
	fmt.Printf("Plan created with %d steps\n", len(plan.Steps))
	
	for i, step := range plan.Steps {
		fmt.Printf("  Step %d: %s using %s\n", i+1, step.Name, step.Tool)
	}
	
	// ============ System 5: Memory ============
	fmt.Println("\n--- System 5: Holographic Memory ---")
	
	fts, err := memory.NewFTSStore("./tmp/memory")
	if err != nil {
		log.Printf("Failed to create FTS store: %v", err)
	} else {
		// Add some memories
		memories := []memory.MemoryRecord{
			{SessionID: "s1", TurnNumber: 1, Role: "user", Content: "How do I create a backup?", ContentType: "text", Importance: 7},
			{SessionID: "s1", TurnNumber: 2, Role: "assistant", Content: "You can use tar to create compressed backups", ContentType: "text", Importance: 8},
			{SessionID: "s1", TurnNumber: 3, Role: "user", Content: "Show me an example", ContentType: "text", Importance: 6},
		}
		
		for i := range memories {
			if err := fts.Add(&memories[i]); err != nil {
				log.Printf("Failed to add memory: %v", err)
			}
		}
		
		// Search memories
		results, err := fts.Search("backup", 5)
		if err != nil {
			log.Printf("Search failed: %v", err)
		} else {
			fmt.Printf("Found %d matching memories\n", len(results))
			for _, r := range results {
				fmt.Printf("  - [%s] %s\n", r.SessionID, r.Snippet)
			}
		}
	}
	
	// ============ System 6: Skill Evolution ============
	fmt.Println("\n--- System 6: Skill Self-Evolution ---")
	
	// Note: Enhanced skill creator would be used here
	fmt.Println("Skill evolution system active - patterns being tracked")
	
	// ============ Execution Layer ============
	fmt.Println("\n--- Execution Layer ---")
	
	execManager := execution.NewManager("./tmp/checkpoints")
	
	// Create a checkpoint
	checkpoint := execManager.StartCheckpoint("task-123", plan)
	fmt.Printf("Checkpoint created: %s\n", checkpoint.ID)
	
	// Simulate execution steps
	for i, step := range plan.Steps {
		execManager.UpdateCheckpoint(checkpoint, i+1, step.Name)
		time.Sleep(50 * time.Millisecond)
	}
	
	// Complete the checkpoint
	execManager.CompleteCheckpoint(checkpoint)
	
	progress := execManager.GetProgress(checkpoint)
	fmt.Printf("Execution progress: %.0f%%\n", progress.Percentage)
	
	// ============ Cortex Orchestration ============
	fmt.Println("\n--- Cortex Full Orchestration ---")
	
	h, err := cortex.NewCortex(&cortex.Config{
		EnablePerception: true,
		EnableCognition:  true,
		EnableExecution:  true,
		EnableMemory:     true,
		EnableReview:     true,
		EnableTriggers:   true,
	})
	if err != nil {
		log.Fatalf("Failed to create Cortex: %v", err)
	}
	
	result, err := h.Process(ctx, "Show me the current project structure")
	if err != nil {
		log.Printf("Processing error: %v", err)
	}
	
	if result != nil {
		fmt.Printf("Processing successful: %v\n", result.Success)
		fmt.Printf("Steps completed: %d\n", result.StepsCompleted)
	}
	
	fmt.Println("\n=== Pipeline Demo Complete ===")
}
