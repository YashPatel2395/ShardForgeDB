package shard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "shard-test-*")
	if err != nil {
		t.Fatalf("tempDir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func mustOpen(t *testing.T, opts Options) *Store {
	t.Helper()
	s, err := Open(opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustPut(t *testing.T, s *Store, key, value string) {
	t.Helper()
	if err := s.Put([]byte(key), []byte(value)); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
}

func mustGet(t *testing.T, s *Store, key string) (string, bool) {
	t.Helper()
	v, ok, err := s.Get([]byte(key))
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	return string(v), ok
}

// ── Test 1: Open rejects empty Dir ───────────────────────────────────────────

func TestOpen_RejectsEmptyDir(t *testing.T) {
	_, err := Open(Options{ShardCount: 4})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("want ErrInvalidOptions, got %v", err)
	}
}

// ── Test 2: Open rejects ShardCount <= 0 on first create ─────────────────────

func TestOpen_RejectsZeroShardCount(t *testing.T) {
	dir := tempDir(t)
	_, err := Open(Options{Dir: dir, ShardCount: 0})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("want ErrInvalidOptions, got %v", err)
	}
}

func TestOpen_RejectsNegativeShardCount(t *testing.T) {
	dir := tempDir(t)
	_, err := Open(Options{Dir: dir, ShardCount: -1})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("want ErrInvalidOptions, got %v", err)
	}
}

// ── Test 3: Open defaults VirtualNodes to 128 ─────────────────────────────────

func TestOpen_DefaultsVirtualNodes128(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 2})
	if s.opts.VirtualNodes != 128 {
		t.Fatalf("want VirtualNodes=128, got %d", s.opts.VirtualNodes)
	}
}

// ── Test 4: Open creates manifest ────────────────────────────────────────────

func TestOpen_CreatesManifest(t *testing.T) {
	dir := tempDir(t)
	mustOpen(t, Options{Dir: dir, ShardCount: 2})

	manifestPath := filepath.Join(dir, manifestFileName)
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest not created: %v", err)
	}

	data, _ := os.ReadFile(manifestPath)
	var m shardingManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if m.ShardCount != 2 {
		t.Errorf("want shard_count=2, got %d", m.ShardCount)
	}
	if m.Hash != hashAlgorithm {
		t.Errorf("want hash=%q, got %q", hashAlgorithm, m.Hash)
	}
}

// ── Test 5: Open creates expected shard directories ───────────────────────────

func TestOpen_CreatesSharDDirectories(t *testing.T) {
	dir := tempDir(t)
	mustOpen(t, Options{Dir: dir, ShardCount: 3})

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("shard-%04d", i)
		p := filepath.Join(dir, "shards", name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("shard dir %q missing: %v", p, err)
		}
	}
}

// ── Test 6: Reopen loads manifest when ShardCount and VirtualNodes are zero ──

func TestReopen_LoadsManifest(t *testing.T) {
	dir := tempDir(t)
	s1 := mustOpen(t, Options{Dir: dir, ShardCount: 3, VirtualNodes: 64})
	s1.Close()

	// Reopen with zero values — should load from manifest.
	s2, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if s2.opts.ShardCount != 3 {
		t.Errorf("want ShardCount=3, got %d", s2.opts.ShardCount)
	}
	if s2.opts.VirtualNodes != 64 {
		t.Errorf("want VirtualNodes=64, got %d", s2.opts.VirtualNodes)
	}
}

// ── Test 7: Reopen rejects mismatched ShardCount ──────────────────────────────

func TestReopen_RejectsMismatchedShardCount(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})
	s.Close()

	_, err := Open(Options{Dir: dir, ShardCount: 8})
	if !errors.Is(err, ErrShardMismatch) {
		t.Fatalf("want ErrShardMismatch, got %v", err)
	}
}

// ── Test 8: Reopen rejects mismatched VirtualNodes ────────────────────────────

func TestReopen_RejectsMismatchedVirtualNodes(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4, VirtualNodes: 64})
	s.Close()

	_, err := Open(Options{Dir: dir, VirtualNodes: 256})
	if !errors.Is(err, ErrShardMismatch) {
		t.Fatalf("want ErrShardMismatch, got %v", err)
	}
}

// ── Test 9: Manifest rejects unsupported version ──────────────────────────────

