package replica

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/YashPatel2395/ShardForgeDB/internal/engine"
)

// replicaHandle holds one replica's metadata, engine, and in-memory state.
type replicaHandle struct {
	info         ReplicaInfo
	eng          *engine.Engine
	appliedIndex LogIndex

	// Simulation controls (in-memory only, not persisted).
	paused          bool
	maxApplyPerCall int // 0 means unlimited
}

// Store is a local in-process leader/follower replication simulation.
//
// All public methods are safe for concurrent use.
//
// # Locking model
//
// s.mu is a reader-writer lock. Every public method that accesses replica
// engines holds s.mu.RLock() for the duration of engine calls. Close holds
// s.mu.Lock() while closing all engines and the log. This prevents Close from
// racing with in-flight operations.
//
// ReplicateOnce and ReplicateAll apply operations to follower engines while
// holding s.mu.RLock, which serialises them with Put/Delete safely.
type Store struct {
	mu       sync.RWMutex
	opts     Options
	replicas []*replicaHandle
	log      *opLog
	leaderID int

	// commitIndex is the log index of the last committed operation.
	// Advanced by Put/Delete under the write path.
	commitIndex LogIndex

	closed bool
}

// Open opens or creates a replica Store rooted at opts.Dir.
//
// On first open, opts.ReplicaCount must be > 0. opts.LeaderID defaults to 0.
// opts.ReplicaPrefix defaults to "replica".
//
// On reopen (REPLICATION.json exists):
//   - ReplicaCount==0 → load from manifest.
//   - LeaderID<0 → load from manifest.
//   - Non-zero ReplicaCount that differs from manifest → ErrReplicaMismatch.
//   - Non-negative LeaderID that differs from manifest → ErrReplicaMismatch.
func Open(opts Options) (*Store, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("%w: Dir is required", ErrInvalidOptions)
	}
	if opts.ReplicaPrefix == "" {
		opts.ReplicaPrefix = defaultReplicaPrefix
	}

	manifestPath := filepath.Join(opts.Dir, manifestFileName)
	manifestExists := pathExists(manifestPath)

	var m *replicationManifest

	if manifestExists {
		loaded, err := readManifest(opts.Dir)
		if err != nil {
			return nil, err
		}
		m = loaded

		// Validate caller options against manifest.
		if opts.ReplicaCount != 0 && opts.ReplicaCount != m.ReplicaCount {
			return nil, fmt.Errorf("%w: ReplicaCount %d != manifest %d",
				ErrReplicaMismatch, opts.ReplicaCount, m.ReplicaCount)
		}
		if opts.LeaderID >= 0 && opts.LeaderID != m.LeaderID {
			return nil, fmt.Errorf("%w: LeaderID %d != manifest %d",
				ErrReplicaMismatch, opts.LeaderID, m.LeaderID)
		}
		// Override options with manifest values.
		opts.ReplicaCount = m.ReplicaCount
		opts.LeaderID = m.LeaderID
		opts.ReplicaPrefix = m.ReplicaPrefix
	} else {
		// First open.
		if opts.ReplicaCount <= 0 {
			return nil, fmt.Errorf("%w: ReplicaCount must be > 0 on first open", ErrInvalidOptions)
		}
		if opts.LeaderID < 0 {
			opts.LeaderID = 0
		}
		if opts.LeaderID >= opts.ReplicaCount {
			return nil, fmt.Errorf("%w: LeaderID %d out of range [0, %d)",
				ErrInvalidOptions, opts.LeaderID, opts.ReplicaCount)
		}

		entries := make([]replicaEntry, opts.ReplicaCount)
		for i := 0; i < opts.ReplicaCount; i++ {
			name := fmt.Sprintf("%s-%04d", opts.ReplicaPrefix, i)
			relPath := filepath.Join(replicasDir, name)
			role := RoleFollower
			if i == opts.LeaderID {
				role = RoleLeader
			}
			entries[i] = replicaEntry{ID: i, Role: role, Name: name, Path: relPath}
		}
		m = &replicationManifest{
			Version:       manifestVersion,
			ReplicaCount:  opts.ReplicaCount,
			LeaderID:      opts.LeaderID,
			ReplicaPrefix: opts.ReplicaPrefix,
			Replicas:      entries,
		}
		if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
			return nil, fmt.Errorf("replica: create dir: %w", err)
		}
		if err := writeManifest(opts.Dir, m); err != nil {
			return nil, err
		}
	}

	// Open replication log.
	logPath := filepath.Join(opts.Dir, replogDir, replogFile)
	ol, err := openLog(logPath)
	if err != nil {
		return nil, fmt.Errorf("replica: open log: %w", err)
	}

	// Open each replica engine.
	handles := make([]*replicaHandle, 0, len(m.Replicas))
	for _, entry := range m.Replicas {
		absPath := filepath.Join(opts.Dir, entry.Path)
		if err := os.MkdirAll(absPath, 0o755); err != nil {
			closeHandles(handles, ol)
			return nil, fmt.Errorf("replica: create replica dir %q: %w", entry.Name, err)
		}
		eng, err := engine.Open(engine.Options{Dir: absPath})
		if err != nil {
			closeHandles(handles, ol)
			return nil, fmt.Errorf("replica: open engine for replica %q: %w", entry.Name, err)
		}
		// Load applied index from disk.
		applied := loadAppliedIndex(absPath)
		handles = append(handles, &replicaHandle{
			info: ReplicaInfo{
				ID:   entry.ID,
				Role: entry.Role,
				Name: entry.Name,
				Path: entry.Path,
			},
			eng:          eng,
			appliedIndex: applied,
		})
	}

	// The commit index is the last index in the log (leader-commit semantics:
	// everything in the log is committed).
	commitIndex := ol.lastIndex()

	return &Store{
		opts:        opts,
		replicas:    handles,
		log:         ol,
		leaderID:    opts.LeaderID,
		commitIndex: commitIndex,
	}, nil
}

