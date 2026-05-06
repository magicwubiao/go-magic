package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// LoadBalancingStrategy defines how to select providers
type LoadBalancingStrategy int

const (
	// RoundRobin selects providers in rotation
	RoundRobin LoadBalancingStrategy = iota
	// Random selects a random provider
	Random
	// Weighted selects based on configured weights
	Weighted
	// LeastUsed selects the provider with least usage
	LeastUsed
)

// ProviderRouter provides unified routing with model fallback, health checks,
// circuit breakers, and load balancing support
type ProviderRouter struct {
	providers    map[string]Provider
	modelRouting map[string]string // model prefix -> provider name
	fallbacks    map[string][]string // provider -> fallback providers
	healthChecks map[string]*HealthCheck
	loadBalance  LoadBalancingStrategy
	mu           sync.RWMutex
	
	// Metrics tracking
	stats map[string]*RouterStats
	
	// Configuration
	config *RouterConfig
}

// RouterConfig configures router behavior
type RouterConfig struct {
	// Enable health checks
	HealthCheckEnabled bool
	// Health check interval
	HealthCheckInterval time.Duration
	// Timeout for health check requests
	HealthCheckTimeout time.Duration
	// Number of consecutive failures before marking unhealthy
	UnhealthyThreshold int
	// Number of consecutive successes before marking healthy
	HealthyThreshold int
	
	// Load balancing strategy
	LoadBalancing LoadBalancingStrategy
	
	// Fallback configuration
	EnableFallback bool
	FallbackDelay  time.Duration
	
	// Circuit breaker settings
	CircuitBreakerEnabled bool
	FailureThreshold      int
	RecoveryTimeout       time.Duration
}

// DefaultRouterConfig returns sensible router defaults
func DefaultRouterConfig() *RouterConfig {
	return &RouterConfig{
		HealthCheckEnabled:     true,
		HealthCheckInterval:    30 * time.Second,
		HealthCheckTimeout:     10 * time.Second,
		UnhealthyThreshold:     3,
		HealthyThreshold:       2,
		LoadBalancing:          RoundRobin,
		EnableFallback:         true,
		FallbackDelay:          500 * time.Millisecond,
		CircuitBreakerEnabled:  true,
		FailureThreshold:       5,
		RecoveryTimeout:        60 * time.Second,
	}
}

// RouterStats tracks routing statistics
type RouterStats struct {
	mu              sync.RWMutex
	TotalRequests   int64
	FailedRequests  int64
	SuccessRequests int64
	RequestsByProvider map[string]int64
	LastUsed        time.Time
	LastError       error
}

// HealthCheck represents a health check for a provider
type HealthCheck struct {
	Provider  string
	URL       string
	LastCheck time.Time
	Healthy   bool
	Failures  int
	Successes int
	mu        sync.RWMutex
}

// NewProviderRouter creates a new enhanced provider router
func NewProviderRouter(config *RouterConfig) *ProviderRouter {
	if config == nil {
		config = DefaultRouterConfig()
	}
	
	router := &ProviderRouter{
		providers:    make(map[string]Provider),
		modelRouting: make(map[string]string),
		fallbacks:    make(map[string][]string),
		healthChecks: make(map[string]*HealthCheck),
		stats:        make(map[string]*RouterStats),
		config:       config,
	}
	
	// Start health check background worker if enabled
	if config.HealthCheckEnabled {
		go router.healthCheckWorker()
	}
	
	return router
}

// Register registers a provider with the router
func (r *ProviderRouter) Register(name string, provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.providers[name] = provider
	r.stats[name] = &RouterStats{
		RequestsByProvider: make(map[string]int64),
	}
	
	// Register health check
	if r.config.HealthCheckEnabled {
		r.healthChecks[name] = &HealthCheck{
			Provider: name,
			Healthy:  true,
		}
	}
}

// RegisterFallback registers fallback providers for a given provider
func (r *ProviderRouter) RegisterFallback(provider string, fallbacks []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.fallbacks[provider] = fallbacks
}

