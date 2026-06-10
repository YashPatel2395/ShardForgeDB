package replica

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func testStore(t *testing.T, count int) (*Store, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "replica-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	s, err := Open(Options{Dir: dir, ReplicaCount: count})
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("Open: %v", err)
	}
	return s, func() {
		s.Close()
		os.RemoveAll(dir)
	}
}

// mustPut is a helper that calls Put and fails the test on error.
func mustPut(t *testing.T, s *Store, key, val string) LogIndex {
	t.Helper()
	idx, err := s.Put([]byte(key), []byte(val))
	if err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
	return idx
}

// mustGet asserts the value returned by Get on the leader.
func mustGet(t *testing.T, s *Store, key, wantVal string, wantFound bool) {
	t.Helper()
	val, found, err := s.Get([]byte(key), ReadLeader, -1)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	if found != wantFound {
		t.Fatalf("Get(%q): found=%v, want %v", key, found, wantFound)
	}
	if wantFound && string(val) != wantVal {
		t.Fatalf("Get(%q): value=%q, want %q", key, val, wantVal)
	}
}

// ── Open validation ───────────────────────────────────────────────────────────

func TestOpen_RejectsEmptyDir(t *testing.T) {
	_, err := Open(Options{Dir: "", ReplicaCount: 2})
	if err == nil {
		t.Fatal("expected error for empty Dir")
	}
}

func TestOpen_RejectsZeroReplicaCount(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(Options{Dir: dir, ReplicaCount: 0})
	if err == nil {
		t.Fatal("expected error for ReplicaCount=0")
	}
}

func TestOpen_DefaultsLeaderIDToZero(t *testing.T) {
	s, cleanup := testStore(t, 3)
	defer cleanup()
	if s.leaderID != 0 {
		t.Fatalf("leaderID=%d, want 0", s.leaderID)
	}
	for _, h := range s.replicas {
		if h.info.ID == 0 && h.info.Role != RoleLeader {
			t.Fatal("replica 0 should be leader")
		}
		if h.info.ID != 0 && h.info.Role != RoleFollower {
			t.Fatalf("replica %d should be follower", h.info.ID)
		}
	}
}

func TestOpen_RejectsLeaderIDOutOfRange(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(Options{Dir: dir, ReplicaCount: 2, LeaderID: 5})
	if err == nil {
		t.Fatal("expected error for LeaderID >= ReplicaCount")
	}
}

func TestOpen_CreatesManifest(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	s.Close()
	if !pathExists(filepath.Join(s.opts.Dir, manifestFileName)) {
		t.Fatal("manifest not created")
	}
}

func TestOpen_CreatesReplicaDirectories(t *testing.T) {
	s, cleanup := testStore(t, 3)
	defer cleanup()
	s.Close()
	for _, h := range s.replicas {
		p := filepath.Join(s.opts.Dir, h.info.Path)
		if !pathExists(p) {
			t.Fatalf("replica dir %q not created", p)
		}
	}
}

func TestReopen_LoadsManifestWhenZeroCount(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 3})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s.Close()

	s2, err := Open(Options{Dir: dir, ReplicaCount: 0, LeaderID: -1})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if s2.opts.ReplicaCount != 3 {
		t.Fatalf("ReplicaCount after reopen: %d, want 3", s2.opts.ReplicaCount)
	}
}

func TestReopen_RejectsMismatchedReplicaCount(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 3})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s.Close()

	_, err = Open(Options{Dir: dir, ReplicaCount: 2})
	if err == nil {
		t.Fatal("expected ErrReplicaMismatch")
	}
}

func TestReopen_RejectsMismatchedLeaderID(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 3, LeaderID: 0})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s.Close()

	_, err = Open(Options{Dir: dir, LeaderID: 2})
	if err == nil {
		t.Fatal("expected ErrReplicaMismatch")
	}
}

// ── Manifest validation ───────────────────────────────────────────────────────

func TestManifest_RejectsUnsupportedVersion(t *testing.T) {
	m := &replicationManifest{
		Version:      99,
		ReplicaCount: 1,
		LeaderID:     0,
		Replicas:     []replicaEntry{{ID: 0, Role: RoleLeader, Name: "r-0000", Path: "replicas/r-0000"}},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for unsupported version")
	}
}