func TestManifest_RejectsUnsupportedVersion(t *testing.T) {
	m := &shardingManifest{
		Version:      99,
		ShardCount:   1,
		VirtualNodes: 128,
		Hash:         hashAlgorithm,
		ShardPrefix:  defaultShardPrefix,
		Shards:       []shardEntry{{ID: 0, Name: "shard-0000", Path: "shards/shard-0000"}},
	}
	if err := validateManifest(m); !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("want ErrCorruptManifest, got %v", err)
	}
}

// ── Test 10: Manifest rejects absolute shard path ────────────────────────────

func TestManifest_RejectsAbsolutePath(t *testing.T) {
	m := &shardingManifest{
		Version:      manifestVersion,
		ShardCount:   1,
		VirtualNodes: 128,
		Hash:         hashAlgorithm,
		ShardPrefix:  defaultShardPrefix,
		Shards:       []shardEntry{{ID: 0, Name: "shard-0000", Path: "/absolute/path"}},
	}
	if err := validateManifest(m); !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("want ErrCorruptManifest, got %v", err)
	}
}

// ── Test 11: Manifest rejects path traversal ──────────────────────────────────

func TestManifest_RejectsPathTraversal(t *testing.T) {
	m := &shardingManifest{
		Version:      manifestVersion,
		ShardCount:   1,
		VirtualNodes: 128,
		Hash:         hashAlgorithm,
		ShardPrefix:  defaultShardPrefix,
		Shards:       []shardEntry{{ID: 0, Name: "bad", Path: "../escape"}},
	}
	if err := validateManifest(m); !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("want ErrCorruptManifest, got %v", err)
	}
}

// ── Test 12: Manifest rejects duplicate shard ID ─────────────────────────────

func TestManifest_RejectsDuplicateID(t *testing.T) {
	m := &shardingManifest{
		Version:      manifestVersion,
		ShardCount:   2,
		VirtualNodes: 128,
		Hash:         hashAlgorithm,
		ShardPrefix:  defaultShardPrefix,
		Shards: []shardEntry{
			{ID: 0, Name: "shard-0000", Path: "shards/shard-0000"},
			{ID: 0, Name: "shard-0001", Path: "shards/shard-0001"},
		},
	}
	if err := validateManifest(m); !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("want ErrCorruptManifest, got %v", err)
	}
}

// ── Test 13: Manifest rejects duplicate shard name ────────────────────────────

func TestManifest_RejectsDuplicateName(t *testing.T) {
	m := &shardingManifest{
		Version:      manifestVersion,
		ShardCount:   2,
		VirtualNodes: 128,
		Hash:         hashAlgorithm,
		ShardPrefix:  defaultShardPrefix,
		Shards: []shardEntry{
			{ID: 0, Name: "same-name", Path: "shards/shard-0000"},
			{ID: 1, Name: "same-name", Path: "shards/shard-0001"},
		},
	}
	if err := validateManifest(m); !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("want ErrCorruptManifest, got %v", err)
	}
}

// ── Test 14: Hash ring is deterministic ───────────────────────────────────────

func TestRing_IsDeterministic(t *testing.T) {
	r1 := newRing(4, 128)
	r2 := newRing(4, 128)

	keys := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for _, k := range keys {
		i1 := r1.shardIndex([]byte(k))
		i2 := r2.shardIndex([]byte(k))
		if i1 != i2 {
			t.Errorf("key %q: ring1=%d ring2=%d", k, i1, i2)
		}
	}
}

// ── Test 15: Same key maps to same shard across reopen ───────────────────────

func TestSameKeyStableAcrossReopen(t *testing.T) {
	dir := tempDir(t)
	key := []byte("stable-routing-key")

	s1 := mustOpen(t, Options{Dir: dir, ShardCount: 4})
	info1, err := s1.ShardForKey(key)
	if err != nil {
		t.Fatalf("ShardForKey: %v", err)
	}
	s1.Close()

	s2, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	info2, err := s2.ShardForKey(key)
	if err != nil {
		t.Fatalf("ShardForKey after reopen: %v", err)
	}

	if info1.ID != info2.ID {
		t.Errorf("shard changed after reopen: before=%d after=%d", info1.ID, info2.ID)
	}
}

// ── Test 16: Different keys distribute across multiple shards ────────────────

