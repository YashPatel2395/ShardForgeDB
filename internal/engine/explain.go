// Package engine — ExplainGet, ExplainPut, ExplainDelete, ExplainScan
//
// These methods mirror the execution paths of their non-Explain counterparts
// exactly. Each step is recorded by the real code that performs the operation;
// no trace steps are fabricated or pre-scripted.
//
// Hard rule: if a step does not reflect a real operation, it must not appear.
package engine

import (
	"fmt"
	"sort"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/memtable"
	"github.com/YashPatel2395/ShardForgeDB/internal/sstable"
	"github.com/YashPatel2395/ShardForgeDB/internal/trace"
	"github.com/YashPatel2395/ShardForgeDB/internal/wal"
)

// ExplainPut performs a Put and records a step-by-step trace of the execution.
//
// Trace steps reflect the real write path:
//  1. Key validation
//  2. WAL append
//  3. MemTable insert
//
// The non-Explain Put behaviour is preserved exactly.
func (e *Engine) ExplainPut(key, value []byte) (*trace.Trace, error) {
	tr := trace.New(trace.OpPut, string(key))

	if len(key) == 0 {
		err := fmt.Errorf("%w: key must be non-empty", ErrInvalidKey)
		tr.Step(trace.ComponentEngine, trace.StepTypeKeyValidated, trace.StatusError, 0,
			"key_len=0 empty=true")
		tr.Finish(err)
		return tr, err
	}
	tr.Step(trace.ComponentEngine, trace.StepTypeKeyValidated, trace.StatusOK, 0,
		fmt.Sprintf("key_len=%d value_len=%d", len(key), len(value)))

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valCopy := make([]byte, len(value))
	copy(valCopy, value)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		tr.Finish(ErrClosed)
		return tr, ErrClosed
	}

	seq := e.nextSeq
	e.nextSeq++

	t0 := time.Now()
	_, err := e.walLog.Append(wal.Record{
		Type:  wal.RecordPut,
		Key:   keyCopy,
		Value: valCopy,
		Seq:   seq,
	})
	walDur := time.Since(t0)

	if err != nil {
		e.nextSeq--
		tr.Step(trace.ComponentWAL, trace.StepTypeWALAppend, trace.StatusError, walDur,
			fmt.Sprintf("seq=%d key_len=%d", seq, len(key)))
		wrapped := fmt.Errorf("engine: WAL append: %w", err)
		tr.Finish(wrapped)
		return tr, wrapped
	}
	tr.Step(trace.ComponentWAL, trace.StepTypeWALAppend, trace.StatusOK, walDur,
		fmt.Sprintf("seq=%d key_len=%d value_len=%d", seq, len(key), len(value)))

	t1 := time.Now()
	err = e.mem.Put(keyCopy, valCopy, seq)
	memDur := time.Since(t1)

	if err != nil {
		tr.Step(trace.ComponentMemTable, trace.StepTypeMemTablePut, trace.StatusError, memDur,
			fmt.Sprintf("seq=%d key_len=%d", seq, len(key)))
		wrapped := fmt.Errorf("engine: memtable put: %w", err)
		tr.Finish(wrapped)
		return tr, wrapped
	}
	tr.Step(trace.ComponentMemTable, trace.StepTypeMemTablePut, trace.StatusOK, memDur,
		fmt.Sprintf("seq=%d key_len=%d", seq, len(key)))

	tr.Finish(nil)
	return tr, nil
}