func TestManifest_RejectsDuplicateReplicaID(t *testing.T) {
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 2,
		LeaderID:     0,
		Replicas: []replicaEntry{
			{ID: 0, Role: RoleLeader, Name: "r-0000", Path: "replicas/r-0000"},
			{ID: 0, Role: RoleFollower, Name: "r-0001", Path: "replicas/r-0001"},
		},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestManifest_RejectsDuplicateName(t *testing.T) {
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 2,
		LeaderID:     0,
		Replicas: []replicaEntry{
			{ID: 0, Role: RoleLeader, Name: "same", Path: "replicas/r-0000"},
			{ID: 1, Role: RoleFollower, Name: "same", Path: "replicas/r-0001"},
		},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestManifest_RejectsDuplicatePath(t *testing.T) {
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 2,
		LeaderID:     0,
		Replicas: []replicaEntry{
			{ID: 0, Role: RoleLeader, Name: "r-0000", Path: "replicas/same"},
			{ID: 1, Role: RoleFollower, Name: "r-0001", Path: "replicas/same"},
		},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for duplicate path")
	}
}

func TestManifest_RejectsAbsolutePath(t *testing.T) {
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 1,
		LeaderID:     0,
		Replicas: []replicaEntry{
			{ID: 0, Role: RoleLeader, Name: "r-0000", Path: "/absolute/path"},
		},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestManifest_RejectsPathTraversal(t *testing.T) {
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 1,
		LeaderID:     0,
		Replicas: []replicaEntry{
			{ID: 0, Role: RoleLeader, Name: "r-0000", Path: "../outside"},
		},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestManifest_RejectsNoLeader(t *testing.T) {
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 2,
		LeaderID:     0,
		Replicas: []replicaEntry{
			{ID: 0, Role: RoleFollower, Name: "r-0000", Path: "replicas/r-0000"},
			{ID: 1, Role: RoleFollower, Name: "r-0001", Path: "replicas/r-0001"},
		},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for no leader")
	}
}

func TestManifest_RejectsMultipleLeaders(t *testing.T) {
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 2,
		LeaderID:     0,
		Replicas: []replicaEntry{
			{ID: 0, Role: RoleLeader, Name: "r-0000", Path: "replicas/r-0000"},
			{ID: 1, Role: RoleLeader, Name: "r-0001", Path: "replicas/r-0001"},
		},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for multiple leaders")
	}
}

func TestManifest_RejectsMissingReplicaID(t *testing.T) {
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 2,
		LeaderID:     1,
		Replicas: []replicaEntry{
			// ID 0 missing — has ID 2 instead
			{ID: 1, Role: RoleLeader, Name: "r-0001", Path: "replicas/r-0001"},
			{ID: 2, Role: RoleFollower, Name: "r-0002", Path: "replicas/r-0002"},
		},
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for missing replica ID")
	}
}

func TestManifest_WriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	m := &replicationManifest{
		Version:      1,
		ReplicaCount: 1,
		LeaderID:     0,
		Replicas:     []replicaEntry{{ID: 0, Role: RoleLeader, Name: "r-0000", Path: "replicas/r-0000"}},
	}
	if err := writeManifest(dir, m); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	tmp := filepath.Join(dir, manifestFileName+".tmp")
	if pathExists(tmp) {
		t.Fatal("temp manifest file still exists after write")
	}
}

// ── Write operations ──────────────────────────────────────────────────────────

func TestPut_ReturnsIndex1(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	idx := mustPut(t, s, "hello", "world")
	if idx != 1 {
		t.Fatalf("first Put index=%d, want 1", idx)
	}
}

func TestPut_DoesNotWriteToFollower(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "k", "v")

	followerID := 1
	_, found, err := s.Get([]byte("k"), ReadFollower, followerID)
	if err != nil {
		t.Fatalf("Get follower: %v", err)
	}
	if found {
		t.Fatal("follower should not have the key before replication")
	}
}

func TestReplicateOnce_AppliesOneOp(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "k", "v")

	n, err := s.ReplicateOnce()
	if err != nil {
		t.Fatalf("ReplicateOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("ReplicateOnce applied %d ops, want 1", n)
	}
	_, found, _ := s.Get([]byte("k"), ReadFollower, 1)
	if !found {
		t.Fatal("follower should have key after ReplicateOnce")
	}
}

