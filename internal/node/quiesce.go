package node

import (
	"sync"
	"sync/atomic"
)

// writeGate synchronizes write handlers and quiesce using an RWMutex.
// Writes acquire a shared lock (RLock); quiesce acquires an exclusive lock (Lock).
// Once quiesced, all future Enter() calls return ErrNodeQuiesced without acquiring any lock.
//
// Scope: primary/standalone nodes only. Follower nodes reject writes at a higher level.
type writeGate struct {
	mu       sync.RWMutex
	quiesced atomic.Bool
}

// Enter acquires a shared write permit. Returns ErrNodeQuiesced if quiesced.
// Caller must call Exit() when the write is complete (only if Enter returned nil).
func (g *writeGate) Enter() error {
	// Fast path: if already quiesced, skip the lock entirely.
	if g.quiesced.Load() {
		return ErrNodeQuiesced
	}
	g.mu.RLock()
	// Double-check after acquiring the lock: Quiesce() may have set the flag
	// between the fast-path check and RLock acquisition.
	if g.quiesced.Load() {
		g.mu.RUnlock()
		return ErrNodeQuiesced
	}
	return nil // caller must call Exit()
}

// Exit releases the shared write permit.
func (g *writeGate) Exit() { g.mu.RUnlock() }

// Quiesce takes exclusive control, draining all in-flight writes.
// After this returns, all future Enter() calls return ErrNodeQuiesced.
// Safe to call multiple times (idempotent).
func (g *writeGate) Quiesce() {
	g.mu.Lock()
	g.quiesced.Store(true)
	g.mu.Unlock()
}

// IsQuiesced reports whether the gate has been quiesced.
func (g *writeGate) IsQuiesced() bool { return g.quiesced.Load() }
