package metrics

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Counter represents a cumulative counter metric
type Counter struct {
	value uint64
}

// Inc increments the counter by 1
func (c *Counter) Inc() {
	atomic.AddUint64(&c.value, 1)
}

// Add adds the given value to the counter
func (c *Counter) Add(v uint64) {
	atomic.AddUint64(&c.value, v)
}

// Value returns the current counter value
func (c *Counter) Value() uint64 {
	return atomic.LoadUint64(&c.value)
}

// Reset resets the counter to zero
func (c *Counter) Reset() {
	atomic.StoreUint64(&c.value, 0)
}

// Gauge represents a point-in-time gauge metric
type Gauge struct {
	value int64
}

// Set sets the gauge to the given value
func (g *Gauge) Set(v int64) {
	atomic.StoreInt64(&g.value, v)
}

// Inc increments the gauge by 1
func (g *Gauge) Inc() {
	atomic.AddInt64(&g.value, 1)
}

// Dec decrements the gauge by 1
func (g *Gauge) Dec() {
	atomic.AddInt64(&g.value, -1)
}

// Add adds the given value to the gauge
func (g *Gauge) Add(v int64) {
	atomic.AddInt64(&g.value, v)
}

// Value returns the current gauge value
func (g *Gauge) Value() int64 {
	return atomic.LoadInt64(&g.value)
}

// Histogram represents a histogram metric for distribution tracking
type Histogram struct {
	mu      sync.Mutex
	buckets map[float64]int64 // Upper bounds to counts
	count   int64
	sum     float64
	min     float64
	max     float64
}

// NewHistogram creates a new histogram with custom buckets
func NewHistogram(buckets []float64) *Histogram {
	h := &Histogram{
		buckets: make(map[float64]int64),
		min:     float64(^uint64(0 >> 1)), // Max float64
	}
	for _, b := range buckets {
		h.buckets[b] = 0
	}
	return h
}

// Observe records a single observation
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	// Update count (no need for atomic since we hold the mutex)
	h.count++
	
	// Update sum
	h.sum += v
	
	// Update min
	if v < h.min {
		h.min = v
	}
	
	// Update max
	if v > h.max {
		h.max = v
	}
	
	// Update bucket counts
	for bound := range h.buckets {
		if v <= bound {
			h.buckets[bound]++
		}
	}
}

// Percentile calculates the p-th percentile
func (h *Histogram) Percentile(p float64) float64 {
	if h.Count() == 0 {
		return 0
	}
	
	h.mu.Lock()
	defer h.mu.Unlock()
	
	// Calculate count at percentile
	target := float64(h.count) * p / 100.0
	var cumulative int64
	
	bounds := make([]float64, 0, len(h.buckets))
	for b := range h.buckets {
		bounds = append(bounds, b)
	}
	
	for i := 0; i < len(bounds)-1; i++ {
		cumulative += h.buckets[bounds[i]]
		if float64(cumulative) >= target {
			return bounds[i]
		}
	}
	
	return bounds[len(bounds)-1]
}

// Count returns the total observation count
func (h *Histogram) Count() int64 {
	return atomic.LoadInt64(&h.count)
}

// Sum returns the sum of all observations
func (h *Histogram) Sum() float64 {
	h.mu.Lock(); defer h.mu.Unlock(); return h.sum
}

// Avg returns the average of all observations
func (h *Histogram) Avg() float64 {
	count := h.Count()
	if count == 0 {
		return 0
	}
	return h.Sum() / float64(count)
}

// Min returns the minimum observation
func (h *Histogram) Min() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	min := h.min
	if min == float64(^uint64(0>>1)) {
		return 0
	}
	return min
}

// Max returns the maximum observation
func (h *Histogram) Max() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.max
}