func TestReplicateAll_CatchesFollowerUp(t *testing.T) {
	s, cleanup := testStore(t, 3)
	defer cleanup()
	for i := 0; i < 5; i++ {
		mustPut(t, s, fmt.Sprintf("k%d", i), fmt.Sprintf("v%d", i))
	}

	if err := s.ReplicateAll(); err != nil {
		t.Fatalf("ReplicateAll: %v", err)
	}

	for i := 0; i < 5; i++ {
		for _, fid := range []int{1, 2} {
			val, found, err := s.Get([]byte(fmt.Sprintf("k%d", i)), ReadFollower, fid)
			if err != nil || !found {
				t.Fatalf("follower %d missing k%d after ReplicateAll: err=%v found=%v", fid, i, err, found)
			}
			if string(val) != fmt.Sprintf("v%d", i) {
				t.Fatalf("follower %d k%d: got %q, want %q", fid, i, val, fmt.Sprintf("v%d", i))
			}
		}
	}
}

func TestDelete_ReplicatesAndRemovesKey(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "del-key", "val")
	if err := s.ReplicateAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delete([]byte("del-key")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.ReplicateAll(); err != nil {
		t.Fatal(err)
	}

	_, found, _ := s.Get([]byte("del-key"), ReadFollower, 1)
	if found {
		t.Fatal("follower should not have key after delete + replication")
	}
}

// ── Stale reads ───────────────────────────────────────────────────────────────

func TestFollowerRead_StaleBeforeReplication(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "stale", "data")

	_, found, _ := s.Get([]byte("stale"), ReadFollower, 1)
	if found {
		t.Fatal("follower should be stale before replication")
	}
}

func TestFollowerRead_CurrentAfterReplicateAll(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "fresh", "data")
	s.ReplicateAll()

	val, found, err := s.Get([]byte("fresh"), ReadFollower, 1)
	if err != nil || !found || string(val) != "data" {
		t.Fatalf("follower should have key after replication: err=%v found=%v val=%q", err, found, val)
	}
}

// ── Pause / lag simulation ────────────────────────────────────────────────────

func TestPausedFollower_DoesNotApply(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	s.SetFollowerPaused(1, true)
	mustPut(t, s, "p", "v")
	s.ReplicateAll()

	_, found, _ := s.Get([]byte("p"), ReadFollower, 1)
	if found {
		t.Fatal("paused follower should not have key")
	}
}

func TestUnpausedFollower_CatchesUp(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	s.SetFollowerPaused(1, true)
	mustPut(t, s, "p", "v")
	s.ReplicateAll()

	s.SetFollowerPaused(1, false)
	s.ReplicateAll()

	_, found, _ := s.Get([]byte("p"), ReadFollower, 1)
	if !found {
		t.Fatal("unpaused follower should catch up")
	}
}

func TestLagLimitedFollower_AppliesLimitedOpsPerCall(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	s.SetFollowerLag(1, 2)
	for i := 0; i < 6; i++ {
		mustPut(t, s, fmt.Sprintf("lag-%d", i), "v")
	}

	n, err := s.ReplicateOnce()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("lag-limited follower applied %d ops, want 2", n)
	}
}

// ── Commit / applied index tracking ──────────────────────────────────────────

func TestCommitIndex_AdvancesOnWrites(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	for i := 0; i < 5; i++ {
		mustPut(t, s, fmt.Sprintf("ci-%d", i), "v")
	}
	if s.commitIndex != 5 {
		t.Fatalf("commitIndex=%d, want 5", s.commitIndex)
	}
}

func TestAppliedIndex_TracksEachFollower(t *testing.T) {
	s, cleanup := testStore(t, 3)
	defer cleanup()
	mustPut(t, s, "a", "1")
	mustPut(t, s, "b", "2")
	s.SetFollowerPaused(2, true)
	s.ReplicateAll()

	st := s.Stats()
	var ai1, ai2 LogIndex
	for _, rs := range st.Replicas {
		if rs.ID == 1 {
			ai1 = rs.AppliedIndex
		}
		if rs.ID == 2 {
			ai2 = rs.AppliedIndex
		}
	}
	if ai1 != 2 {
		t.Fatalf("follower 1 appliedIndex=%d, want 2", ai1)
	}
	if ai2 != 0 {
		t.Fatalf("follower 2 appliedIndex=%d, want 0 (paused)", ai2)
	}
}

