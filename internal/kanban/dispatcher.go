package kanban

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// WorkerSpawner spawns a worker agent to execute a kanban task.
type WorkerSpawner func(task *Task) error

// Dispatcher manages task dispatch and lifecycle
type Dispatcher struct {
	db                     *KanbanDB
	tickInterval           time.Duration
	maxRetries             int
	maxConsecutiveFailures int
	consecutiveFailures    int
	running                bool
	stopCh                 chan struct{}
	wg                     sync.WaitGroup
	mu                     sync.Mutex
	spawner                WorkerSpawner
}

// NewDispatcher creates a new dispatcher
func NewDispatcher(db *KanbanDB) *Dispatcher {
	return &Dispatcher{
		db:                     db,
		tickInterval:           60 * time.Second,
		maxRetries:             3,
		maxConsecutiveFailures: 5,
		stopCh:                 make(chan struct{}),
	}
}

// SetSpawner sets the worker spawner function.
func (d *Dispatcher) SetSpawner(s WorkerSpawner) {
	d.spawner = s
}

// SetTickInterval sets the tick interval
func (d *Dispatcher) SetTickInterval(interval time.Duration) {
	d.tickInterval = interval
}

// SetMaxRetries sets the maximum retries
func (d *Dispatcher) SetMaxRetries(max int) {
	d.maxRetries = max
}

// SetMaxConsecutiveFailures sets the maximum consecutive failures before circuit break
func (d *Dispatcher) SetMaxConsecutiveFailures(max int) {
	d.maxConsecutiveFailures = max
}

// Start starts the dispatcher background loop
func (d *Dispatcher) Start() {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.stopCh = make(chan struct{})
	d.mu.Unlock()

	d.wg.Add(1)
	go d.runLoop()

	log.Infof("[Dispatcher] Started with tick interval %v", d.tickInterval)
}

// Stop stops the dispatcher
func (d *Dispatcher) Stop() {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	d.running = false
	close(d.stopCh)
	d.mu.Unlock()

	d.wg.Wait()
	log.Infof("[Dispatcher] Stopped")
}

func (d *Dispatcher) runLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.tickInterval)
	defer ticker.Stop()

	// Run once immediately
	d.Tick()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.Tick()
		}
	}
}

// Tick performs one dispatch cycle
func (d *Dispatcher) Tick() error {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	log.Debugf("[Dispatcher] Running tick cycle")

	// Step 1: Recover timed-out running tasks
	if err := d.recoverTimedOutTasks(); err != nil {
		log.Warnf("[Dispatcher] Failed to recover timed-out tasks: %v", err)
		d.recordFailure()
	}

	// Step 2: Check crashed processes
	if err := d.checkCrashedProcesses(); err != nil {
		log.Warnf("[Dispatcher] Failed to check crashed processes: %v", err)
		d.recordFailure()
	}

	// Step 3: Promote todo tasks to ready when all parents are done
	if err := d.promoteReadyTasks(); err != nil {
		log.Warnf("[Dispatcher] Failed to promote ready tasks: %v", err)
		d.recordFailure()
	}

	// Step 4: Claim and spawn workers for ready tasks
	if err := d.dispatchReadyTasks(); err != nil {
		log.Warnf("[Dispatcher] Failed to dispatch ready tasks: %v", err)
		d.recordFailure()
		return err
	}

	// All steps succeeded
	d.recordSuccess()

	return nil
}

// recoverTimedOutTasks recovers tasks that have exceeded their max runtime
func (d *Dispatcher) recoverTimedOutTasks() error {
	runningTasks, err := d.db.GetRunningTasks()
	if err != nil {
		return err
	}

	now := time.Now()
	for _, task := range runningTasks {
		if task.MaxRuntimeSeconds <= 0 {
			continue // No timeout set
		}

		elapsed := now.Sub(task.CreatedAt)
		if elapsed > time.Duration(task.MaxRuntimeSeconds)*time.Second {
			log.Infof("[Dispatcher] Task %s timed out (running for %v, max: %ds)",
				task.ID, elapsed, task.MaxRuntimeSeconds)

			// Update run status
			run, err := d.db.GetCurrentRun(task.ID)
			if err == nil && run != nil {
				finishedAt := now
				run.Status = RunStatusTimedOut
				run.FinishedAt = &finishedAt
				run.Summary = fmt.Sprintf("Task timed out after %d seconds", task.MaxRuntimeSeconds)
				d.db.UpdateRun(run)
			}

			// Update task status back to ready
			if err := d.db.UpdateTaskStatus(task.ID, string(StatusReady), "Task timed out, reverted to ready"); err != nil {
				log.Warnf("[Dispatcher] Failed to revert task %s to ready: %v", task.ID, err)
			}

			// Add timeout event
			event := &Event{
				ID:        generateID("evt"),
				TaskID:    task.ID,
				EventType: EventTimeout,
				Payload:   fmt.Sprintf(`{"max_runtime":%d}`, task.MaxRuntimeSeconds),
			}
			d.db.AddEvent(event)
		}
	}

	return nil
}

