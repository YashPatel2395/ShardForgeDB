package replnet

import (
	"fmt"
	"sync"
	"time"
)

// DefaultEntriesLimit is the maximum number of entries returned by EntriesAfter when limit <= 0.
const DefaultEntriesLimit = 1000

// Log is an in-memory, append-only mutation log for a primary node.
//
// Entries are assigned monotonically increasing sequence numbers starting at 1.
// The log is goroutine-safe. It is NOT persisted to disk; it is cleared on restart.
type Log struct {
	mu      sync.RWMutex
	entries []Entry
	nextSeq uint64 // next sequence number to assign; starts at 1
	closed  bool
}

// NewLog creates a new, empty mutation log.
func NewLog() *Log {
	return &Log{nextSeq: 1}
}

// Append records a new mutation in the log and returns the assigned Entry.
// op must be OpPut or OpDelete; key must be non-empty.
// Returns ErrClosed if the log is closed.
func (l *Log) Append(op Operation, key, value string) (Entry, error) {
	if op != OpPut && op != OpDelete {
		return Entry{}, fmt.Errorf("%w: op must be %q or %q", ErrInvalidEntry, OpPut, OpDelete)
	}
	if key == "" {
		return Entry{}, fmt.Errorf("%w: key must not be empty", ErrInvalidEntry)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return Entry{}, ErrClosed
	}

	e := Entry{
		Seq:       l.nextSeq,
		Op:        op,
		Key:       key,
		Value:     value,
		Timestamp: time.Now().UTC(),
	}
	l.entries = append(l.entries, e)
	l.nextSeq++
	return e, nil
}

// EntriesAfter returns up to limit entries whose Seq > after, in ascending order.
// If limit <= 0, DefaultEntriesLimit is used.
// Returns ErrClosed if the log is closed.
func (l *Log) EntriesAfter(after uint64, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = DefaultEntriesLimit
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return nil, ErrClosed
	}

	result := make([]Entry, 0, limit)
	for _, e := range l.entries {
		if e.Seq > after {
			result = append(result, e)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// Stats returns a point-in-time snapshot of the log's state.
// Returns ErrClosed if the log is closed.
func (l *Log) Stats() (LogStats, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return LogStats{}, ErrClosed
	}

	var lastSeq uint64
	if n := len(l.entries); n > 0 {
		lastSeq = l.entries[n-1].Seq
	}
	return LogStats{
		Count:   len(l.entries),
		LastSeq: lastSeq,
	}, nil
}

// Close marks the log as closed. All subsequent operations return ErrClosed.
// Safe to call multiple times.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}
