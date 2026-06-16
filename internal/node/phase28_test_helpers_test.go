package node

import (
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// mustNewQuiesceRecord is a test helper that fatals if NewQuiesceRecord returns an error.
// Use in Phase 28 tests to construct QuiesceRecord values for PromoteRequest bodies.
func mustNewQuiesceRecord(t *testing.T, primaryNodeID, primaryBaseURL string, latestSeq uint64) *replnet.QuiesceRecord {
	t.Helper()
	rec, err := replnet.NewQuiesceRecord(primaryNodeID, primaryBaseURL, latestSeq)
	if err != nil {
		t.Fatalf("NewQuiesceRecord: %v", err)
	}
	return rec
}

// p28PrimaryRunning opens a primary node, starts its background listener, and registers
// cleanup. Use for tests that call POST /replication/quiesce, which requires a bound
// listener so the quiesce record captures the real primary_base_url.
func p28PrimaryRunning(t *testing.T) *Server {
	t.Helper()
	srv := p28Primary(t)
	if err := srv.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	return srv
}