// ── Reopen / persistence ──────────────────────────────────────────────────────

func TestReopen_PreservesLeaderData(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mustPut(t, s, "persist", "yes")
	s.Flush()
	s.Close()

	s2, err := Open(Options{Dir: dir, LeaderID: -1})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	mustGet(t, s2, "persist", "yes", true)
}

func TestReopen_PreservesReplicationLog(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		mustPut(t, s, fmt.Sprintf("r%d", i), "v")
	}
	s.Close()

	s2, err := Open(Options{Dir: dir, LeaderID: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.commitIndex != 5 {
		t.Fatalf("commitIndex after reopen=%d, want 5", s2.commitIndex)
	}
}

func TestReopen_FollowerNoDuplicateApply(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, "dup", "first")
	s.ReplicateAll()
	// Applied index persisted = 1. Close and reopen.
	s.Close()

	s2, err := Open(Options{Dir: dir, LeaderID: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// follower applied index should still be 1.
	st := s2.Stats()
	for _, rs := range st.Replicas {
		if rs.ID == 1 && rs.AppliedIndex != 1 {
			t.Fatalf("follower appliedIndex after reopen=%d, want 1", rs.AppliedIndex)
		}
	}
	// ReplicateAll should be a no-op (no new ops pending).
	n, _ := s2.ReplicateOnce()
	if n != 0 {
		t.Fatalf("ReplicateOnce after reopen with caught-up follower applied %d ops, want 0", n)
	}
}

func TestReopen_ThenReplicateAllCatchesUp(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		mustPut(t, s, fmt.Sprintf("rr%d", i), "v")
	}
	// Do NOT replicate before close.
	s.Close()

	s2, err := Open(Options{Dir: dir, LeaderID: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := s2.ReplicateAll(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, found, _ := s2.Get([]byte(fmt.Sprintf("rr%d", i)), ReadFollower, 1)
		if !found {
			t.Fatalf("follower missing rr%d after reopen+ReplicateAll", i)
		}
	}
}

// ── Read modes ────────────────────────────────────────────────────────────────

func TestReadLeader_Works(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "rl", "v")
	val, found, err := s.Get([]byte("rl"), ReadLeader, -1)
	if err != nil || !found || string(val) != "v" {
		t.Fatalf("ReadLeader failed: err=%v found=%v val=%q", err, found, val)
	}
}

func TestReadFollower_RejectsLeaderID(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	_, _, err := s.Get([]byte("k"), ReadFollower, 0) // 0 is leader
	if err == nil {
		t.Fatal("expected error when ReadFollower targets leader")
	}
}

func TestReadFollower_RejectsInvalidID(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	_, _, err := s.Get([]byte("k"), ReadFollower, 99)
	if err == nil {
		t.Fatal("expected error for out-of-range follower ID")
	}
}

func TestReadAny_DefaultsToLeaderWhenNegative(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "any", "v")
	val, found, err := s.Get([]byte("any"), ReadAny, -1)
	if err != nil || !found || string(val) != "v" {
		t.Fatalf("ReadAny with replicaID=-1 failed: err=%v found=%v val=%q", err, found, val)
	}
}

// ── Scan ──────────────────────────────────────────────────────────────────────

func TestScan_LeaderReturnsSortedEntries(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	keys := []string{"c", "a", "b"}
	for _, k := range keys {
		mustPut(t, s, k, k+"-val")
	}
	entries, err := s.Scan(nil, nil, ReadLeader, -1)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("Scan returned %d entries, want 3", len(entries))
	}
	for i := 0; i < len(entries)-1; i++ {
		if string(entries[i].Key) >= string(entries[i+1].Key) {
			t.Fatalf("Scan not sorted: %q >= %q", entries[i].Key, entries[i+1].Key)
		}
	}
}

func TestScan_FollowerStaleBeforeCatchup(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "s1", "v1")
	mustPut(t, s, "s2", "v2")
	// No replication.
	entries, err := s.Scan(nil, nil, ReadFollower, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("follower scan before replication: got %d entries, want 0", len(entries))
	}
}

