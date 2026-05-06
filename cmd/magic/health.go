package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

var (
	gatewayStartTime time.Time
	healthMu         sync.RWMutex
	platformsStatus  map[string]bool // platform name -> healthy
)

func init() {
	gatewayStartTime = time.Now()
	platformsStatus = make(map[string]bool)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	healthMu.RLock()
	defer healthMu.RUnlock()

	cfg, err := config.Load()
	if err != nil {
		http.Error(w, `{"status":"error","message":"failed to load config"}`, http.StatusInternalServerError)
		return
	}

	// Count healthy platforms
	healthyCount := 0
	totalCount := 0
	for name, plat := range cfg.Gateway.Platforms {
		if plat.Enabled {
			totalCount++
			if platformsStatus[name] {
				healthyCount++
			}
		}
	}

	status := "healthy"
	if healthyCount == 0 && totalCount > 0 {
		status = "degraded"
	}

	resp := map[string]interface{}{
		"status":         status,
		"uptime_seconds": time.Since(gatewayStartTime).Seconds(),
		"start_time":     gatewayStartTime.Format(time.RFC3339),
		"platforms": map[string]interface{}{
			"total":   totalCount,
			"healthy": healthyCount,
		},
		"version": Version,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func startHealthServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		ctxShutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctxShutdown)
	}()

	fmt.Println("[Health] Starting health check server on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Printf("[Health] Failed to start health check server: %v\n", err)
	}
}
