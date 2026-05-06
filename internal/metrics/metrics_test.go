package metrics

import (
	"testing"
	"time"
)

func TestCounter_Operations(t *testing.T) {
	c := &Counter{}
	
	// Test Inc
	c.Inc()
	if c.Value() != 1 {
		t.Errorf("Expected 1, got %d", c.Value())
	}
	
	// Test Add
	c.Add(5)
	if c.Value() != 6 {
		t.Errorf("Expected 6, got %d", c.Value())
	}
	
	// Test Reset
	c.Reset()
	if c.Value() != 0 {
		t.Errorf("Expected 0 after reset, got %d", c.Value())
	}
}

func TestGauge_Operations(t *testing.T) {
	g := &Gauge{}
	
	// Test Set
	g.Set(100)
	if g.Value() != 100 {
		t.Errorf("Expected 100, got %d", g.Value())
	}
	
	// Test Inc
	g.Inc()
	if g.Value() != 101 {
		t.Errorf("Expected 101, got %d", g.Value())
	}
	
	// Test Dec
	g.Dec()
	if g.Value() != 100 {
		t.Errorf("Expected 100, got %d", g.Value())
	}
	
	// Test Add
	g.Add(50)
	if g.Value() != 150 {
		t.Errorf("Expected 150, got %d", g.Value())
	}
}

func TestHistogram_Operations(t *testing.T) {
	h := NewHistogram([]float64{1, 5, 10, 25, 50, 100})
	
	// Test Observe
	h.Observe(5)
	h.Observe(10)
	h.Observe(25)
	h.Observe(50)
	h.Observe(100)
	h.Observe(200)
	
	if h.Count() != 6 {
		t.Errorf("Expected count 6, got %d", h.Count())
	}
	
	sum := h.Sum()
	if sum != 390 {
		t.Errorf("Expected sum 390, got %f", sum)
	}
	
	avg := h.Avg()
	expectedAvg := 390.0 / 6.0
	if avg != expectedAvg {
		t.Errorf("Expected avg %f, got %f", expectedAvg, avg)
	}
}

func TestHistogram_Percentiles(t *testing.T) {
	h := NewHistogram([]float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100})
	
	// Add values 1-100
	for i := 1; i <= 100; i++ {
		h.Observe(float64(i))
	}
	
	// Test percentiles
	p50 := h.Percentile(50)
	if p50 < 45 || p50 > 55 {
		t.Errorf("Expected P50 around 50, got %f", p50)
	}
	
	p95 := h.Percentile(95)
	if p95 < 90 || p95 > 100 {
		t.Errorf("Expected P95 around 95, got %f", p95)
	}
}

func TestHistogram_MinMax(t *testing.T) {
	h := NewHistogram([]float64{10, 50, 100})
	
	h.Observe(25)
	h.Observe(75)
	
	if h.Min() != 25 {
		t.Errorf("Expected min 25, got %f", h.Min())
	}
	
	if h.Max() != 75 {
		t.Errorf("Expected max 75, got %f", h.Max())
	}
}

func TestNewMetrics(t *testing.T) {
	m := NewMetrics()
	
	if m.PerceptionLatency == nil {
		t.Error("Expected non-nil PerceptionLatency histogram")
	}
	
	if m.IntentClassification == nil {
		t.Error("Expected non-nil IntentClassification counter")
	}
	
	if m.ExecutionSuccess == nil {
		t.Error("Expected non-nil ExecutionSuccess counter")
	}
}

func TestRecordPerception(t *testing.T) {
	m := NewMetrics()
	
	m.RecordPerception(10*time.Millisecond, "file_operation")
	m.RecordPerception(15*time.Millisecond, "file_operation")
	m.RecordPerception(20*time.Millisecond, "code_analysis")
	
	// Check counter
	if m.IntentClassification.Value() != 3 {
		t.Errorf("Expected 3 classifications, got %d", m.IntentClassification.Value())
	}
	
	// Check histogram
	if m.PerceptionLatency.Count() != 3 {
		t.Errorf("Expected 3 observations, got %d", m.PerceptionLatency.Count())
	}
	
	// Check intent distribution
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	if m.IntentDistribution["file_operation"].Value() != 2 {
		t.Errorf("Expected 2 file_operation, got %d", m.IntentDistribution["file_operation"].Value())
	}
}

func TestRecordPlanning(t *testing.T) {
	m := NewMetrics()
	
	m.RecordPlanning(5*time.Millisecond, 3)
	m.RecordPlanning(10*time.Millisecond, 5)
	
	if m.PlanningLatency.Count() != 2 {
		t.Errorf("Expected 2 observations, got %d", m.PlanningLatency.Count())
	}
	
	if m.AvgStepsPerPlan.Value() != 5 {
		t.Errorf("Expected avg steps 5, got %d", m.AvgStepsPerPlan.Value())
	}
}