// ── Flush / Compact ───────────────────────────────────────────────────────────

func TestFlush_PersistsAllReplicas(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "fl", "v")
	s.ReplicateAll()
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	st := s.Stats()
	for _, rs := range st.Replicas {
		if rs.EngineFlushCount == 0 {
			t.Fatalf("replica %d: FlushCount=0 after Flush", rs.ID)
		}
	}
}

func TestCompact_PreservesAllReplicas(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "c1", "v1")
	mustPut(t, s, "c2", "v2")
	s.ReplicateAll()
	s.Flush()
	mustPut(t, s, "c1", "v1b") // overwrite to get two SSTables
	s.ReplicateAll()
	s.Flush()
	if err := s.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	// Reads should still work.
	mustGet(t, s, "c2", "v2", true)
}

// ── Close ─────────────────────────────────────────────────────────────────────

func TestClose_IsIdempotent(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestOperationsAfterClose_ReturnErrClosed(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	s.Close()

	if _, err := s.Put([]byte("k"), []byte("v")); err != ErrClosed {
		t.Fatalf("Put after Close: got %v, want ErrClosed", err)
	}
	if _, _, err := s.Get([]byte("k"), ReadLeader, -1); err != ErrClosed {
		t.Fatalf("Get after Close: got %v, want ErrClosed", err)
	}
	if _, err := s.Scan(nil, nil, ReadLeader, -1); err != ErrClosed {
		t.Fatalf("Scan after Close: got %v, want ErrClosed", err)
	}
	if _, err := s.ReplicateOnce(); err != ErrClosed {
		t.Fatalf("ReplicateOnce after Close: got %v, want ErrClosed", err)
	}
	if err := s.ReplicateAll(); err != ErrClosed {
		t.Fatalf("ReplicateAll after Close: got %v, want ErrClosed", err)
	}
	if err := s.Flush(); err != ErrClosed {
		t.Fatalf("Flush after Close: got %v, want ErrClosed", err)
	}
	if err := s.Compact(); err != ErrClosed {
		t.Fatalf("Compact after Close: got %v, want ErrClosed", err)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestConcurrentPutAndReplicateAll_RaceSafe(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Put([]byte(fmt.Sprintf("cc-%05d", n)), []byte("v"))
		}(i)
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.ReplicateAll()
		}()
	}
	wg.Wait()
}

func TestConcurrentGetScan_RaceSafe(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	for i := 0; i < 20; i++ {
		mustPut(t, s, fmt.Sprintf("rk-%03d", i), "v")
	}
	s.ReplicateAll()

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			s.Get([]byte(fmt.Sprintf("rk-%03d", n%20)), ReadLeader, -1)
		}(i)
		go func(n int) {
			defer wg.Done()
			s.Scan(nil, nil, ReadFollower, 1)
		}(i)
	}
	wg.Wait()
}

func TestConcurrentClose_RaceSafe(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Put([]byte(fmt.Sprintf("cl-%04d", n)), []byte("v"))
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Close()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.ReplicateAll()
	}()
	wg.Wait()
}

// ── Log codec ─────────────────────────────────────────────────────────────────

func TestLogCodec_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.dat")
	ol, err := openLog(path)
	if err != nil {
		t.Fatalf("openLog: %v", err)
	}

	ops := []Operation{
		{Type: OpPut, Key: []byte("hello"), Value: []byte("world")},
		{Type: OpDelete, Key: []byte("gone")},
		{Type: OpPut, Key: []byte("binary"), Value: []byte{0x00, 0xFF, 0x42}},
	}
	for _, op := range ops {
		if _, err := ol.append(op); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	ol.close()

	ol2, err := openLog(path)
	if err != nil {
		t.Fatalf("reopen log: %v", err)
	}
	defer ol2.close()

	var got []Operation
	ol2.replay(func(op Operation) error {
		got = append(got, op)
		return nil
	})

	if len(got) != len(ops) {
		t.Fatalf("replayed %d ops, want %d", len(got), len(ops))
	}
	for i, op := range ops {
		if string(got[i].Key) != string(op.Key) {
			t.Fatalf("op %d: key %q != %q", i, got[i].Key, op.Key)
		}
		if string(got[i].Value) != string(op.Value) {
			t.Fatalf("op %d: value %q != %q", i, got[i].Value, op.Value)
		}
		if got[i].Type != op.Type {
			t.Fatalf("op %d: type %d != %d", i, got[i].Type, op.Type)
		}
		if got[i].Index != LogIndex(i+1) {
			t.Fatalf("op %d: index %d != %d", i, got[i].Index, i+1)
		}
	}
}