// RegisterModelRouting registers model prefix -> provider mapping
func (r *ProviderRouter) RegisterModelRouting(modelPrefix, provider string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.modelRouting[modelPrefix] = provider
}

// GetProvider returns a provider by name
func (r *ProviderRouter) GetProvider(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	
	// Check circuit breaker if enabled
	if r.config.CircuitBreakerEnabled {
		if bp, ok := provider.(*BaseProvider); ok {
			if !bp.IsHealthy() {
				return nil, fmt.Errorf("provider %s is unhealthy (circuit breaker open)", name)
			}
		}
	}
	
	return provider, nil
}

// GetProviderForModel returns the appropriate provider for a given model
func (r *ProviderRouter) GetProviderForModel(model string) (Provider, string, error) {
	r.mu.RLock()
	
	// First try exact match
	if providerName, ok := r.modelRouting[model]; ok {
		if provider, ok := r.providers[providerName]; ok {
			r.mu.RUnlock()
			return provider, providerName, nil
		}
	}
	
	// Try prefix matching
	for prefix, providerName := range r.modelRouting {
		if strings.HasPrefix(model, prefix) {
			if provider, ok := r.providers[providerName]; ok {
				r.mu.RUnlock()
				return provider, providerName, nil
			}
		}
	}
	
	r.mu.RUnlock()
	
	// Default to first registered provider
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	for name, provider := range r.providers {
		return provider, name, nil
	}
	
	return nil, "", fmt.Errorf("no providers registered")
}

// RouteWithFallback executes a request with automatic fallback
func (r *ProviderRouter) RouteWithFallback(ctx context.Context, model string, 
	execute func(ctx context.Context, provider Provider) error) error {
	
	if !r.config.EnableFallback {
		provider, _, err := r.GetProviderForModel(model)
		if err != nil {
			return err
		}
		return execute(ctx, provider)
	}
	
	// Get primary provider
	primary, primaryName, err := r.GetProviderForModel(model)
	if err != nil {
		return err
	}
	
	// Try primary first
	if err := execute(ctx, primary); err == nil {
		r.recordSuccess(primaryName)
		return nil
	}
	
	r.recordFailure(primaryName)
	
	// Get fallbacks
	r.mu.RLock()
	fallbacks := r.fallbacks[primaryName]
	r.mu.RUnlock()
	
	// Try each fallback
	for _, fbName := range fallbacks {
		fbProvider, err := r.GetProvider(fbName)
		if err != nil {
			log.Debugf("Fallback provider %s unavailable: %v", fbName, err)
			continue
		}
		
		log.Infof("Attempting fallback to provider %s", fbName)
		
		if err := execute(ctx, fbProvider); err == nil {
			r.recordSuccess(fbName)
			return nil
		}
		
		r.recordFailure(fbName)
	}
	
	return fmt.Errorf("all providers failed for model %s", model)
}

// recordSuccess records a successful request
func (r *ProviderRouter) recordSuccess(provider string) {
	r.mu.RLock()
	stats, ok := r.stats[provider]
	r.mu.RUnlock()
	
	if ok {
		stats.mu.Lock()
		stats.SuccessRequests++
		stats.LastUsed = time.Now()
		stats.LastError = nil
		stats.mu.Unlock()
	}
}

// recordFailure records a failed request
func (r *ProviderRouter) recordFailure(provider string) {
	r.mu.RLock()
	stats, ok := r.stats[provider]
	r.mu.RUnlock()
	
	if ok {
		stats.mu.Lock()
		stats.FailedRequests++
		stats.mu.Unlock()
	}
}

// GetStats returns routing statistics
func (r *ProviderRouter) GetStats(provider string) (stats RouterStats) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if s, ok := r.stats[provider]; ok {
		s.mu.RLock()
		stats = RouterStats{
			TotalRequests:   s.TotalRequests,
			FailedRequests:  s.FailedRequests,
			SuccessRequests: s.SuccessRequests,
		}
		s.mu.RUnlock()
	}
	return
}