// Metrics represents a collection of all metrics
type Metrics struct {
	// Perception layer metrics
	PerceptionLatency   *Histogram `json:"perception_latency"`
	IntentClassification *Counter  `json:"intent_classification"`
	
	// Decision layer metrics
	PlanningLatency     *Histogram `json:"planning_latency"`
	AvgStepsPerPlan     *Gauge     `json:"avg_steps_per_plan"`
	PlanAdjustments     *Counter   `json:"plan_adjustments"`
	
	// Execution layer metrics
	ExecutionLatency   *Histogram `json:"execution_latency"`
	ExecutionSuccess   *Counter   `json:"execution_success"`
	ExecutionFailure   *Counter   `json:"execution_failure"`
	CheckpointsCreated *Counter    `json:"checkpoints_created"`
	CheckpointsRestored *Counter   `json:"checkpoints_restored"`
	
	// Memory metrics
	MemorySize         *Gauge     `json:"memory_size_bytes"`
	FTSQueryLatency    *Histogram `json:"fts_query_latency"`
	MemoryRetrievalCount *Counter  `json:"memory_retrieval_count"`
	CacheHits          *Counter   `json:"cache_hits"`
	CacheMisses        *Counter   `json:"cache_misses"`
	
	// Plugin metrics
	PluginLoadCount    *Counter   `json:"plugin_load_count"`
	PluginUsageCount   *Counter   `json:"plugin_usage_count"`
	PluginErrors       *Counter   `json:"plugin_errors"`
	
	// Skill metrics
	SkillsCreated      *Counter   `json:"skills_created"`
	SkillUsageCount    *Counter   `json:"skill_usage_count"`
	PatternDetections  *Counter   `json:"pattern_detections"`
	
	// Trigger metrics
	NudgeCount         *Counter   `json:"nudge_count"`
	NudgeAcceptance    *Counter   `json:"nudge_acceptance"`
	
	// Review metrics
	ReviewCount        *Counter   `json:"review_count"`
	
	// System metrics
	ActiveGoroutines   *Gauge     `json:"active_goroutines"`
	Uptime             *Gauge     `json:"uptime_seconds"`
	
	// Intent distribution
	IntentDistribution map[string]*Counter
	
	startTime time.Time
	mu        sync.RWMutex
}

// NewMetrics creates a new metrics collector
func NewMetrics() *Metrics {
	m := &Metrics{
		PerceptionLatency:    NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0}),
		IntentClassification: &Counter{},
		PlanningLatency:      NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0}),
		AvgStepsPerPlan:      &Gauge{},
		PlanAdjustments:      &Counter{},
		ExecutionLatency:     NewHistogram([]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}),
		ExecutionSuccess:     &Counter{},
		ExecutionFailure:     &Counter{},
		CheckpointsCreated:   &Counter{},
		CheckpointsRestored:  &Counter{},
		MemorySize:           &Gauge{},
		FTSQueryLatency:      NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5}),
		MemoryRetrievalCount:  &Counter{},
		CacheHits:             &Counter{},
		CacheMisses:          &Counter{},
		PluginLoadCount:      &Counter{},
		PluginUsageCount:     &Counter{},
		PluginErrors:         &Counter{},
		SkillsCreated:        &Counter{},
		SkillUsageCount:      &Counter{},
		PatternDetections:    &Counter{},
		NudgeCount:           &Counter{},
		NudgeAcceptance:      &Counter{},
		ReviewCount:          &Counter{},
		ActiveGoroutines:     &Gauge{},
		Uptime:               &Gauge{},
		IntentDistribution:   make(map[string]*Counter),
		startTime:            time.Now(),
	}
	
	return m
}

// RecordPerception records perception layer metrics
func (m *Metrics) RecordPerception(duration time.Duration, intent string) {
	m.PerceptionLatency.Observe(duration.Seconds())
	m.IntentClassification.Inc()
	
	// Track intent distribution
	m.mu.Lock()
	if _, ok := m.IntentDistribution[intent]; !ok {
		m.IntentDistribution[intent] = &Counter{}
	}
	m.IntentDistribution[intent].Inc()
	m.mu.Unlock()
}

// RecordPlanning records planning layer metrics
func (m *Metrics) RecordPlanning(duration time.Duration, steps int) {
	m.PlanningLatency.Observe(duration.Seconds())
	m.AvgStepsPerPlan.Set(int64(steps))
}

// RecordExecution records execution layer metrics
func (m *Metrics) RecordExecution(duration time.Duration, success bool) {
	m.ExecutionLatency.Observe(duration.Seconds())
	if success {
		m.ExecutionSuccess.Inc()
	} else {
		m.ExecutionFailure.Inc()
	}
}

// RecordCheckpoint records checkpoint operations
func (m *Metrics) RecordCheckpoint(created bool) {
	if created {
		m.CheckpointsCreated.Inc()
	} else {
		m.CheckpointsRestored.Inc()
	}
}

