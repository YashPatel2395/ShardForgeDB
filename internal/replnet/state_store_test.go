package replnet

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplicationStateStore_FreshStart(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 0 {
		t.Errorf("fresh start: want 0, got %d", got)
	}
}

func TestReplicationStateStore_AdvanceAndLoad(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := ss.AdvanceTo(5); err != nil {
		t.Fatalf("advance to 5: %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 5 {
		t.Errorf("after advance: want 5, got %d", got)
	}

	// Reload from disk.
	ss2, err := NewReplicationStateStore(dir)
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
		ss, err := NewReplicationStateStore(dir)
		if err != nil {
			t.Fatalf("new at seq %d: %v", seq, err)
		}
		if err := ss.AdvanceTo(seq); err != nil {
			t.Fatalf("advance to %d: %v", seq, err)
		}
	}

	ss, err := NewReplicationStateStore(dir)
	if err != nil {
		t.Fatalf("final load: %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 999 {
		t.Errorf("after multiple restarts: want 999, got %d", got)
	}
}

func TestReplicationStateStore_IdempotentAdvance(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ss.AdvanceTo(10)
	// Advancing to a smaller or equal value is a no-op.
	if err := ss.AdvanceTo(5); err != nil {
		t.Fatalf("advance to 5 (regression): %v", err)
	}
	if err := ss.AdvanceTo(10); err != nil {
		t.Fatalf("advance to 10 (same): %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 10 {
		t.Errorf("after regression: want 10, got %d", got)
	}
}

func TestReplicationStateStore_CorruptChecksum(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ss.AdvanceTo(42)

	// Corrupt the state file.
	path := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	// Flip a byte in the JSON to break the checksum.
	data[len(data)-2] ^= 0xFF
	os.WriteFile(path, data, 0o644)

	_, err = NewReplicationStateStore(dir)
	if !errors.Is(err, ErrCorruptedState) {
		t.Errorf("corrupt checksum: want ErrCorruptedState, got %v", err)
	}
}

func TestReplicationStateStore_MissingFileIsOK(t *testing.T) {
	dir := t.TempDir()
	// Don't write any state file.
	ss, err := NewReplicationStateStore(dir)
	if err != nil {
		t.Fatalf("missing file: want nil err, got %v", err)
	}
	if got := ss.LastAppliedSeq(); got != 0 {
		t.Errorf("missing file: want seq 0, got %d", got)
	}
}

func TestReplicationStateStore_AtomicWrite_TempFileCleanedUp(t *testing.T) {
	dir := t.TempDir()
	ss, err := NewReplicationStateStore(dir)
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

func TestReplicationStateStore_Checksum_ZeroSeq(t *testing.T) {
	// Ensure checksum of 0 is deterministic.
	c1 := checksumSeq(0)
	c2 := checksumSeq(0)
	if c1 != c2 {
		t.Error("checksum of 0 not deterministic")
	}
}