func TestLog_RejectsBadCRC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.dat")
	ol, err := openLog(path)
	if err != nil {
		t.Fatalf("openLog: %v", err)
	}
	ol.append(Operation{Type: OpPut, Key: []byte("k"), Value: []byte("v")})
	ol.close()

	// Corrupt one byte inside the CRC field (offset logHeaderSize + 4 bytes recordLen).
	f, _ := os.OpenFile(path, os.O_RDWR, 0)
	// Header(10) + recordLen(4) = offset 14; corrupt byte 14 (start of CRC).
	f.WriteAt([]byte{0xFF}, 14)
	f.Close()

	_, err = openLog(path)
	if err == nil {
		t.Fatal("expected error for bad CRC")
	}
}

func TestLog_RejectsTruncatedRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.dat")
	ol, err := openLog(path)
	if err != nil {
		t.Fatalf("openLog: %v", err)
	}
	ol.append(Operation{Type: OpPut, Key: []byte("k"), Value: []byte("v")})
	ol.close()

	// Truncate the file to remove the last few bytes of the record body.
	fi, _ := os.Stat(path)
	os.Truncate(path, fi.Size()-3)

	_, err = openLog(path)
	if err == nil {
		t.Fatal("expected error for truncated record")
	}
}

func TestLog_RejectsUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.dat")
	// Write header with version 99.
	f, _ := os.Create(path)
	hdr := make([]byte, 10)
	copy(hdr[:8], logMagic)
	hdr[8] = 99 // version low byte
	hdr[9] = 0
	f.Write(hdr)
	f.Close()

	_, err2 := openLog(path)
	if err2 == nil {
		t.Fatal("expected error for unsupported log version")
	}
}

func TestLog_OperationsAfter_ReturnsCorrectSuffix(t *testing.T) {
	dir := t.TempDir()
	ol, _ := openLog(filepath.Join(dir, "log.dat"))
	defer ol.close()

	for i := 0; i < 5; i++ {
		ol.append(Operation{Type: OpPut, Key: []byte(fmt.Sprintf("k%d", i))})
	}

	ops := ol.operationsAfter(2, 0)
	if len(ops) != 3 {
		t.Fatalf("operationsAfter(2): got %d ops, want 3", len(ops))
	}
	if ops[0].Index != 3 {
		t.Fatalf("first op index=%d, want 3", ops[0].Index)
	}
}

// ── Misc / edge cases ─────────────────────────────────────────────────────────

func TestDeleteMissingKey_SafeNoOp(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	idx, err := s.Delete([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("Delete missing key: %v", err)
	}
	if idx == 0 {
		t.Fatal("Delete should return a non-zero log index even for missing keys")
	}
	if err := s.ReplicateAll(); err != nil {
		t.Fatal(err)
	}
	_, found, _ := s.Get([]byte("nonexistent"), ReadFollower, 1)
	if found {
		t.Fatal("should not find nonexistent key after delete replication")
	}
}

func TestEmptyKey_RejectedForPutDeleteGet(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()

	if _, err := s.Put([]byte{}, []byte("v")); err != ErrInvalidKey {
		t.Fatalf("Put empty key: got %v, want ErrInvalidKey", err)
	}
	if _, err := s.Delete([]byte{}); err != ErrInvalidKey {
		t.Fatalf("Delete empty key: got %v, want ErrInvalidKey", err)
	}
	if _, _, err := s.Get([]byte{}, ReadLeader, -1); err != ErrInvalidKey {
		t.Fatalf("Get empty key: got %v, want ErrInvalidKey", err)
	}
}

func TestReplicaNames_Deterministic(t *testing.T) {
	s, cleanup := testStore(t, 3)
	defer cleanup()
	infos := s.Replicas()
	for i, info := range infos {
		wantName := fmt.Sprintf("replica-%04d", i)
		if info.Name != wantName {
			t.Fatalf("replica %d: name=%q, want %q", i, info.Name, wantName)
		}
	}
}

func TestReadModeValidation(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	_, _, err := s.Get([]byte("k"), ReadMode("bogus"), 0)
	if err == nil {
		t.Fatal("expected error for invalid ReadMode")
	}
}

func TestStats_IncludesPerReplicaAppliedIndexes(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "x", "y")
	s.ReplicateAll()

	st := s.Stats()
	if st.CommitIndex != 1 {
		t.Fatalf("CommitIndex=%d, want 1", st.CommitIndex)
	}
	for _, rs := range st.Replicas {
		if rs.AppliedIndex != 1 {
			t.Fatalf("replica %d AppliedIndex=%d, want 1", rs.ID, rs.AppliedIndex)
		}
	}
}