// RecordMemory records memory operation metrics
func (m *Metrics) RecordMemory(size int64, queryDuration time.Duration, cacheHit bool) {
	m.MemorySize.Set(size)
	m.FTSQueryLatency.Observe(queryDuration.Seconds())
	m.MemoryRetrievalCount.Inc()
	if cacheHit {
		m.CacheHits.Inc()
	} else {
		m.CacheMisses.Inc()
	}
}

// RecordPlugin records plugin metrics
func (m *Metrics) RecordPlugin(operation string, success bool) {
	switch operation {
	case "load":
		m.PluginLoadCount.Inc()
	case "use":
		m.PluginUsageCount.Inc()
	case "error":
		m.PluginErrors.Inc()
	}
}

// RecordSkill records skill metrics
func (m *Metrics) RecordSkill(operation string) {
	switch operation {
	case "create":
		m.SkillsCreated.Inc()
	case "use":
		m.SkillUsageCount.Inc()
	case "pattern":
		m.PatternDetections.Inc()
	}
}

// RecordNudge records nudge metrics
func (m *Metrics) RecordNudge(accepted bool) {
	m.NudgeCount.Inc()
	if accepted {
		m.NudgeAcceptance.Inc()
	}
}

// RecordReview records review metrics
func (m *Metrics) RecordReview() {
	m.ReviewCount.Inc()
}

// RecordPlanAdjustment records a plan adjustment
func (m *Metrics) RecordPlanAdjustment() {
	m.PlanAdjustments.Inc()
}

// UpdateSystemMetrics updates system-level metrics
func (m *Metrics) UpdateSystemMetrics() {
	m.Uptime.Set(int64(time.Since(m.startTime).Seconds()))
}

// MetricsSnapshot represents a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	Timestamp        time.Time `json:"timestamp"`
	Perception      PerceptionMetrics `json:"perception"`
	Planning        PlanningMetrics   `json:"planning"`
	Execution       ExecutionMetrics  `json:"execution"`
	Memory          MemoryMetrics     `json:"memory"`
	Plugins         PluginMetrics     `json:"plugins"`
	Skills          SkillMetrics      `json:"skills"`
	Triggers        TriggerMetrics    `json:"triggers"`
	IntentDistribution map[string]uint64 `json:"intent_distribution"`
}

// PerceptionMetrics represents perception layer snapshot
type PerceptionMetrics struct {
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	P50LatencyMs    float64 `json:"p50_latency_ms"`
	P95LatencyMs    float64 `json:"p95_latency_ms"`
	P99LatencyMs    float64 `json:"p99_latency_ms"`
	TotalClassified uint64  `json:"total_classified"`
}

// PlanningMetrics represents planning layer snapshot
type PlanningMetrics struct {
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	P50LatencyMs    float64 `json:"p50_latency_ms"`
	P95LatencyMs    float64 `json:"p95_latency_ms"`
	AvgSteps        int64   `json:"avg_steps"`
	TotalAdjustments uint64 `json:"total_adjustments"`
}

// ExecutionMetrics represents execution layer snapshot
type ExecutionMetrics struct {
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	P50LatencyMs     float64 `json:"p50_latency_ms"`
	P95LatencyMs     float64 `json:"p95_latency_ms"`
	SuccessRate      float64 `json:"success_rate"`
	TotalSuccess     uint64  `json:"total_success"`
	TotalFailure     uint64  `json:"total_failure"`
	Checkpoints      uint64  `json:"checkpoints_created"`
}