func TestKeyDistribution(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	// Use hash-spread keys so consecutive indices produce well-distributed hash
	// values rather than correlated strings that cluster into one ring segment.
	seen := make(map[int]bool)
	for i := 0; i < 200; i++ {
		// Hash the index to get a spread-out key string.
		h := fnv1a64([]byte(fmt.Sprintf("seed-%d", i)))
		key := fmt.Sprintf("dist-%016x", h)
		info, err := s.ShardForKey([]byte(key))
		if err != nil {
			t.Fatalf("ShardForKey: %v", err)
		}
		seen[info.ID] = true
	}
	if len(seen) < 2 {
		t.Errorf("200 hash-spread keys routed to fewer than 2 shards: seen=%v", seen)
	}
}

// ── Test 17: ShardForKey rejects empty key ────────────────────────────────────

func TestShardForKey_RejectsEmptyKey(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 2})
	_, err := s.ShardForKey([]byte{})
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("want ErrInvalidKey, got %v", err)
	}
}

// ── Test 18: Put then Get works ───────────────────────────────────────────────

func TestPutGet(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})
	mustPut(t, s, "hello", "world")
	v, ok := mustGet(t, s, "hello")
	if !ok || v != "world" {
		t.Errorf("Get: got (%q, %v), want (\"world\", true)", v, ok)
	}
}

// ── Test 19: Get missing returns false ────────────────────────────────────────

func TestGet_MissingKey(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 2})
	_, ok, err := s.Get([]byte("no-such-key"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Error("want ok=false for missing key")
	}
}

// ── Test 20: Put overwrites existing key ──────────────────────────────────────

func TestPut_Overwrite(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 2})
	mustPut(t, s, "k", "v1")
	mustPut(t, s, "k", "v2")
	v, ok := mustGet(t, s, "k")
	if !ok || v != "v2" {
		t.Errorf("after overwrite: got (%q, %v), want (\"v2\", true)", v, ok)
	}
}

// ── Test 21: Delete removes key ───────────────────────────────────────────────

func TestDelete_RemovesKey(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 2})
	mustPut(t, s, "del-me", "val")
	if err := s.Delete([]byte("del-me")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, ok, err := s.Get([]byte("del-me"))
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if ok {
		t.Error("key should be gone after Delete")
	}
}

// ── Test 22: Delete missing key is safe ──────────────────────────────────────

func TestDelete_MissingKeyIsSafe(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 2})
	if err := s.Delete([]byte("never-existed")); err != nil {
		t.Fatalf("Delete missing key: %v", err)
	}
}

// ── Test 23: Put/Get across many keys uses multiple shards ───────────────────

func TestPutGet_UsesMultipleShards(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	// Use hash-spread keys to guarantee multi-shard distribution.
	keys := make([]string, 200)
	vals := make([]string, 200)
	for i := 0; i < 200; i++ {
		h := fnv1a64([]byte(fmt.Sprintf("seed-%d", i)))
		keys[i] = fmt.Sprintf("dist-%016x", h)
		vals[i] = fmt.Sprintf("val-%016x", h)
	}

	for i := 0; i < 200; i++ {
		mustPut(t, s, keys[i], vals[i])
	}
	for i := 0; i < 200; i++ {
		got, ok := mustGet(t, s, keys[i])
		if !ok || got != vals[i] {
			t.Errorf("key %q: got (%q, %v), want (%q, true)", keys[i], got, ok, vals[i])
		}
	}

	// Confirm multiple shards are actually used.
	shardCounts := make(map[int]int)
	for i := 0; i < 200; i++ {
		info, _ := s.ShardForKey([]byte(keys[i]))
		shardCounts[info.ID]++
	}
	if len(shardCounts) < 2 {
		t.Errorf("expected keys spread across multiple shards, got %v", shardCounts)
	}
}

// ── Test 24: Scan fans out and returns sorted results ────────────────────────

func TestScan_FansOut(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	keys := []string{"apple", "banana", "cherry", "date", "elderberry"}
	for _, k := range keys {
		mustPut(t, s, k, k+"-val")
	}

	entries, err := s.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != len(keys) {
		t.Fatalf("want %d entries, got %d", len(keys), len(entries))
	}
	// Must be sorted.
	for i := 1; i < len(entries); i++ {
		if string(entries[i].Key) < string(entries[i-1].Key) {
			t.Errorf("results not sorted at index %d: %q < %q",
				i, entries[i].Key, entries[i-1].Key)
		}
	}
}

// ── Test 25: Scan respects start/end bounds ───────────────────────────────────