// ── Write operations ──────────────────────────────────────────────────────────

// Put writes key→value to the leader, appends the operation to the replication
// log, and advances the commit index. Returns the log index of the operation.
//
// Empty key returns ErrInvalidKey. Operation after Close returns ErrClosed.
func (s *Store) Put(key, value []byte) (LogIndex, error) {
	if len(key) == 0 {
		return 0, ErrInvalidKey
	}
	keyCopy := copyBytes(key)
	valCopy := copyBytes(value)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}

	// Append to log first.
	idx, err := s.log.append(Operation{Type: OpPut, Key: keyCopy, Value: valCopy})
	if err != nil {
		return 0, fmt.Errorf("replica: log append: %w", err)
	}

	// Apply to leader engine.
	leader := s.replicas[s.leaderID]
	if err := leader.eng.Put(keyCopy, valCopy); err != nil {
		// Log is appended but not committed; leave commitIndex unchanged.
		// The record exists in the log. On restart the log will be replayed
		// but the leader engine already has the write (or failed). Document:
		// if the leader engine write fails, we do not advance commitIndex and
		// the log has an uncommitted tail entry. Future opens will replay that
		// entry and attempt the leader write again (idempotent for the same key).
		return 0, fmt.Errorf("replica: leader engine put: %w", err)
	}

	// Advance leader's applied index and commit index.
	leader.appliedIndex = idx
	saveAppliedIndex(filepath.Join(s.opts.Dir, leader.info.Path), idx)
	s.commitIndex = idx
	return idx, nil
}

// Delete removes key from the leader, appends the operation, and advances the
// commit index. Deleting a missing key is a safe no-op. Returns the log index.
func (s *Store) Delete(key []byte) (LogIndex, error) {
	if len(key) == 0 {
		return 0, ErrInvalidKey
	}
	keyCopy := copyBytes(key)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}

	idx, err := s.log.append(Operation{Type: OpDelete, Key: keyCopy})
	if err != nil {
		return 0, fmt.Errorf("replica: log append: %w", err)
	}

	leader := s.replicas[s.leaderID]
	if err := leader.eng.Delete(keyCopy); err != nil {
		return 0, fmt.Errorf("replica: leader engine delete: %w", err)
	}

	leader.appliedIndex = idx
	saveAppliedIndex(filepath.Join(s.opts.Dir, leader.info.Path), idx)
	s.commitIndex = idx
	return idx, nil
}

// ── Read operations ───────────────────────────────────────────────────────────

// Get retrieves the value for key from the replica selected by mode/replicaID.
//
// ReadLeader: reads from the leader.
// ReadFollower: reads from the follower identified by replicaID; returns
//
//	ErrInvalidReplica if replicaID is the leader or out of range.
//
// ReadAny: reads from replicaID if >= 0, otherwise from the leader.
//
// Follower reads may be stale if ReplicateAll has not been called yet.
func (s *Store) Get(key []byte, mode ReadMode, replicaID int) ([]byte, bool, error) {
	if len(key) == 0 {
		return nil, false, ErrInvalidKey
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, false, ErrClosed
	}

	h, err := s.selectReplica(mode, replicaID)
	if err != nil {
		return nil, false, err
	}
	return h.eng.Get(key)
}