// ExplainGet performs a Get and records a step-by-step trace of the execution.
//
// Trace steps reflect the real read path:
//  1. Key validation
//  2. MemTable lookup (hit or miss)
//  3. For each SSTable (newest first):
//     a. Bounds check (skip if key is outside [minKey, maxKey])
//     b. Bloom filter check (skip or proceed)
//     c. SSTable index lookup (hit or miss)
//  4. NOT_FOUND if key is absent everywhere
//
// The non-Explain Get behaviour is preserved exactly.
func (e *Engine) ExplainGet(key []byte) (*trace.Trace, []byte, bool, error) {
	tr := trace.New(trace.OpGet, string(key))

	if len(key) == 0 {
		tr.Step(trace.ComponentEngine, trace.StepTypeKeyValidated, trace.StatusOK, 0,
			"key_len=0 empty=true")
		tr.Finish(nil)
		return tr, nil, false, nil
	}
	tr.Step(trace.ComponentEngine, trace.StepTypeKeyValidated, trace.StatusOK, 0,
		fmt.Sprintf("key_len=%d", len(key)))

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		tr.Finish(ErrClosed)
		return tr, nil, false, ErrClosed
	}

	// 1. MemTable.
	t0 := time.Now()
	entry, ok := e.mem.Get(key)
	memDur := time.Since(t0)

	if ok {
		if entry.Kind == memtable.EntryPut {
			tr.Step(trace.ComponentMemTable, trace.StepTypeMemTableHit, trace.StatusOK, memDur,
				fmt.Sprintf("key_len=%d value_len=%d", len(key), len(entry.Value)))
			val := make([]byte, len(entry.Value))
			copy(val, entry.Value)
			tr.Finish(nil)
			return tr, val, true, nil
		}
		// Tombstone in MemTable.
		tr.Step(trace.ComponentMemTable, trace.StepTypeMemTableHit, trace.StatusOK, memDur,
			fmt.Sprintf("key_len=%d tombstone=true", len(key)))
		tr.Finish(nil)
		return tr, nil, false, nil
	}
	tr.Step(trace.ComponentMemTable, trace.StepTypeMemTableMiss, trace.StatusOK, memDur,
		fmt.Sprintf("key_len=%d", len(key)))

	// 2. SSTables, newest to oldest.
	for i := len(e.tables) - 1; i >= 0; i-- {
		th := e.tables[i]

		// Bounds skip — key is outside this SSTable's key range.
		if len(th.minKey) > 0 && string(key) < string(th.minKey) {
			tr.Step(trace.ComponentSSTable, trace.StepTypeBoundsSkip, trace.StatusSkipped, 0,
				fmt.Sprintf("sstable_id=%d reason=below_min", th.id))
			continue
		}
		if len(th.maxKey) > 0 && string(key) > string(th.maxKey) {
			tr.Step(trace.ComponentSSTable, trace.StepTypeBoundsSkip, trace.StatusSkipped, 0,
				fmt.Sprintf("sstable_id=%d reason=above_max", th.id))
			continue
		}

		// Bloom filter check.
		e.bloomChecks.Add(1)
		t1 := time.Now()
		contains := th.bloom.MightContain(key)
		bloomDur := time.Since(t1)

		if !contains {
			e.bloomNegSkips.Add(1)
			tr.Step(trace.ComponentBloom, trace.StepTypeBloomSkip, trace.StatusSkipped, bloomDur,
				fmt.Sprintf("sstable_id=%d", th.id))
			continue
		}
		tr.Step(trace.ComponentBloom, trace.StepTypeBloomCheck, trace.StatusOK, bloomDur,
			fmt.Sprintf("sstable_id=%d", th.id))

		// SSTable index lookup.
		t2 := time.Now()
		sstEntry, found, err := th.reader.Get(key)
		sstDur := time.Since(t2)

		if err != nil {
			wrapped := fmt.Errorf("engine: SSTable get: %w", err)
			tr.Finish(wrapped)
			return tr, nil, false, wrapped
		}
		if !found {
			tr.Step(trace.ComponentSSTable, trace.StepTypeSSTableMiss, trace.StatusOK, sstDur,
				fmt.Sprintf("sstable_id=%d", th.id))
			continue
		}
		if sstEntry.Kind == sstable.EntryPut {
			tr.Step(trace.ComponentSSTable, trace.StepTypeSSTableHit, trace.StatusOK, sstDur,
				fmt.Sprintf("sstable_id=%d value_len=%d", th.id, len(sstEntry.Value)))
			val := make([]byte, len(sstEntry.Value))
			copy(val, sstEntry.Value)
			tr.Finish(nil)
			return tr, val, true, nil
		}
		// Tombstone in SSTable.
		tr.Step(trace.ComponentSSTable, trace.StepTypeSSTableHit, trace.StatusOK, sstDur,
			fmt.Sprintf("sstable_id=%d tombstone=true", th.id))
		tr.Finish(nil)
		return tr, nil, false, nil
	}

	tr.Step(trace.ComponentEngine, trace.StepTypeNotFound, trace.StatusOK, 0,
		fmt.Sprintf("key_len=%d sstables_checked=%d", len(key), len(e.tables)))
	tr.Finish(nil)
	return tr, nil, false, nil
}