func TestScan_RespectsStartEnd(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	for _, k := range []string{"aaa", "bbb", "ccc", "ddd", "eee"} {
		mustPut(t, s, k, "v")
	}

	entries, err := s.Scan([]byte("bbb"), []byte("ddd"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = string(e.Key)
	}
	// engine.Scan semantics: start inclusive, end exclusive
	// (bbb and ccc should be in range, ddd exclusive).
	for _, k := range got {
		if k < "bbb" || k >= "ddd" {
			t.Errorf("key %q out of scan range [bbb, ddd)", k)
		}
	}
}

// ── Test 26: Scan resolves duplicate keys by highest Seq ─────────────────────

func TestScan_DeduplicatesByHighestSeq(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	// Write the same key twice to build up a sequence in the same engine.
	// Then validate that Scan returns the latest value.
	mustPut(t, s, "dup-key", "first")
	mustPut(t, s, "dup-key", "second")

	entries, err := s.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, e := range entries {
		if string(e.Key) == "dup-key" {
			if string(e.Value) != "second" {
				t.Errorf("want \"second\", got %q", e.Value)
			}
			return
		}
	}
	t.Error("dup-key not found in Scan results")
}

// TestScan_DeduplicatesAcrossShardEngines injects the same key into two shard
// engines directly and verifies the higher-Seq value wins in the Scan merge.
func TestScan_DeduplicatesAcrossShardEngines(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	const sharedKey = "cross-shard-dup"

	// Each engine has its own independent sequence counter starting at 1.
	// To ensure shard[1] gets a strictly higher seq than shard[0], we advance
	// shard[1]'s counter first with dummy writes.
	for i := 0; i < 5; i++ {
		if err := s.shards[1].engine.Put([]byte(fmt.Sprintf("dummy-%d", i)), []byte("x")); err != nil {
			t.Fatalf("advance seq shard 1: %v", err)
		}
	}
	// shard[0] seq counter is still at 0; next write gets seq=1.
	// shard[1] seq counter is at 5; next write gets seq=6.
	if err := s.shards[0].engine.Put([]byte(sharedKey), []byte("older")); err != nil {
		t.Fatalf("direct engine Put shard 0: %v", err)
	}
	if err := s.shards[1].engine.Put([]byte(sharedKey), []byte("newer")); err != nil {
		t.Fatalf("direct engine Put shard 1: %v", err)
	}

	entries, err := s.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var found []Entry
	for _, e := range entries {
		if string(e.Key) == sharedKey {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 merged result for %q, got %d: %v",
			sharedKey, len(found), found)
	}
	if string(found[0].Value) != "newer" {
		t.Errorf("expected \"newer\" (higher Seq), got %q", found[0].Value)
	}
}

// ── Test 27: Flush persists all shards through reopen ────────────────────────

func TestFlush_PersistsThroughReopen(t *testing.T) {
	dir := tempDir(t)
	s1 := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	for i := 0; i < 20; i++ {
		mustPut(t, s1, fmt.Sprintf("flush-key-%03d", i), fmt.Sprintf("v%d", i))
	}
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s1.Close()

	s2, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("flush-key-%03d", i)
		want := fmt.Sprintf("v%d", i)
		got, ok := mustGet(t, s2, key)
		if !ok || got != want {
			t.Errorf("after reopen: key %q got (%q, %v), want (%q, true)", key, got, ok, want)
		}
	}
}

// ── Test 28: Compact preserves all keys through reopen ───────────────────────

func TestCompact_PreservesKeysThroughReopen(t *testing.T) {
	dir := tempDir(t)
	s1 := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	for i := 0; i < 20; i++ {
		mustPut(t, s1, fmt.Sprintf("compact-key-%03d", i), fmt.Sprintf("v%d", i))
	}
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	// Write again to create a second flush candidate.
	for i := 0; i < 20; i++ {
		mustPut(t, s1, fmt.Sprintf("compact-key-%03d", i), fmt.Sprintf("v%d-new", i))
	}
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush 2: %v", err)
	}
	if err := s1.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	s1.Close()

	s2, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("compact-key-%03d", i)
		want := fmt.Sprintf("v%d-new", i)
		got, ok := mustGet(t, s2, key)
		if !ok || got != want {
			t.Errorf("after compact+reopen: key %q got (%q, %v), want (%q, true)", key, got, ok, want)
		}
	}
}

// ── Test 29: Close is idempotent ─────────────────────────────────────────────

