package wal

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkAppend_NoSync(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.wal")
	w, err := Open(path, Options{SyncOnWrite: false})
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()

	r := Record{Type: RecordPut, Key: []byte("benchkey"), Value: []byte("benchvalue-1234567890")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Append(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAppend_Sync(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench_sync.wal")
	w, err := Open(path, Options{SyncOnWrite: true})
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()

	r := Record{Type: RecordPut, Key: []byte("benchkey"), Value: []byte("benchvalue-1234567890")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Append(r); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkReplay(b *testing.B, n int) {
	b.Helper()
	path := filepath.Join(b.TempDir(), fmt.Sprintf("replay_%d.wal", n))
	w, err := Open(path, Options{})
	if err != nil {
		b.Fatal(err)
	}

	r := Record{Type: RecordPut, Key: []byte("benchkey"), Value: []byte("benchvalue-1234567890")}
	for i := 0; i < n; i++ {
		if _, err := w.Append(r); err != nil {
			b.Fatal(err)
		}
	}
	w.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w2, err := Open(path, Options{})
		if err != nil {
			b.Fatal(err)
		}
		err = w2.Replay(func(Record) error { return nil })
		w2.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReplay_1k(b *testing.B)   { benchmarkReplay(b, 1_000) }
func BenchmarkReplay_100k(b *testing.B) { benchmarkReplay(b, 100_000) }
