package replnet

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testID and testPrimaryURL are stable identity values used across test cases.
const testFollowerID = "follower-test-1"
const testPrimaryURL = "http://primary-test:8080"

func TestReplicationStateStore_FreshStart(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 0 {
		t.Errorf("fresh start: want 0, got %d", got)
	}
}

func TestReplicationStateStore_AdvanceAndLoad(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := ss.AdvanceTo(5); err != nil {
		t.Fatalf("advance to 5: %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 5 {
		t.Errorf("after advance: want 5, got %d", got)
	}

	// Reload from disk with same identity.
	ss2, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := ss2.LastAppliedSeq(); got != 5 {
		t.Errorf("after reload: want 5, got %d", got)
	}
}

func TestReplicationStateStore_SurvivesRestarts(t *testing.T) {
	dir := t.TempDir()

	for _, seq := range []uint64{1, 10, 100, 999} {
		ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
		if err != nil {
			t.Fatalf("new at seq %d: %v", seq, err)
		}
		if err := ss.AdvanceTo(seq); err != nil {
			t.Fatalf("advance to %d: %v", seq, err)
		}
	}

	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("final load: %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 999 {
		t.Errorf("after multiple restarts: want 999, got %d", got)
	}
}

func TestReplicationStateStore_IdempotentAdvance(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := ss.AdvanceTo(10); err != nil {
		t.Fatalf("advance to 10: %v", err)
	}

	// Advancing to the same value is a no-op (idempotent).
	if err := ss.AdvanceTo(10); err != nil {
		t.Fatalf("advance to 10 (same): %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 10 {
		t.Errorf("after same advance: want 10, got %d", got)
	}
}

func TestReplicationStateStore_Regression_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := ss.AdvanceTo(10); err != nil {
		t.Fatalf("advance to 10: %v", err)
	}

	// Advancing to a strictly lower value must return ErrInvalidSeqRegression.
	if err := ss.AdvanceTo(5); !errors.Is(err, ErrInvalidSeqRegression) {
		t.Errorf("advance to 5 (regression): want ErrInvalidSeqRegression, got %v", err)
	}
	// In-memory cursor must not change.
	if got := ss.LastAppliedSeq(); got != 10 {
		t.Errorf("after regression error: want 10, got %d", got)
	}
}

func TestReplicationStateStore_CorruptChecksum(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ss.AdvanceTo(42)

	// Corrupt the state file by flipping a byte in the JSON body.
	path := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	// Flip a byte far from the end to avoid hitting the checksum field directly.
	data[10] ^= 0xFF
	os.WriteFile(path, data, 0o644)

	_, err = NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if !errors.Is(err, ErrCorruptedState) {
		t.Errorf("corrupt checksum: want ErrCorruptedState, got %v", err)
	}
}

func TestReplicationStateStore_MissingFileIsOK(t *testing.T) {
	dir := t.TempDir()
	// Don't write any state file.
	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("missing file: want nil err, got %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 0 {
		t.Errorf("missing file: want seq 0, got %d", got)
	}
}

func TestReplicationStateStore_AtomicWrite_TempFileCleanedUp(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ss.AdvanceTo(7)

	// After a successful write, the tmp file should not exist.
	tmpPath := filepath.Join(dir, stateFileTmp)
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("tmp file should be gone after rename, got: %v", err)
	}

	// The final state file must exist.
	finalPath := filepath.Join(dir, stateFileName)
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("state file missing: %v", err)
	}
}

func TestReplicationStateStore_FollowerIDMismatch(t *testing.T) {
	dir := t.TempDir()

	// Create state file bound to "node-A".
	ss, err := NewReplicationStateStore(dir, "node-A", testPrimaryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ss.AdvanceTo(3)

	// Try to open the same dir as "node-B".
	_, err = NewReplicationStateStore(dir, "node-B", testPrimaryURL)
	if !errors.Is(err, ErrFollowerIdentityMismatch) {
		t.Errorf("wrong node ID: want ErrFollowerIdentityMismatch, got %v", err)
	}
}

func TestReplicationStateStore_PrimaryURLMismatch(t *testing.T) {
	dir := t.TempDir()

	// Create state file bound to primary-A.
	ss, err := NewReplicationStateStore(dir, testFollowerID, "http://primary-A:8080")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ss.AdvanceTo(3)

	// Try to open the same dir pointing at primary-B.
	_, err = NewReplicationStateStore(dir, testFollowerID, "http://primary-B:8080")
	if !errors.Is(err, ErrPrimaryIdentityMismatch) {
		t.Errorf("wrong primary URL: want ErrPrimaryIdentityMismatch, got %v", err)
	}
}

func TestReplicationStateStore_ChecksumCoversAllFields(t *testing.T) {
	// Two identical sequences but different follower IDs must produce different checksums.
	c1 := checksumState(stateVersion, "node-A", testPrimaryURL, 42, "2026-01-01T00:00:00Z")
	c2 := checksumState(stateVersion, "node-B", testPrimaryURL, 42, "2026-01-01T00:00:00Z")
	if c1 == c2 {
		t.Error("checksum: different follower IDs must produce different checksums")
	}

	// Two identical sequences but different primary URLs must produce different checksums.
	c3 := checksumState(stateVersion, testFollowerID, "http://primary-A", 42, "2026-01-01T00:00:00Z")
	c4 := checksumState(stateVersion, testFollowerID, "http://primary-B", 42, "2026-01-01T00:00:00Z")
	if c3 == c4 {
		t.Error("checksum: different primary URLs must produce different checksums")
	}

	// Same inputs → same checksum (deterministic).
	c5 := checksumState(stateVersion, testFollowerID, testPrimaryURL, 99, "2026-06-01T00:00:00Z")
	c6 := checksumState(stateVersion, testFollowerID, testPrimaryURL, 99, "2026-06-01T00:00:00Z")
	if c5 != c6 {
		t.Error("checksum: same inputs must be deterministic")
	}
}

func TestReplicationStateStore_PersistFailure_ErrorNotSwallowed(t *testing.T) {
	// Verify that a cursor persistence failure returns an error to the caller
	// and does not advance the in-memory cursor.
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir, testFollowerID, testPrimaryURL)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := ss.AdvanceTo(5); err != nil {
		t.Fatalf("advance to 5: %v", err)
	}

	// Make the directory non-writable so the next AdvanceTo cannot create the temp file.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("cannot chmod dir (platform may not support it): %v", err)
	}
	defer os.Chmod(dir, 0o755) // restore so t.TempDir() cleanup works

	err = ss.AdvanceTo(6)
	if err == nil {
		t.Error("AdvanceTo with non-writable dir: want error, got nil")
	}

	// In-memory cursor must not have advanced.
	if got := ss.LastAppliedSeq(); got != 5 {
		t.Errorf("cursor after persist failure: want 5, got %d", got)
	}
}