func TestClose_IsIdempotent(t *testing.T) {
	dir := tempDir(t)
	s, err := Open(Options{Dir: dir, ShardCount: 2})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// ── Test 30: Operations after Close return ErrClosed ─────────────────────────

func TestClose_OperationsReturnErrClosed(t *testing.T) {
	dir := tempDir(t)
	s, err := Open(Options{Dir: dir, ShardCount: 2})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()

	if err := s.Put([]byte("k"), []byte("v")); !errors.Is(err, ErrClosed) {
		t.Errorf("Put after Close: want ErrClosed, got %v", err)
	}
	if err := s.Delete([]byte("k")); !errors.Is(err, ErrClosed) {
		t.Errorf("Delete after Close: want ErrClosed, got %v", err)
	}
	if _, _, err := s.Get([]byte("k")); !errors.Is(err, ErrClosed) {
		t.Errorf("Get after Close: want ErrClosed, got %v", err)
	}
	if _, err := s.Scan(nil, nil); !errors.Is(err, ErrClosed) {
		t.Errorf("Scan after Close: want ErrClosed, got %v", err)
	}
	if err := s.Flush(); !errors.Is(err, ErrClosed) {
		t.Errorf("Flush after Close: want ErrClosed, got %v", err)
	}
	if err := s.Compact(); !errors.Is(err, ErrClosed) {
		t.Errorf("Compact after Close: want ErrClosed, got %v", err)
	}
	if _, err := s.ShardForKey([]byte("k")); !errors.Is(err, ErrClosed) {
		t.Errorf("ShardForKey after Close: want ErrClosed, got %v", err)
	}
}

// ── Test 31: Concurrent Put/Get/Scan race-safe ────────────────────────────────

func TestConcurrent_PutGetScan(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	const goroutines = 8
	const ops = 50

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := fmt.Sprintf("g%d-k%d", g, i)
				if err := s.Put([]byte(key), []byte("v")); err != nil {
					return // store may be closed; acceptable
				}
				s.Get([]byte(key))
				s.Scan(nil, nil)
			}
		}()
	}
	wg.Wait()
}

// ── Test 32: Concurrent Close with Get/Put race-safe ─────────────────────────

func TestConcurrent_CloseWithPutGet(t *testing.T) {
	dir := tempDir(t)
	s, err := Open(Options{Dir: dir, ShardCount: 4})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Seed some data.
	for i := 0; i < 20; i++ {
		s.Put([]byte(fmt.Sprintf("seed-%03d", i)), []byte("v"))
	}

	var wg sync.WaitGroup
	// Writers.
	for g := 0; g < 4; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				key := fmt.Sprintf("concurrent-g%d-k%d", g, i)
				err := s.Put([]byte(key), []byte("v"))
				if err != nil && !errors.Is(err, ErrClosed) {
					// unexpected error
				}
			}
		}()
	}
	// Readers.
	for g := 0; g < 4; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				key := fmt.Sprintf("seed-%03d", g*5+i%5)
				s.Get([]byte(key))
			}
		}()
	}
	// Close races with all of the above.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Close()
	}()
	wg.Wait()
}

// ── Test 33: Stats aggregates shard stats correctly ───────────────────────────

func TestStats_AggregatesCorrectly(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	for i := 0; i < 40; i++ {
		mustPut(t, s, fmt.Sprintf("stats-key-%03d", i), "v")
	}

	st := s.Stats()
	if st.ShardCount != 4 {
		t.Errorf("want ShardCount=4, got %d", st.ShardCount)
	}
	if st.TotalMemTableEntries != 40 {
		t.Errorf("want TotalMemTableEntries=40, got %d", st.TotalMemTableEntries)
	}
	if len(st.Shards) != 4 {
		t.Errorf("want 4 per-shard stats, got %d", len(st.Shards))
	}
}

// ── Test 34: Stats includes per-shard entries ─────────────────────────────────

func TestStats_IncludesPerShardEntries(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 4})

	for i := 0; i < 40; i++ {
		mustPut(t, s, fmt.Sprintf("pse-key-%03d", i), "v")
	}

	st := s.Stats()
	total := 0
	for _, ss := range st.Shards {
		if ss.Name == "" {
			t.Errorf("shard %d has empty Name", ss.ID)
		}
		total += ss.MemTableEntries
	}
	if total != st.TotalMemTableEntries {
		t.Errorf("per-shard total %d != aggregate %d", total, st.TotalMemTableEntries)
	}
}

// ── Test 35: Large deterministic workload ─────────────────────────────────────

