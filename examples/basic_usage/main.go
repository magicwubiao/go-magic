// Package main provides basic usage examples for the Cortex Agent
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/metrics"
	"github.com/magicwubiao/go-magic/internal/perception"
	"github.com/magicwubiao/go-magic/internal/cognition"
	"github.com/magicwubiao/go-magic/internal/execution"
)

func main() {
	// Example 1: Basic Agent Initialization
	basicInitialization()

	// Example 2: Processing a User Request
	processUserRequest()

	// Example 3: Using Metrics
	usingMetrics()

	// Example 4: Cortex Full Pipeline
	cortexPipeline()
}

func basicInitialization() {
	fmt.Println("=== Example 1: Basic Agent Initialization ===")

	// Create a new agent
	a := agent.NewAgent(&agent.Config{
		Name:        "my-agent",
		Description: "A basic agent example",
	})

	// Initialize the agent
	if err := a.Initialize(); err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	fmt.Println("Agent initialized successfully")
}

func processUserRequest() {
	fmt.Println("\n=== Example 2: Processing a User Request ===")

	// Create perception layer
	parser := perception.NewParser()

	// Parse user input
	input := "Create a new file called hello.txt with the content Hello, World!"
	result := parser.Parse(input, nil)

	fmt.Printf("Parsed intent: %s\n", result.Intent)
	fmt.Printf("Confidence: %.2f\n", result.Confidence)
	fmt.Printf("Complexity: %s\n", result.Complexity)
	fmt.Printf("Entities: %v\n", result.Entities)

	// Create planner
	planner := cognition.NewPlanner()

	// Create execution plan
	plan := planner.CreatePlan(input, result)

	fmt.Printf("Plan steps: %d\n", len(plan.Steps))
	for i, step := range plan.Steps {
		fmt.Printf("  Step %d: %s (%s)\n", i+1, step.Name, step.Tool)
	}
}

func usingMetrics() {
	fmt.Println("\n=== Example 3: Using Metrics ===")

	// Create metrics collector
	m := metrics.NewMetrics()

	// Record some fake metrics
	ctx := context.Background()
	
	// Simulate perception
	start := time.Now()
	m.RecordPerception(time.Since(start), "file_operation")
	
	// Simulate planning
	start = time.Now()
	m.RecordPlanning(time.Since(start), 3)
	
	// Simulate execution
	start = time.Now()
	m.RecordExecution(time.Since(start), true)

	// Get snapshot
	snapshot := m.Snapshot()

	fmt.Printf("Perception avg latency: %.2fms\n", snapshot.Perception.AvgLatencyMs)
	fmt.Printf("Planning avg steps: %d\n", snapshot.Planning.AvgSteps)
	fmt.Printf("Execution success rate: %.2f%%\n", snapshot.Execution.SuccessRate*100)

	// Export to JSON
	jsonData, _ := m.ExportJSON()
	fmt.Printf("Metrics JSON: %s\n", string(jsonData[:min(200, len(jsonData))]))
}

func cortexPipeline() {
	fmt.Println("\n=== Example 4: Cortex Full Pipeline ===")

	// Create Cortex agent
	h, err := cortex.NewCortex(&cortex.Config{
		EnablePerception: true,
		EnableCognition: true,
		EnableExecution: true,
		EnableMemory:    true,
	})
	if err != nil {
		log.Fatalf("Failed to create Cortex: %v", err)
	}

	// Create context
	ctx := context.Background()

	// Process a complex request
	input := "Analyze the codebase for any TODO comments and create a summary report"

	result, err := h.Process(ctx, input)
	if err != nil {
		log.Printf("Processing error: %v", err)
	}

	fmt.Printf("Result: %s\n", result.Summary)
	fmt.Printf("Steps completed: %d\n", result.StepsCompleted)
	fmt.Printf("Success: %v\n", result.Success)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
