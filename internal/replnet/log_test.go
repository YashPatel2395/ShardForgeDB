package replnet_test

import (
	"errors"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

func TestNewLog_Empty(t *testing.T) {
	l := replnet.NewLog()
	stats, err := l.Stats()
	if err != nil {
		t.Fatalf("Stats on empty log: %v", err)
	}
	if stats.Count != 0 {
		t.Errorf("Count = %d, want 0", stats.Count)
	}
	if stats.LastSeq != 0 {
		t.Errorf("LastSeq = %d, want 0", stats.LastSeq)
	}
}

func TestLog_Append_AssignsMonotonicSeqs(t *testing.T) {
	l := replnet.NewLog()
	e1, err := l.Append(replnet.OpPut, "key1", "val1")
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	e2, err := l.Append(replnet.OpPut, "key2", "val2")
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if e1.Seq != 1 {
		t.Errorf("e1.Seq = %d, want 1", e1.Seq)
	}
	if e2.Seq != 2 {
		t.Errorf("e2.Seq = %d, want 2", e2.Seq)
	}
}

func TestLog_Append_Put_StoresValue(t *testing.T) {
	l := replnet.NewLog()
	e, _ := l.Append(replnet.OpPut, "k", "v")
	if e.Key != "k" || e.Value != "v" {
		t.Errorf("got key=%q value=%q, want key=k value=v", e.Key, e.Value)
	}
	if e.Op != replnet.OpPut {
		t.Errorf("Op = %q, want %q", e.Op, replnet.OpPut)
	}
}

func TestLog_Append_Delete_EmptyValue(t *testing.T) {
	l := replnet.NewLog()
	e, _ := l.Append(replnet.OpDelete, "k", "")
	if e.Op != replnet.OpDelete {
		t.Errorf("Op = %q, want %q", e.Op, replnet.OpDelete)
	}
	if e.Value != "" {
		t.Errorf("Value = %q, want empty", e.Value)
	}
}

func TestLog_Append_InvalidOp_ReturnsError(t *testing.T) {
	l := replnet.NewLog()
	_, err := l.Append("upsert", "k", "v")
	if !errors.Is(err, replnet.ErrInvalidEntry) {
		t.Errorf("expected ErrInvalidEntry, got %v", err)
	}
}

func TestLog_Append_EmptyKey_ReturnsError(t *testing.T) {
	l := replnet.NewLog()
	_, err := l.Append(replnet.OpPut, "", "v")
	if !errors.Is(err, replnet.ErrInvalidEntry) {
		t.Errorf("expected ErrInvalidEntry for empty key, got %v", err)
	}
}

func TestLog_Append_TimestampSet(t *testing.T) {
	l := replnet.NewLog()
	e, _ := l.Append(replnet.OpPut, "k", "v")
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero, want non-zero")
	}
}

func TestLog_Stats_AfterAppends(t *testing.T) {
	l := replnet.NewLog()
	l.Append(replnet.OpPut, "a", "1")
	l.Append(replnet.OpPut, "b", "2")
	l.Append(replnet.OpDelete, "a", "")
	stats, err := l.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Count != 3 {
		t.Errorf("Count = %d, want 3", stats.Count)
	}
	if stats.LastSeq != 3 {
		t.Errorf("LastSeq = %d, want 3", stats.LastSeq)
	}
}

func TestLog_EntriesAfter_Zero_ReturnsAll(t *testing.T) {
	l := replnet.NewLog()
	l.Append(replnet.OpPut, "a", "1")
	l.Append(replnet.OpPut, "b", "2")
	l.Append(replnet.OpPut, "c", "3")

	entries, err := l.EntriesAfter(0, 0)
	if err != nil {
		t.Fatalf("EntriesAfter: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}
}

func TestLog_EntriesAfter_NonZero_FiltersCorrectly(t *testing.T) {
	l := replnet.NewLog()
	l.Append(replnet.OpPut, "a", "1")
	l.Append(replnet.OpPut, "b", "2")
	l.Append(replnet.OpPut, "c", "3")

	entries, err := l.EntriesAfter(2, 0)
	if err != nil {
		t.Fatalf("EntriesAfter: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
	if entries[0].Seq != 3 {
		t.Errorf("entry[0].Seq = %d, want 3", entries[0].Seq)
	}
}

func TestLog_EntriesAfter_LimitRespected(t *testing.T) {
	l := replnet.NewLog()
	for i := 0; i < 10; i++ {
		l.Append(replnet.OpPut, "k", "v")
	}
	entries, err := l.EntriesAfter(0, 3)
	if err != nil {
		t.Fatalf("EntriesAfter: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}
}

func TestLog_EntriesAfter_BeyondLastSeq_ReturnsEmpty(t *testing.T) {
	l := replnet.NewLog()
	l.Append(replnet.OpPut, "k", "v")

	entries, err := l.EntriesAfter(999, 0)
	if err != nil {
		t.Fatalf("EntriesAfter: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestLog_EntriesAfter_AscendingOrder(t *testing.T) {
	l := replnet.NewLog()
	l.Append(replnet.OpPut, "a", "1")
	l.Append(replnet.OpPut, "b", "2")
	l.Append(replnet.OpPut, "c", "3")

	entries, _ := l.EntriesAfter(0, 0)
	for i := 1; i < len(entries); i++ {
		if entries[i].Seq <= entries[i-1].Seq {
			t.Errorf("entries not in ascending Seq order at index %d: %d <= %d",
				i, entries[i].Seq, entries[i-1].Seq)
		}
	}
}

func TestLog_Close_PreventsAppend(t *testing.T) {
	l := replnet.NewLog()
	l.Close()
	_, err := l.Append(replnet.OpPut, "k", "v")
	if !errors.Is(err, replnet.ErrClosed) {
		t.Errorf("expected ErrClosed after Close, got %v", err)
	}
}

func TestLog_Close_PreventsStats(t *testing.T) {
	l := replnet.NewLog()
	l.Close()
	_, err := l.Stats()
	if !errors.Is(err, replnet.ErrClosed) {
		t.Errorf("expected ErrClosed after Close, got %v", err)
	}
}

func TestLog_Close_PreventsEntriesAfter(t *testing.T) {
	l := replnet.NewLog()
	l.Close()
	_, err := l.EntriesAfter(0, 0)
	if !errors.Is(err, replnet.ErrClosed) {
		t.Errorf("expected ErrClosed after Close, got %v", err)
	}
}

func TestLog_Close_Idempotent(t *testing.T) {
	l := replnet.NewLog()
	if err := l.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestLog_Concurrent_Append(t *testing.T) {
	l := replnet.NewLog()
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			l.Append(replnet.OpPut, "concurrent-key", "val")
		}()
	}
	for i := 0; i < 50; i++ {
		<-done
	}
	stats, err := l.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Count != 50 {
		t.Errorf("Count = %d, want 50", stats.Count)
	}
}

func TestLog_Concurrent_AppendAndRead(t *testing.T) {
	l := replnet.NewLog()
	// Pre-populate
	for i := 0; i < 20; i++ {
		l.Append(replnet.OpPut, "k", "v")
	}
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			l.Append(replnet.OpPut, "k", "v")
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			l.EntriesAfter(0, 5)
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