// ExplainDelete performs a Delete and records a step-by-step trace of the
// execution.
//
// Trace steps reflect the real write path:
//  1. Key validation
//  2. WAL append (tombstone record)
//  3. MemTable tombstone insert
//
// The non-Explain Delete behaviour is preserved exactly.
func (e *Engine) ExplainDelete(key []byte) (*trace.Trace, error) {
	tr := trace.New(trace.OpDelete, string(key))

	if len(key) == 0 {
		err := fmt.Errorf("%w: key must be non-empty", ErrInvalidKey)
		tr.Step(trace.ComponentEngine, trace.StepTypeKeyValidated, trace.StatusError, 0,
			"key_len=0 empty=true")
		tr.Finish(err)
		return tr, err
	}
	tr.Step(trace.ComponentEngine, trace.StepTypeKeyValidated, trace.StatusOK, 0,
		fmt.Sprintf("key_len=%d", len(key)))

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		tr.Finish(ErrClosed)
		return tr, ErrClosed
	}

	seq := e.nextSeq
	e.nextSeq++

	t0 := time.Now()
	_, err := e.walLog.Append(wal.Record{
		Type: wal.RecordDelete,
		Key:  keyCopy,
		Seq:  seq,
	})
	walDur := time.Since(t0)

	if err != nil {
		e.nextSeq--
		tr.Step(trace.ComponentWAL, trace.StepTypeWALAppend, trace.StatusError, walDur,
			fmt.Sprintf("seq=%d key_len=%d tombstone=true", seq, len(key)))
		wrapped := fmt.Errorf("engine: WAL append: %w", err)
		tr.Finish(wrapped)
		return tr, wrapped
	}
	tr.Step(trace.ComponentWAL, trace.StepTypeWALAppend, trace.StatusOK, walDur,
		fmt.Sprintf("seq=%d key_len=%d tombstone=true", seq, len(key)))

	t1 := time.Now()
	err = e.mem.Delete(keyCopy, seq)
	memDur := time.Since(t1)

	if err != nil {
		tr.Step(trace.ComponentMemTable, trace.StepTypeMemTableDelete, trace.StatusError, memDur,
			fmt.Sprintf("seq=%d key_len=%d", seq, len(key)))
		wrapped := fmt.Errorf("engine: memtable delete: %w", err)
		tr.Finish(wrapped)
		return tr, wrapped
	}
	tr.Step(trace.ComponentMemTable, trace.StepTypeMemTableDelete, trace.StatusOK, memDur,
		fmt.Sprintf("seq=%d key_len=%d", seq, len(key)))

	tr.Finish(nil)
	return tr, nil
}

// ExplainScan performs a Scan and records a step-by-step trace of the
// execution.
//
// Trace steps reflect the real scan path:
//  1. MemTable scan (records entry count)
//  2. Each SSTable scan (records table ID and entry count)
//  3. Merge + tombstone suppression (records live and suppressed counts)
//
// The non-Explain Scan behaviour is preserved exactly.
func (e *Engine) ExplainScan(start, end []byte) (*trace.Trace, []Entry, error) {
	tr := trace.New(trace.OpScan, "")

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		tr.Finish(ErrClosed)
		return tr, nil, ErrClosed
	}

	type candidate struct {
		seq   uint64
		kind  uint8 // 1=put, 2=delete
		value []byte
	}
	best := make(map[string]*candidate)

	merge := func(k []byte, seq uint64, kind uint8, value []byte) {
		ks := string(k)
		if c, ok := best[ks]; ok && seq <= c.seq {
			return
		}
		val := make([]byte, len(value))
		copy(val, value)
		best[ks] = &candidate{seq: seq, kind: kind, value: val}
	}

	// MemTable scan.
	t0 := time.Now()
	memEntries := e.mem.Scan(start, end)
	memDur := time.Since(t0)
	for _, me := range memEntries {
		kind := uint8(1)
		if me.Kind == memtable.EntryDelete {
			kind = 2
		}
		merge(me.Key, me.Seq, kind, me.Value)
	}
	tr.Step(trace.ComponentMemTable, trace.StepTypeScanSource, trace.StatusOK, memDur,
		fmt.Sprintf("source=memtable entry_count=%d", len(memEntries)))

	// All SSTables.
	for _, th := range e.tables {
		t1 := time.Now()
		sstEntries, err := th.reader.Scan(start, end)
		sstDur := time.Since(t1)
		if err != nil {
			wrapped := fmt.Errorf("engine: SSTable scan: %w", err)
			tr.Finish(wrapped)
			return tr, nil, wrapped
		}
		for _, se := range sstEntries {
			kind := uint8(1)
			if se.Kind == sstable.EntryDelete {
				kind = 2
			}
			merge(se.Key, se.Seq, kind, se.Value)
		}
		tr.Step(trace.ComponentSSTable, trace.StepTypeScanSource, trace.StatusOK, sstDur,
			fmt.Sprintf("source=sstable sstable_id=%d entry_count=%d", th.id, len(sstEntries)))
	}

	// Merge: collect live entries, suppress tombstones.
	t2 := time.Now()
	var result []Entry
	tombstoneCount := 0
	for ks, c := range best {
		if c.kind == 1 {
			k := make([]byte, len(ks))
			copy(k, ks)
			result = append(result, Entry{Key: k, Value: c.value, Seq: c.seq})
		} else {
			tombstoneCount++
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].Key) < string(result[j].Key)
	})
	mergeDur := time.Since(t2)

	tr.Step(trace.ComponentEngine, trace.StepTypeScanMerge, trace.StatusOK, mergeDur,
		fmt.Sprintf("total_keys=%d live=%d tombstones_suppressed=%d",
			len(best), len(result), tombstoneCount))

	tr.Finish(nil)
	return tr, result, nil
}