// checkCrashedProcesses checks if running task processes have crashed
func (d *Dispatcher) checkCrashedProcesses() error {
	runningTasks, err := d.db.GetRunningTasks()
	if err != nil {
		return err
	}

	for _, task := range runningTasks {
		run, err := d.db.GetCurrentRun(task.ID)
		if err != nil || run == nil {
			continue
		}

		if run.PID <= 0 {
			continue // No PID to check
		}

		// Check retry count
		if d.maxRetries > 0 && run.RetryCount >= d.maxRetries {
			log.Infof("[Dispatcher] Task %s exceeded max retries (%d), marking as failed", task.ID, run.RetryCount)
			finishedAt := time.Now()
			run.Status = RunStatusFailed
			run.FinishedAt = &finishedAt
			run.Summary = fmt.Sprintf("Task failed after %d retries", run.RetryCount)
			d.db.UpdateRun(run)
			d.db.UpdateTaskStatus(task.ID, string(StatusBlocked), fmt.Sprintf("Exceeded max retries (%d)", d.maxRetries))
			continue
		}

		// Check if process exists
		proc, err := os.FindProcess(run.PID)
		if err != nil {
			log.Warnf("[Dispatcher] Failed to find process %d for task %s: %v", run.PID, task.ID, err)
			continue
		}

		// Try to signal the process (0 doesn't kill, just checks)
		if err := proc.Signal(os.Signal(nil)); err != nil {
			// Process doesn't exist or is not accessible
			log.Infof("[Dispatcher] Process %d for task %s appears to have crashed", run.PID, task.ID)

			// Update run status
			finishedAt := time.Now()
			run.Status = RunStatusCrashed
			run.FinishedAt = &finishedAt
			run.Summary = "Process crashed"
			d.db.UpdateRun(run)

			// Revert task to ready for retry
			if err := d.db.UpdateTaskStatus(task.ID, string(StatusReady), "Process crashed, reverted to ready for retry"); err != nil {
				log.Warnf("[Dispatcher] Failed to revert task %s to ready: %v", task.ID, err)
			}

			// Add crash event
			event := &Event{
				ID:        generateID("evt"),
				TaskID:    task.ID,
				EventType: EventCrash,
				Payload:   fmt.Sprintf(`{"pid":%d}`, run.PID),
			}
			d.db.AddEvent(event)
		}
	}

	return nil
}

// promoteReadyTasks promotes todo tasks to ready when all parents are done
func (d *Dispatcher) promoteReadyTasks() error {
	todoTasks, err := d.db.GetTodoTasks()
	if err != nil {
		return err
	}

	for _, task := range todoTasks {
		allParentsDone, err := d.db.AreAllParentsDone(task.ID)
		if err != nil {
			log.Warnf("[Dispatcher] Failed to check parents for task %s: %v", task.ID, err)
			continue
		}

		if allParentsDone {
			log.Infof("[Dispatcher] Promoting task %s from todo to ready", task.ID)
			if err := d.db.UpdateTaskStatus(task.ID, string(StatusReady), "All parent tasks completed"); err != nil {
				log.Warnf("[Dispatcher] Failed to promote task %s: %v", task.ID, err)
			}
		}
	}

	return nil
}

// dispatchReadyTasks claims ready tasks and spawns workers
func (d *Dispatcher) dispatchReadyTasks() error {
	readyTasks, err := d.db.GetReadyTasks()
	if err != nil {
		return err
	}

	for _, task := range readyTasks {
		// Add spawned/pending event
		event := &Event{
			ID:        generateID("evt"),
			TaskID:    task.ID,
			EventType: "spawned",
			Payload:   fmt.Sprintf(`{"title":"%s","assignee":"%s","priority":%d}`, task.Title, task.Assignee, task.Priority),
		}
		d.db.AddEvent(event)

		// Spawn worker agent if configured
		if d.spawner != nil {
			t := task // capture
			go func() {
				if err := d.spawner(t); err != nil {
					log.Errorf("[Dispatcher] Spawn worker for task %s: %v", t.ID, err)
				}
			}()
		}
		log.Debugf("[Dispatcher] Ready task: %s (%s)", task.ID, task.Title)
	}

	return nil
}

func (d *Dispatcher) recordFailure() {
	d.mu.Lock()
	d.consecutiveFailures++
	d.mu.Unlock()

	if d.consecutiveFailures >= d.maxConsecutiveFailures {
		log.Warnf("[Dispatcher] Circuit breaker: %d consecutive failures, pausing", d.consecutiveFailures)
	}
}

func (d *Dispatcher) recordSuccess() {
	d.mu.Lock()
	d.consecutiveFailures = 0
	d.mu.Unlock()
}

// IsCircuitBroken returns true if the dispatcher has too many consecutive failures
func (d *Dispatcher) IsCircuitBroken() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.consecutiveFailures >= d.maxConsecutiveFailures
}

// ResetFailures resets the consecutive failure counter
func (d *Dispatcher) ResetFailures() {
	d.mu.Lock()
	d.consecutiveFailures = 0
	d.mu.Unlock()
}