// MemoryMetrics represents memory layer snapshot
type MemoryMetrics struct {
	SizeBytes         int64   `json:"size_bytes"`
	AvgQueryLatencyMs float64 `json:"avg_query_latency_ms"`
	P95LatencyMs      float64 `json:"p95_latency_ms"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
	TotalRetrievals   uint64  `json:"total_retrievals"`
}

// PluginMetrics represents plugin layer snapshot
type PluginMetrics struct {
	TotalLoads   uint64 `json:"total_loads"`
	TotalUsages  uint64 `json:"total_usages"`
	TotalErrors  uint64 `json:"total_errors"`
}

// SkillMetrics represents skill layer snapshot
type SkillMetrics struct {
	TotalCreated    uint64 `json:"total_created"`
	TotalUsages     uint64 `json:"total_usages"`
	PatternsFound   uint64 `json:"patterns_found"`
}

// TriggerMetrics represents trigger layer snapshot
type TriggerMetrics struct {
	TotalNudges     uint64 `json:"total_nudges"`
	TotalAccepted   uint64 `json:"total_accepted"`
	AcceptanceRate  float64 `json:"acceptance_rate"`
}

// Snapshot returns a point-in-time snapshot of all metrics
func (m *Metrics) Snapshot() *MetricsSnapshot {
	m.UpdateSystemMetrics()
	
	snapshot := &MetricsSnapshot{
		Timestamp: time.Now(),
		IntentDistribution: make(map[string]uint64),
	}
	
	// Perception
	snapshot.Perception = PerceptionMetrics{
		AvgLatencyMs:    m.PerceptionLatency.Avg() * 1000,
		P50LatencyMs:    m.PerceptionLatency.Percentile(50) * 1000,
		P95LatencyMs:    m.PerceptionLatency.Percentile(95) * 1000,
		P99LatencyMs:    m.PerceptionLatency.Percentile(99) * 1000,
		TotalClassified: m.IntentClassification.Value(),
	}
	
	// Planning
	snapshot.Planning = PlanningMetrics{
		AvgLatencyMs:     m.PlanningLatency.Avg() * 1000,
		P50LatencyMs:     m.PlanningLatency.Percentile(50) * 1000,
		P95LatencyMs:     m.PlanningLatency.Percentile(95) * 1000,
		AvgSteps:         m.AvgStepsPerPlan.Value(),
		TotalAdjustments: m.PlanAdjustments.Value(),
	}
	
	// Execution
	success := m.ExecutionSuccess.Value()
	failure := m.ExecutionFailure.Value()
	total := success + failure
	var successRate float64
	if total > 0 {
		successRate = float64(success) / float64(total)
	}
	snapshot.Execution = ExecutionMetrics{
		AvgLatencyMs:  m.ExecutionLatency.Avg() * 1000,
		P50LatencyMs:  m.ExecutionLatency.Percentile(50) * 1000,
		P95LatencyMs:  m.ExecutionLatency.Percentile(95) * 1000,
		SuccessRate:   successRate,
		TotalSuccess:  success,
		TotalFailure: failure,
		Checkpoints:  m.CheckpointsCreated.Value(),
	}
	
	// Memory
	hits := m.CacheHits.Value()
	misses := m.CacheMisses.Value()
	cacheTotal := hits + misses
	var cacheHitRate float64
	if cacheTotal > 0 {
		cacheHitRate = float64(hits) / float64(cacheTotal)
	}
	snapshot.Memory = MemoryMetrics{
		SizeBytes:         m.MemorySize.Value(),
		AvgQueryLatencyMs: m.FTSQueryLatency.Avg() * 1000,
		P95LatencyMs:      m.FTSQueryLatency.Percentile(95) * 1000,
		CacheHitRate:      cacheHitRate,
		TotalRetrievals:   m.MemoryRetrievalCount.Value(),
	}
	
	// Plugins
	snapshot.Plugins = PluginMetrics{
		TotalLoads:  m.PluginLoadCount.Value(),
		TotalUsages: m.PluginUsageCount.Value(),
		TotalErrors: m.PluginErrors.Value(),
	}
	
	// Skills
	snapshot.Skills = SkillMetrics{
		TotalCreated:  m.SkillsCreated.Value(),
		TotalUsages:   m.SkillUsageCount.Value(),
		PatternsFound: m.PatternDetections.Value(),
	}
	
	// Triggers
	nudges := m.NudgeCount.Value()
	accepted := m.NudgeAcceptance.Value()
	var nudgeRate float64
	if nudges > 0 {
		nudgeRate = float64(accepted) / float64(nudges)
	}
	snapshot.Triggers = TriggerMetrics{
		TotalNudges:    nudges,
		TotalAccepted:  accepted,
		AcceptanceRate: nudgeRate,
	}
	
	// Intent distribution
	m.mu.RLock()
	for intent, counter := range m.IntentDistribution {
		snapshot.IntentDistribution[intent] = counter.Value()
	}
	m.mu.RUnlock()
	
	return snapshot
}

// ExportJSON exports metrics as JSON
func (m *Metrics) ExportJSON() ([]byte, error) {
	return json.MarshalIndent(m.Snapshot(), "", "  ")
}

// ExportPrometheus exports metrics in Prometheus text format
func (m *Metrics) ExportPrometheus() string {
	var sb strings.Builder
	snapshot := m.Snapshot()
	
	// Helper to write metrics
	writeMetric := func(name, help string, value interface{}) {
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
		sb.WriteString(fmt.Sprintf("# TYPE %s gauge\n", name))
		sb.WriteString(fmt.Sprintf("%s %v\n\n", name, value))
	}
	
	// Perception
	writeMetric("cortex_perception_latency_avg_ms", "Average perception latency in ms", snapshot.Perception.AvgLatencyMs)
	writeMetric("cortex_perception_latency_p95_ms", "P95 perception latency in ms", snapshot.Perception.P95LatencyMs)
	writeMetric("cortex_perception_classified_total", "Total intent classifications", snapshot.Perception.TotalClassified)
	
	// Planning
	writeMetric("cortex_planning_latency_avg_ms", "Average planning latency in ms", snapshot.Planning.AvgLatencyMs)
	writeMetric("cortex_planning_steps_avg", "Average steps per plan", snapshot.Planning.AvgSteps)
	writeMetric("cortex_planning_adjustments_total", "Total plan adjustments", snapshot.Planning.TotalAdjustments)
	
	// Execution
	writeMetric("cortex_execution_latency_avg_ms", "Average execution latency in ms", snapshot.Execution.AvgLatencyMs)
	writeMetric("cortex_execution_success_rate", "Execution success rate", snapshot.Execution.SuccessRate)
	writeMetric("cortex_execution_success_total", "Total successful executions", snapshot.Execution.TotalSuccess)
	writeMetric("cortex_execution_failure_total", "Total failed executions", snapshot.Execution.TotalFailure)
	writeMetric("cortex_checkpoints_created_total", "Total checkpoints created", snapshot.Execution.Checkpoints)
	
	// Memory
	writeMetric("cortex_memory_size_bytes", "Memory store size in bytes", snapshot.Memory.SizeBytes)
	writeMetric("cortex_memory_query_latency_avg_ms", "Average query latency in ms", snapshot.Memory.AvgQueryLatencyMs)
	writeMetric("cortex_memory_cache_hit_rate", "Cache hit rate", snapshot.Memory.CacheHitRate)
	
	// Plugins
	writeMetric("cortex_plugins_loaded_total", "Total plugin loads", snapshot.Plugins.TotalLoads)
	writeMetric("cortex_plugins_usage_total", "Total plugin usages", snapshot.Plugins.TotalUsages)
	writeMetric("cortex_plugins_errors_total", "Total plugin errors", snapshot.Plugins.TotalErrors)
	
	// Skills
	writeMetric("cortex_skills_created_total", "Total skills created", snapshot.Skills.TotalCreated)
	writeMetric("cortex_skills_usage_total", "Total skill usages", snapshot.Skills.TotalUsages)
	
	// Triggers
	writeMetric("cortex_nudges_total", "Total nudges sent", snapshot.Triggers.TotalNudges)
	writeMetric("cortex_nudges_accepted_total", "Total nudges accepted", snapshot.Triggers.TotalAccepted)
	writeMetric("cortex_nudges_acceptance_rate", "Nudge acceptance rate", snapshot.Triggers.AcceptanceRate)
	
	return sb.String()
}


// Reset resets all metrics
func (m *Metrics) Reset() {
	m.PerceptionLatency = NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0})
	m.IntentClassification.Reset()
	m.PlanningLatency = NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0})
	m.AvgStepsPerPlan.Set(0)
	m.PlanAdjustments.Reset()
	m.ExecutionLatency = NewHistogram([]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0})
	m.ExecutionSuccess.Reset()
	m.ExecutionFailure.Reset()
	m.CheckpointsCreated.Reset()
	m.CheckpointsRestored.Reset()
	m.MemorySize.Set(0)
	m.FTSQueryLatency = NewHistogram([]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5})
	m.MemoryRetrievalCount.Reset()
	m.CacheHits.Reset()
	m.CacheMisses.Reset()
	m.PluginLoadCount.Reset()
	m.PluginUsageCount.Reset()
	m.PluginErrors.Reset()
	m.SkillsCreated.Reset()
	m.SkillUsageCount.Reset()
	m.PatternDetections.Reset()
	m.NudgeCount.Reset()
	m.NudgeAcceptance.Reset()
	m.ReviewCount.Reset()
	m.ActiveGoroutines.Set(0)
	m.startTime = time.Now()
	
	m.mu.Lock()
	m.IntentDistribution = make(map[string]*Counter)
	m.mu.Unlock()
}