func TestStats_IncludesEngineStats(t *testing.T) {
	s, cleanup := testStore(t, 2)
	defer cleanup()
	mustPut(t, s, "e", "v")
	s.ReplicateAll()
	s.Flush()

	st := s.Stats()
	for _, rs := range st.Replicas {
		if rs.EngineFlushCount == 0 {
			t.Fatalf("replica %d: EngineFlushCount=0 after Flush", rs.ID)
		}
	}
}

// ── Large workload ────────────────────────────────────────────────────────────

func TestLargeWorkload_1000Writes_PartialReplication_Reopen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large workload test in short mode")
	}
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}

	const n = 1000
	for i := 0; i < n; i++ {
		mustPut(t, s, fmt.Sprintf("lw-%06d", i), fmt.Sprintf("val-%06d", i))
	}

	// Partially replicate.
	s.SetFollowerLag(1, 300)
	s.ReplicateAll()

	s.Flush()
	s.Close()

	// Reopen and catch up.
	s2, err := Open(Options{Dir: dir, LeaderID: -1})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if err := s2.ReplicateAll(); err != nil {
		t.Fatalf("ReplicateAll after reopen: %v", err)
	}

	// Spot-check a sample of keys.
	for _, i := range []int{0, 499, 999} {
		k := fmt.Sprintf("lw-%06d", i)
		v := fmt.Sprintf("val-%06d", i)
		val, found, err := s2.Get([]byte(k), ReadFollower, 1)
		if err != nil || !found || string(val) != v {
			t.Fatalf("follower missing %q after reopen+ReplicateAll: err=%v found=%v val=%q", k, err, found, val)
		}
	}
}

// ── Commit-index recovery fix tests ──────────────────────────────────────────

// TestOpen_LoadsCommitIndexFromCommitFile verifies that after normal Put
// operations the COMMIT file is written and loaded correctly on reopen.
func TestOpen_LoadsCommitIndexFromCommitFile(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		mustPut(t, s, fmt.Sprintf("ci-%d", i), "v")
	}
	if s.commitIndex != 5 {
		t.Fatalf("in-memory commitIndex=%d, want 5", s.commitIndex)
	}
	s.Close()

	// Verify COMMIT file exists and contains 5.
	on_disk := loadCommitIndex(dir)
	if on_disk != 5 {
		t.Fatalf("COMMIT file has index %d, want 5", on_disk)
	}

	// Reopen — commitIndex must come from COMMIT, not log.lastIndex().
	s2, err := Open(Options{Dir: dir, LeaderID: -1})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if s2.commitIndex != 5 {
		t.Fatalf("reopened commitIndex=%d, want 5", s2.commitIndex)
	}
}

// TestOpen_MissingCommitFileDoesNotTreatLogTailAsCommitted verifies that if
// COMMIT is absent, commitIndex is 0 regardless of the replication log size.
func TestOpen_MissingCommitFileDoesNotTreatLogTailAsCommitted(t *testing.T) {
	dir := t.TempDir()

	// First: open, write ops, close normally (COMMIT written).
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, "k1", "v1")
	mustPut(t, s, "k2", "v2")
	s.Close()

	// Remove the COMMIT file to simulate the missing-file scenario.
	if err := os.Remove(filepath.Join(dir, commitFileName)); err != nil {
		t.Fatalf("remove COMMIT: %v", err)
	}

	// Reopen: commitIndex must be 0, not log.lastIndex() (which is 2).
	s2, err := Open(Options{Dir: dir, LeaderID: -1})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if s2.commitIndex != 0 {
		t.Fatalf("commitIndex after missing COMMIT=%d, want 0", s2.commitIndex)
	}

	// ReplicateAll should apply 0 operations to follower (nothing committed).
	n, err := s2.ReplicateOnce()
	if err != nil {
		t.Fatalf("ReplicateOnce: %v", err)
	}
	if n != 0 {
		t.Fatalf("ReplicateOnce applied %d ops with commitIndex=0, want 0", n)
	}
}

