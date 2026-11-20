package server

import (
	"context"
	"sync/atomic"
)

// Readiness combines application startup/shutdown state with persistence health.
// Construct it with NewReadiness. Its methods support concurrent use.
type Readiness struct {
	started          atomic.Bool
	stopping         atomic.Bool
	context          context.Context
	persistenceReady func() bool
}

// NewReadiness starts unready. The persistence callback must be nonblocking and
// perform no I/O or cache/AOF locking. A missing context or callback stays unready.
func NewReadiness(ctx context.Context, persistenceReady func() bool) *Readiness {
	return &Readiness{context: ctx, persistenceReady: persistenceReady}
}

// MarkReady is called after successful recovery and worker initialization.
// It cannot reverse BeginShutdown, context cancellation, or persistence failure.
func (r *Readiness) MarkReady() {
	r.started.Store(true)
}

// BeginShutdown permanently withdraws readiness for this application instance.
func (r *Readiness) BeginShutdown() {
	r.stopping.Store(true)
}

// Ready is a lightweight state check; it does not inspect cache entries or files.
func (r *Readiness) Ready() bool {
	return r != nil && r.started.Load() && !r.stopping.Load() &&
		r.context != nil && r.context.Err() == nil &&
		r.persistenceReady != nil && r.persistenceReady()
}