func TestLargeWorkload(t *testing.T) {
	const n = 5000
	dir := tempDir(t)
	s1 := mustOpen(t, Options{Dir: dir, ShardCount: 8})

	// Insert n keys deterministically.
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("large-key-%07d", i)
		val := fmt.Sprintf("large-val-%07d", i)
		if err := s1.Put([]byte(key), []byte(val)); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s1.Close()

	// Reopen and spot-check.
	s2, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	spotChecks := []int{0, 1, 100, 999, 2500, 4000, 4999}
	for _, i := range spotChecks {
		key := fmt.Sprintf("large-key-%07d", i)
		want := fmt.Sprintf("large-val-%07d", i)
		got, ok := mustGet(t, s2, key)
		if !ok || got != want {
			t.Errorf("spot-check key %q: got (%q, %v), want (%q, true)", key, got, ok, want)
		}
	}

	// Scan all and verify count.
	entries, err := s2.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != n {
		t.Errorf("Scan returned %d entries, want %d", len(entries), n)
	}
	// Verify sort order.
	for i := 1; i < len(entries); i++ {
		if string(entries[i].Key) < string(entries[i-1].Key) {
			t.Errorf("scan not sorted at %d", i)
		}
	}
}

// ── Test 36: Empty key rejected for all operations ────────────────────────────

func TestEmptyKey_RejectedForAllOps(t *testing.T) {
	dir := tempDir(t)
	s := mustOpen(t, Options{Dir: dir, ShardCount: 2})

	if err := s.Put([]byte{}, []byte("v")); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Put empty key: want ErrInvalidKey, got %v", err)
	}
	if err := s.Delete([]byte{}); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Delete empty key: want ErrInvalidKey, got %v", err)
	}
	if _, _, err := s.Get([]byte{}); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Get empty key: want ErrInvalidKey, got %v", err)
	}
	if _, err := s.ShardForKey([]byte{}); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("ShardForKey empty key: want ErrInvalidKey, got %v", err)
	}
}

// ── Test 37: Manifest write is atomic (temp file cleaned up) ─────────────────

func TestManifest_WriteIsAtomic(t *testing.T) {
	dir := tempDir(t)
	mustOpen(t, Options{Dir: dir, ShardCount: 2})

	// After Open completes, no .tmp file should linger.
	tmpPath := filepath.Join(dir, manifestFileName+".tmp")
	if _, err := os.Stat(tmpPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp manifest file still exists after Open: %v", err)
	}
	// The real manifest must exist.
	if _, err := os.Stat(filepath.Join(dir, manifestFileName)); err != nil {
		t.Errorf("manifest missing after Open: %v", err)
	}
}

// ── Test 38: Corrupt manifest returns ErrCorruptManifest ─────────────────────

func TestManifest_CorruptReturnsError(t *testing.T) {
	dir := tempDir(t)
	// Create directory and write a bad manifest.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFileName),
		[]byte("this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(Options{Dir: dir})
	if !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("want ErrCorruptManifest, got %v", err)
	}
}

// ── Test 39: Unknown hash algorithm returns ErrCorruptManifest ───────────────

func TestManifest_UnknownHashAlgorithm(t *testing.T) {
	dir := tempDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := shardingManifest{
		Version:      manifestVersion,
		ShardCount:   2,
		VirtualNodes: 128,
		Hash:         "sha256", // unsupported
		ShardPrefix:  "shard",
		Shards: []shardEntry{
			{ID: 0, Name: "shard-0000", Path: "shards/shard-0000"},
			{ID: 1, Name: "shard-0001", Path: "shards/shard-0001"},
		},
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(filepath.Join(dir, manifestFileName), data, 0o644)

	_, err := Open(Options{Dir: dir})
	if !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("want ErrCorruptManifest, got %v", err)
	}
}

// ── Test 40: Shard names are deterministic ────────────────────────────────────

func TestShardNames_AreDeterministic(t *testing.T) {
	dir1 := tempDir(t)
	dir2 := tempDir(t)

	s1 := mustOpen(t, Options{Dir: dir1, ShardCount: 4})
	s2 := mustOpen(t, Options{Dir: dir2, ShardCount: 4})

	names1 := make([]string, len(s1.shards))
	names2 := make([]string, len(s2.shards))
	for i, sh := range s1.shards {
		names1[i] = sh.info.Name
	}
	for i, sh := range s2.shards {
		names2[i] = sh.info.Name
	}
	sort.Strings(names1)
	sort.Strings(names2)
	for i := range names1 {
		if names1[i] != names2[i] {
			t.Errorf("shard names differ at %d: %q vs %q", i, names1[i], names2[i])
		}
	}
}
