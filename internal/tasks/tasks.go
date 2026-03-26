// Package tasks manages background goroutines and collects their results
// for injection into the next conversation turn.
package tasks

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var taskCounter int64

// TaskResult holds the outcome of a completed background task.
type TaskResult struct {
	ID      string
	Label   string
	Output  string
	Elapsed time.Duration
	Err     error
}

// Manager spawns background goroutines and accumulates their results until
// the caller drains them via DrainUpdates.
type Manager struct {
	mu      sync.Mutex
	pending []TaskResult
	active  atomic.Int32
}

// NewManager returns an initialised Manager.
func NewManager() *Manager { return &Manager{} }

// Spawn launches fn in a goroutine and returns a task ID immediately.
// When fn returns, the result is appended to the pending queue.
func (m *Manager) Spawn(label string, fn func() (string, error)) string {
	id := fmt.Sprintf("task-%d", atomic.AddInt64(&taskCounter, 1))
	m.active.Add(1)
	go func() {
		start := time.Now()
		out, err := fn()
		m.mu.Lock()
		m.pending = append(m.pending, TaskResult{
			ID:      id,
			Label:   label,
			Output:  out,
			Elapsed: time.Since(start),
			Err:     err,
		})
		m.mu.Unlock()
		m.active.Add(-1)
	}()
	return id
}

// DrainUpdates atomically returns and clears all completed results.
// Returns nil if nothing is pending.
func (m *Manager) DrainUpdates() []TaskResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pending) == 0 {
		return nil
	}
	out := m.pending
	m.pending = nil
	return out
}

// HasPending returns true if there are results not yet delivered.
func (m *Manager) HasPending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending) > 0
}

// ActiveCount returns the number of goroutines still running.
func (m *Manager) ActiveCount() int { return int(m.active.Load()) }