func TestRecordExecution(t *testing.T) {
	m := NewMetrics()
	
	m.RecordExecution(100*time.Millisecond, true)
	m.RecordExecution(200*time.Millisecond, true)
	m.RecordExecution(50*time.Millisecond, false)
	
	if m.ExecutionSuccess.Value() != 2 {
		t.Errorf("Expected 2 successes, got %d", m.ExecutionSuccess.Value())
	}
	
	if m.ExecutionFailure.Value() != 1 {
		t.Errorf("Expected 1 failure, got %d", m.ExecutionFailure.Value())
	}
}

func TestRecordCheckpoint(t *testing.T) {
	m := NewMetrics()
	
	m.RecordCheckpoint(true)  // Created
	m.RecordCheckpoint(false) // Restored
	m.RecordCheckpoint(true)  // Created
	
	if m.CheckpointsCreated.Value() != 2 {
		t.Errorf("Expected 2 created, got %d", m.CheckpointsCreated.Value())
	}
	
	if m.CheckpointsRestored.Value() != 1 {
		t.Errorf("Expected 1 restored, got %d", m.CheckpointsRestored.Value())
	}
}

func TestRecordMemory(t *testing.T) {
	m := NewMetrics()
	
	m.RecordMemory(1024, 5*time.Millisecond, true)
	m.RecordMemory(2048, 10*time.Millisecond, false)
	
	if m.MemorySize.Value() != 2048 {
		t.Errorf("Expected memory size 2048, got %d", m.MemorySize.Value())
	}
	
	if m.MemoryRetrievalCount.Value() != 2 {
		t.Errorf("Expected 2 retrievals, got %d", m.MemoryRetrievalCount.Value())
	}
	
	if m.CacheHits.Value() != 1 {
		t.Errorf("Expected 1 cache hit, got %d", m.CacheHits.Value())
	}
	
	if m.CacheMisses.Value() != 1 {
		t.Errorf("Expected 1 cache miss, got %d", m.CacheMisses.Value())
	}
}

func TestRecordPlugin(t *testing.T) {
	m := NewMetrics()
	
	m.RecordPlugin("load", true)
	m.RecordPlugin("use", true)
	m.RecordPlugin("use", true)
	m.RecordPlugin("error", true)
	
	if m.PluginLoadCount.Value() != 1 {
		t.Errorf("Expected 1 load, got %d", m.PluginLoadCount.Value())
	}
	
	if m.PluginUsageCount.Value() != 2 {
		t.Errorf("Expected 2 usages, got %d", m.PluginUsageCount.Value())
	}
	
	if m.PluginErrors.Value() != 1 {
		t.Errorf("Expected 1 error, got %d", m.PluginErrors.Value())
	}
}

func TestRecordSkill(t *testing.T) {
	m := NewMetrics()
	
	m.RecordSkill("create")
	m.RecordSkill("use")
	m.RecordSkill("use")
	m.RecordSkill("pattern")
	
	if m.SkillsCreated.Value() != 1 {
		t.Errorf("Expected 1 skill created, got %d", m.SkillsCreated.Value())
	}
	
	if m.SkillUsageCount.Value() != 2 {
		t.Errorf("Expected 2 skill usages, got %d", m.SkillUsageCount.Value())
	}
	
	if m.PatternDetections.Value() != 1 {
		t.Errorf("Expected 1 pattern detection, got %d", m.PatternDetections.Value())
	}
}

func TestRecordNudge(t *testing.T) {
	m := NewMetrics()
	
	m.RecordNudge(true)
	m.RecordNudge(true)
	m.RecordNudge(false)
	
	if m.NudgeCount.Value() != 3 {
		t.Errorf("Expected 3 nudges, got %d", m.NudgeCount.Value())
	}
	
	if m.NudgeAcceptance.Value() != 2 {
		t.Errorf("Expected 2 accepted, got %d", m.NudgeAcceptance.Value())
	}
}

func TestSnapshot(t *testing.T) {
	m := NewMetrics()
	
	// Record some data
	m.RecordPerception(10*time.Millisecond, "test")
	m.RecordPlanning(5*time.Millisecond, 3)
	m.RecordExecution(100*time.Millisecond, true)
	m.RecordMemory(1024, 5*time.Millisecond, true)
	
	snapshot := m.Snapshot()
	
	if snapshot == nil {
		t.Fatal("Expected non-nil snapshot")
	}
	
	// Check perception metrics
	if snapshot.Perception.TotalClassified != 1 {
		t.Errorf("Expected 1 classification, got %d", snapshot.Perception.TotalClassified)
	}
	
	// Check execution metrics
	if snapshot.Execution.SuccessRate != 1.0 {
		t.Errorf("Expected 100%% success rate, got %f", snapshot.Execution.SuccessRate)
	}
	
	// Check memory metrics
	if snapshot.Memory.CacheHitRate != 1.0 {
		t.Errorf("Expected 100%% cache hit rate, got %f", snapshot.Memory.CacheHitRate)
	}
}

