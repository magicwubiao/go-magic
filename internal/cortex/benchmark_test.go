package cortex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/internal/memory"
	"github.com/magicwubiao/go-magic/internal/perception"
)

// BenchmarkCortexPerception benchmarks the perception layer
func BenchmarkCortexPerception(b *testing.B) {
	parser := perception.NewParser()

	testCases := []string{
		"write a Python script to parse CSV files",
		"build a complete ETL pipeline and deploy to docker",
		"analyze the error logs and fix the bug",
		"create a REST API with authentication",
		"run the tests and verify results",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range testCases {
			parser.Parse(input, nil)
		}
	}
}

// BenchmarkMemorySnapshot benchmarks the snapshot manager
func BenchmarkMemorySnapshot(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manager := memory.NewSnapshotManager(tmpDir)
	manager.UpdateMemory("# Initial memory content")
	manager.RefreshSnapshot()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate mid-turn update
		manager.UpdateMemory("# Updated memory with new information")
		// Read frozen snapshot for prompt
		_ = manager.GetMemoryForPrompt()
	}
}

// BenchmarkCognitionPlanning benchmarks the cognition planning layer
func BenchmarkCognitionPlanning(b *testing.B) {
	// This would need to be implemented with the actual cognition package
	// For now, we'll benchmark the perception which feeds into cognition
	parser := perception.NewParser()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := parser.Parse("create a Python script to parse data", nil)
		// Simulate planning complexity determination
		_ = result.Intent.Complexity
	}
}

// BenchmarkFTSRetrieval benchmarks FTS retrieval (if FTS is available)
func BenchmarkFTSRetrieval(b *testing.B) {
	// Skip if FTS is not available
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Skip("temp dir not available")
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Skip("Cortex manager start failed: " + err.Error())
	}

	// Add some memory entries for search
	mgr.AppendMemory("- Python scripts are common")
	mgr.AppendMemory("- User prefers dark mode")
	mgr.AppendMemory("- Docker deployment workflow")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Search for relevant memories
		_ = mgr.FTSMemory
		// Note: Actual FTS search would be called here
		// mgr.FTSMemory.Search("python scripts")
	}
}

// BenchmarkFullPipeline benchmarks the complete Cortex pipeline
func BenchmarkFullPipeline(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Fatal(err)
	}

	testInputs := []string{
		"write a Python script to parse CSV files",
		"build a complete ETL pipeline",
		"analyze the error logs",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range testInputs {
			// User message trigger
			mgr.OnUserMessage(input)

			// Turn start (freeze snapshot)
			mgr.OnTurnStart()

			// Get perception result
			_ = mgr.LastPerception

			// Get decision
			_ = mgr.LastDecision

			// Turn end
			mgr.OnTurnEnd()
		}
	}
}

// BenchmarkSnapshotRefresh benchmarks snapshot refresh cycle
func BenchmarkSnapshotRefresh(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Fatal(err)
	}

	// Add memory
	mgr.AppendMemory("- User works with Python")
	mgr.AppendMemory("- Prefers automated solutions")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.RefreshSnapshot()
		_ = mgr.Snapshot.GetMemoryForPrompt()
	}
}

// BenchmarkAppendMemory benchmarks memory append operations
func BenchmarkAppendMemory(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Fatal(err)
	}

	entries := []string{
		"- User prefers dark mode",
		"- Python is the main language",
		"- Docker is used for deployment",
		"- GitHub is the code repository",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, entry := range entries {
			mgr.AppendMemory(entry)
		}
	}
}

// BenchmarkManagerCreation benchmarks Cortex Manager creation
func BenchmarkManagerCreation(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create new manager for each iteration
		os.RemoveAll(tmpDir)
		os.MkdirAll(tmpDir, 0755)

		mgr := NewManager(tmpDir)
		mgr.Start()
	}
}

// BenchmarkPerceptionWithEntities benchmarks perception with entity extraction
func BenchmarkPerceptionWithEntities(b *testing.B) {
	parser := perception.NewParser()

	inputs := []string{
		"write a Python script using docker and git",
		"create a TypeScript file with .ts extension",
		"deploy to kubernetes using kubectl",
		"analyze the app.js and data.csv files",
		"run npm install and pip install python packages",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			result := parser.Parse(input, nil)
			_ = result.Intent.Entities
			_ = result.Intent.Keywords
		}
	}
}

// BenchmarkPerceptionWithHistory benchmarks perception with context history
func BenchmarkPerceptionWithHistory(b *testing.B) {
	parser := perception.NewParser()

	history := []string{
		"build the Docker image",
		"run the tests",
		"deploy to staging",
		"configure the environment",
	}

	input := "continue with the deployment"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := parser.Parse(input, history)
		_ = result.ContextHints
	}
}

// BenchmarkTriggerNudge benchmarks the trigger nudge mechanism
func BenchmarkTriggerNudge(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.OnUserMessage("write a script")
		_ = mgr.Trigger.ShouldNudge()
	}
}

// BenchmarkCheckpointCreation benchmarks execution checkpoint creation
func BenchmarkCheckpointCreation(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan := mgr.Cognition.CreatePlan("test task", &perception.PerceptionResult{
			Intent: perception.IntentClassification{
				Type: perception.IntentTask,
			},
		})

		if plan != nil && plan.Plan != nil {
			// Create checkpoint
			_ = mgr.Execution.StartCheckpoint("bench-task-"+string(rune('0'+i%10)), plan.Plan)
		}
	}
}

// BenchmarkMultiTurnScenario benchmarks a realistic multi-turn scenario
func BenchmarkMultiTurnScenario(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	scenario := []struct {
		input    string
		turns    int
	}{
		{"write a Python script to parse CSV", 3},
		{"add error handling", 2},
		{"create tests for the script", 3},
		{"run the tests", 1},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr := NewManager(tmpDir)
		mgr.Start()

		for _, step := range scenario {
			mgr.OnUserMessage(step.input)
			mgr.OnTurnStart()

			for t := 0; t < step.turns; t++ {
				_ = mgr.LastPerception
				_ = mgr.LastDecision
			}

			mgr.OnTurnEnd()
		}
	}
}

// BenchmarkGetMemoryForPrompt benchmarks memory retrieval for prompts
func BenchmarkGetMemoryForPrompt(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Fatal(err)
	}

	// Add some memory entries
	for i := 0; i < 20; i++ {
		mgr.AppendMemory("- Memory entry " + string(rune('0'+i%10)))
	}
	mgr.RefreshSnapshot()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.GetMemoryForPrompt()
	}
}

// BenchmarkReviewCreation benchmarks background review creation
func BenchmarkReviewCreation(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.CreateReview("test", "summary", "Test review content", "positive")
	}
}

// BenchmarkSkillSuggestion benchmarks skill suggestion
func BenchmarkSkillSuggestion(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cortex-bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	if err := mgr.Start(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.SuggestSkills("python script", 3)
	}
}