// Scan returns all live entries in [start, end) from the selected replica,
// merged and sorted by key ascending.
//
// The same mode/replicaID semantics as Get apply. Follower scans may be stale.
func (s *Store) Scan(start, end []byte, mode ReadMode, replicaID int) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}

	h, err := s.selectReplica(mode, replicaID)
	if err != nil {
		return nil, err
	}
	raw, err := h.eng.Scan(start, end)
	if err != nil {
		return nil, fmt.Errorf("replica: scan replica %d: %w", h.info.ID, err)
	}
	result := make([]Entry, len(raw))
	for i, e := range raw {
		result[i] = Entry{Key: e.Key, Value: e.Value, Seq: e.Seq}
	}
	return result, nil
}

// selectReplica returns the handle for the replica selected by mode/replicaID.
// Must be called under s.mu.RLock or s.mu.Lock.
func (s *Store) selectReplica(mode ReadMode, replicaID int) (*replicaHandle, error) {
	switch mode {
	case ReadLeader:
		return s.replicas[s.leaderID], nil

	case ReadFollower:
		if replicaID < 0 || replicaID >= len(s.replicas) {
			return nil, fmt.Errorf("%w: replicaID %d", ErrInvalidReplica, replicaID)
		}
		if replicaID == s.leaderID {
			return nil, fmt.Errorf("%w: replicaID %d is the leader; use ReadLeader or ReadAny",
				ErrInvalidReplica, replicaID)
		}
		return s.replicas[replicaID], nil

	case ReadAny:
		if replicaID < 0 {
			return s.replicas[s.leaderID], nil
		}
		if replicaID >= len(s.replicas) {
			return nil, fmt.Errorf("%w: replicaID %d", ErrInvalidReplica, replicaID)
		}
		return s.replicas[replicaID], nil

	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidReadMode, mode)
	}
}

// ── Replication ───────────────────────────────────────────────────────────────

// ReplicateOnce applies at most one pending committed operation to each
// non-paused follower. For followers with a lag limit, at most maxApplyPerCall
// operations are applied (default unlimited).
//
// Returns the total number of follower applications performed across all
// followers, or an error if any engine write fails.
//
// Holds s.mu.Lock for the duration so that concurrent Put/Delete operations
// cannot race with appliedIndex updates.
func (s *Store) ReplicateOnce() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	return s.replicateOnce()
}

// replicateOnce is the internal implementation. Must be called under s.mu.
func (s *Store) replicateOnce() (int, error) {
	total := 0
	for _, h := range s.replicas {
		if h.info.Role == RoleLeader {
			continue
		}
		if h.paused {
			continue
		}
		if h.appliedIndex >= s.commitIndex {
			continue
		}

		limit := 1
		if h.maxApplyPerCall > 0 {
			limit = h.maxApplyPerCall
		}

		ops := s.log.operationsAfter(h.appliedIndex, limit)
		for _, op := range ops {
			if op.Index > s.commitIndex {
				break
			}
			if err := applyOp(h, op); err != nil {
				return total, fmt.Errorf("replica: apply op %d to replica %d: %w",
					op.Index, h.info.ID, err)
			}
			h.appliedIndex = op.Index
			saveAppliedIndex(filepath.Join(s.opts.Dir, h.info.Path), op.Index)
			total++
		}
	}
	return total, nil
}

// ReplicateAll repeatedly applies pending committed operations to all
// non-paused followers until no progress remains.
//
// Holds s.mu.Lock for the entire call so that writes and replication are
// fully serialised.
func (s *Store) ReplicateAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	for {
		n, err := s.replicateOnce()
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
	}
}

// applyOp applies one operation to the follower's engine.
func applyOp(h *replicaHandle, op Operation) error {
	switch op.Type {
	case OpPut:
		return h.eng.Put(op.Key, op.Value)
	case OpDelete:
		return h.eng.Delete(op.Key)
	default:
		return fmt.Errorf("unknown op type %d", op.Type)
	}
}

// ── Pause / lag simulation ────────────────────────────────────────────────────

// SetFollowerPaused pauses or unpauses a follower replica. A paused follower
// will not apply any new operations during ReplicateOnce/ReplicateAll. These
// controls are in-memory only and are not persisted across restarts.
func (s *Store) SetFollowerPaused(replicaID int, paused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if replicaID < 0 || replicaID >= len(s.replicas) {
		return fmt.Errorf("%w: replicaID %d", ErrInvalidReplica, replicaID)
	}
	if replicaID == s.leaderID {
		return fmt.Errorf("%w: cannot pause the leader (replicaID %d)", ErrInvalidReplica, replicaID)
	}
	s.replicas[replicaID].paused = paused
	return nil
}