func TestSnapshot_Empty(t *testing.T) {
	m := NewMetrics()
	snapshot := m.Snapshot()
	
	if snapshot.Perception.AvgLatencyMs != 0 {
		t.Errorf("Expected 0 avg latency, got %f", snapshot.Perception.AvgLatencyMs)
	}
	
	if snapshot.Execution.SuccessRate != 0 {
		t.Errorf("Expected 0 success rate, got %f", snapshot.Execution.SuccessRate)
	}
}

func TestExportJSON(t *testing.T) {
	m := NewMetrics()
	
	m.RecordPerception(10*time.Millisecond, "test")
	
	data, err := m.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}
	
	if len(data) == 0 {
		t.Error("Expected non-empty JSON")
	}
	
	// Check it contains expected keys
	jsonStr := string(data)
	if len(jsonStr) < 50 {
		t.Error("JSON seems too short")
	}
}

func TestExportPrometheus(t *testing.T) {
	m := NewMetrics()
	
	m.RecordPerception(10*time.Millisecond, "test")
	m.RecordExecution(100*time.Millisecond, true)
	
	output := m.ExportPrometheus()
	
	// Check for metric names
	expectedMetrics := []string{
		"cortex_perception_latency_avg_ms",
		"cortex_execution_success_total",
	}
	
	for _, metric := range expectedMetrics {
		if !contains(output, metric) {
			t.Errorf("Expected Prometheus output to contain %s", metric)
		}
	}
}

func TestReset(t *testing.T) {
	m := NewMetrics()
	
	// Record some data
	m.RecordPerception(10*time.Millisecond, "test")
	m.RecordExecution(100*time.Millisecond, true)
	
	// Reset
	m.Reset()
	
	// Check metrics are reset
	if m.IntentClassification.Value() != 0 {
		t.Errorf("Expected 0 classifications after reset, got %d", m.IntentClassification.Value())
	}
	
	if m.ExecutionSuccess.Value() != 0 {
		t.Errorf("Expected 0 successes after reset, got %d", m.ExecutionSuccess.Value())
	}
}

func TestUpdateSystemMetrics(t *testing.T) {
	m := NewMetrics()
	
	m.UpdateSystemMetrics()
	
	if m.Uptime.Value() < 0 {
		t.Error("Expected non-negative uptime")
	}
}

func TestMetricsSnapshot_AllFields(t *testing.T) {
	m := NewMetrics()
	
	// Fill with data
	m.RecordPerception(10*time.Millisecond, "test")
	m.RecordPlanning(5*time.Millisecond, 3)
	m.RecordExecution(100*time.Millisecond, true)
	m.RecordExecution(200*time.Millisecond, false)
	m.RecordMemory(1024, 5*time.Millisecond, true)
	m.RecordPlugin("load", true)
	m.RecordSkill("create", true)
	m.RecordNudge(true)
	
	snapshot := m.Snapshot()
	
	// Verify all fields are populated
	if snapshot.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}
	
	if snapshot.Perception.TotalClassified == 0 {
		t.Error("Expected non-zero total classified")
	}
	
	if snapshot.Planning.TotalAdjustments == 0 {
		t.Error("Expected planning metrics")
	}
	
	if snapshot.Execution.TotalSuccess == 0 {
		t.Error("Expected execution success count")
	}
	
	if snapshot.Memory.SizeBytes == 0 {
		t.Error("Expected memory size")
	}
	
	if snapshot.Plugins.TotalLoads == 0 {
		t.Error("Expected plugin loads")
	}
	
	if snapshot.Skills.TotalCreated == 0 {
		t.Error("Expected skill created count")
	}
	
	if snapshot.Triggers.TotalNudges == 0 {
		t.Error("Expected nudge count")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func BenchmarkRecordPerception(b *testing.B) {
	m := NewMetrics()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordPerception(time.Millisecond, "test")
	}
}

func BenchmarkSnapshot(b *testing.B) {
	m := NewMetrics()
	m.RecordPerception(10*time.Millisecond, "test")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Snapshot()
	}
}

func BenchmarkExportJSON(b *testing.B) {
	m := NewMetrics()
	m.RecordPerception(10*time.Millisecond, "test")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.ExportJSON()
	}
}