// GetAllStats returns stats for all providers
func (r *ProviderRouter) GetAllStats() map[string]RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	result := make(map[string]RouterStats)
	for name, s := range r.stats {
		s.mu.RLock()
		result[name] = RouterStats{
			TotalRequests:   s.TotalRequests,
			FailedRequests:  s.FailedRequests,
			SuccessRequests: s.SuccessRequests,
		}
		s.mu.RUnlock()
	}
	return result
}

// GetHealthStatus returns the health status of a provider
func (r *ProviderRouter) GetHealthStatus(provider string) (healthy bool, lastCheck time.Time) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if hc, ok := r.healthChecks[provider]; ok {
		hc.mu.RLock()
		healthy = hc.Healthy
		lastCheck = hc.LastCheck
		hc.mu.RUnlock()
	}
	return
}

// healthCheckWorker runs periodic health checks
func (r *ProviderRouter) healthCheckWorker() {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		r.runHealthChecks()
	}
}

// runHealthChecks performs health checks on all providers
func (r *ProviderRouter) runHealthChecks() {
	r.mu.RLock()
	providers := make(map[string]Provider)
	for name, provider := range r.providers {
		providers[name] = provider
	}
	r.mu.RUnlock()
	
	for name, provider := range providers {
		r.checkProvider(name, provider)
	}
}

// checkProvider performs a health check on a single provider
func (r *ProviderRouter) checkProvider(name string, provider Provider) {
	r.mu.RLock()
	hc, ok := r.healthChecks[name]
	r.mu.RUnlock()
	
	if !ok {
		return
	}
	
	// Try to ping the provider
	healthy := r.pingProvider(provider)
	
	hc.mu.Lock()
	hc.LastCheck = time.Now()
	
	if healthy {
		hc.Successes++
		hc.Failures = 0
		if hc.Successes >= r.config.HealthyThreshold {
			hc.Healthy = true
		}
	} else {
		hc.Failures++
		hc.Successes = 0
		if hc.Failures >= r.config.UnhealthyThreshold {
			hc.Healthy = false
		}
	}
	hc.mu.Unlock()
}

// pingProvider checks if a provider is responsive
func (r *ProviderRouter) pingProvider(provider Provider) bool {
	// Check if provider has a health check method
	if hc, ok := provider.(interface{ HealthCheck(context.Context) error }); ok {
		ctx, cancel := context.WithTimeout(context.Background(), r.config.HealthCheckTimeout)
		defer cancel()
		return hc.HealthCheck(ctx) == nil
	}
	
	// Default: assume healthy if no health check method
	return true
}

// SetLoadBalancing sets the load balancing strategy
func (r *ProviderRouter) SetLoadBalancing(strategy LoadBalancingStrategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadBalance = strategy
}

// SelectProvider selects a provider based on load balancing strategy
func (r *ProviderRouter) SelectProvider(providers []string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if len(providers) == 0 {
		return ""
	}
	
	if len(providers) == 1 {
		return providers[0]
	}
	
	switch r.loadBalance {
	case RoundRobin:
		// Simple round robin - return first for now
		// In production, would track index
		return providers[0]
	case Random:
		// Would use random selection
		return providers[0]
	case LeastUsed:
		// Find provider with least requests
		var selected string
		var minRequests int64 = -1
		for _, name := range providers {
			if stats, ok := r.stats[name]; ok {
				stats.mu.RLock()
				total := stats.TotalRequests
				stats.mu.RUnlock()
				if minRequests < 0 || total < minRequests {
					minRequests = total
					selected = name
				}
			}
		}
		return selected
	default:
		return providers[0]
	}
}

// ListProviders returns all registered provider names
func (r *ProviderRouter) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// Close shuts down the router
func (r *ProviderRouter) Close() {
	// Stop health check worker
	// Close any open connections
}