// SetFollowerLag limits the maximum number of operations a follower may apply
// per ReplicateOnce call. maxApplyPerCall <= 0 means unlimited.
func (s *Store) SetFollowerLag(replicaID int, maxApplyPerCall int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if replicaID < 0 || replicaID >= len(s.replicas) {
		return fmt.Errorf("%w: replicaID %d", ErrInvalidReplica, replicaID)
	}
	if replicaID == s.leaderID {
		return fmt.Errorf("%w: cannot set lag on the leader (replicaID %d)", ErrInvalidReplica, replicaID)
	}
	s.replicas[replicaID].maxApplyPerCall = maxApplyPerCall
	return nil
}

// ── Flush / Compact ───────────────────────────────────────────────────────────

// Flush flushes every replica engine's MemTable to disk.
func (s *Store) Flush() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ErrClosed
	}
	for _, h := range s.replicas {
		if err := h.eng.Flush(); err != nil {
			return fmt.Errorf("replica: flush replica %d (%s): %w", h.info.ID, h.info.Name, err)
		}
	}
	return nil
}

// Compact runs manual full compaction on every replica engine.
func (s *Store) Compact() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ErrClosed
	}
	for _, h := range s.replicas {
		if err := h.eng.Compact(); err != nil {
			return fmt.Errorf("replica: compact replica %d (%s): %w", h.info.ID, h.info.Name, err)
		}
	}
	return nil
}

// ── Stats / Replicas ──────────────────────────────────────────────────────────

// Stats returns aggregated statistics across all replicas.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st := Stats{
		ReplicaCount: len(s.replicas),
		LeaderID:     s.leaderID,
		CommitIndex:  s.commitIndex,
		Replicas:     make([]ReplicaStats, 0, len(s.replicas)),
	}

	minApplied := LogIndex(^uint64(0))
	for _, h := range s.replicas {
		es := h.eng.Stats()
		rs := ReplicaStats{
			ID:                    h.info.ID,
			Role:                  h.info.Role,
			Name:                  h.info.Name,
			Path:                  h.info.Path,
			AppliedIndex:          h.appliedIndex,
			EngineSSTableCount:    es.SSTableCount,
			EngineFlushCount:      es.FlushCount,
			EngineCompactionCount: es.CompactionCount,
		}
		st.Replicas = append(st.Replicas, rs)
		if h.appliedIndex < minApplied {
			minApplied = h.appliedIndex
		}
	}
	if minApplied == LogIndex(^uint64(0)) {
		minApplied = 0
	}
	st.AppliedIndex = minApplied

	// Sort replicas by ID for deterministic output.
	sort.Slice(st.Replicas, func(i, j int) bool {
		return st.Replicas[i].ID < st.Replicas[j].ID
	})
	return st
}

// Replicas returns a snapshot of all replica metadata.
func (s *Store) Replicas() []ReplicaInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ReplicaInfo, len(s.replicas))
	for i, h := range s.replicas {
		out[i] = h.info
	}
	return out
}

// ── Close ─────────────────────────────────────────────────────────────────────

// Close closes all replica engines and the replication log. Close is
// idempotent; calling it twice returns nil. After Close, all operations return
// ErrClosed.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	var firstErr error
	for _, h := range s.replicas {
		if err := h.eng.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("replica: close replica %d (%s): %w", h.info.ID, h.info.Name, err)
		}
	}
	if err := s.log.close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("replica: close log: %w", err)
	}
	return firstErr
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// closeHandles closes all engines and the log. Used during failed Open.
func closeHandles(handles []*replicaHandle, ol *opLog) {
	for _, h := range handles {
		h.eng.Close()
	}
	if ol != nil {
		ol.close()
	}
}

// loadAppliedIndex reads the applied index from <replicaDir>/APPLIED.
// Returns 0 if the file does not exist or cannot be parsed.
func loadAppliedIndex(replicaDir string) LogIndex {
	path := filepath.Join(replicaDir, appliedFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(data))
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return LogIndex(n)
}

// saveAppliedIndex writes the applied index to <replicaDir>/APPLIED atomically.
func saveAppliedIndex(replicaDir string, idx LogIndex) {
	path := filepath.Join(replicaDir, appliedFileName)
	tmp := path + ".tmp"
	data := []byte(strconv.FormatUint(uint64(idx), 10) + "\n")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return // best-effort
	}
	os.Rename(tmp, path) // best-effort
}