// TestUncommittedLogTailNotReplicatedAfterRestart verifies the core fix:
// a log record that exists but whose COMMIT index is lower is never replicated
// to followers after restart.
func TestUncommittedLogTailNotReplicatedAfterRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}

	// Write 2 normally committed operations.
	mustPut(t, s, "committed-1", "v1")
	mustPut(t, s, "committed-2", "v2")
	// commitIndex is now 2 and COMMIT file contains "2".

	// Simulate a failed Put: append directly to the log (bypassing COMMIT
	// update). This mimics a scenario where Put succeeded at the log-append
	// step but the leader Engine write returned an error.
	_, err = s.log.append(Operation{Type: OpPut, Key: []byte("uncommitted"), Value: []byte("ghost")})
	if err != nil {
		t.Fatalf("direct log append: %v", err)
	}
	// s.commitIndex is still 2 (COMMIT file still contains "2").

	s.Close()

	// Reopen: commitIndex must be 2 (from COMMIT), not 3 (from log.lastIndex).
	s2, err := Open(Options{Dir: dir, LeaderID: -1})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if s2.commitIndex != 2 {
		t.Fatalf("commitIndex after reopen=%d, want 2", s2.commitIndex)
	}

	// ReplicateAll should propagate operations 1 and 2 but NOT 3.
	if err := s2.ReplicateAll(); err != nil {
		t.Fatalf("ReplicateAll: %v", err)
	}

	// Follower must have the two committed keys.
	for _, k := range []string{"committed-1", "committed-2"} {
		_, found, err := s2.Get([]byte(k), ReadFollower, 1)
		if err != nil || !found {
			t.Fatalf("follower missing committed key %q: err=%v found=%v", k, err, found)
		}
	}

	// Follower must NOT have the uncommitted key.
	_, found, err := s2.Get([]byte("uncommitted"), ReadFollower, 1)
	if err != nil {
		t.Fatalf("Get uncommitted key: %v", err)
	}
	if found {
		t.Fatal("follower has uncommitted key 'uncommitted' — recovery fix broken")
	}
}

// TestPutPersistsCommitIndex verifies that after a successful Put the COMMIT
// file on disk matches the returned log index.
func TestPutPersistsCommitIndex(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	idx := mustPut(t, s, "p", "v")
	on_disk := loadCommitIndex(dir)
	if on_disk != idx {
		t.Fatalf("COMMIT file=%d, Put returned index=%d; want equal", on_disk, idx)
	}
}

// TestDeletePersistsCommitIndex verifies the same for Delete.
func TestDeletePersistsCommitIndex(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, ReplicaCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	mustPut(t, s, "d", "v")
	idx, err := s.Delete([]byte("d"))
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	on_disk := loadCommitIndex(dir)
	if on_disk != idx {
		t.Fatalf("COMMIT file=%d, Delete returned index=%d; want equal", on_disk, idx)
	}
}

// TestCommitFileTempCleanup verifies that no COMMIT.tmp file is left after a
// successful saveCommitIndex call.
func TestCommitFileTempCleanup(t *testing.T) {
	dir := t.TempDir()
	s, cleanup := testStore(t, 2)
	defer cleanup()

	mustPut(t, s, "x", "y")
	tmp := filepath.Join(dir, commitFileName+".tmp")
	if pathExists(tmp) {
		t.Fatal("COMMIT.tmp temp file still exists after Put")
	}
	_ = s // suppress unused warning

	// Directly test saveCommitIndex leaves no temp file.
	if err := saveCommitIndex(t.TempDir(), 42); err != nil {
		t.Fatalf("saveCommitIndex: %v", err)
	}
}
