package replnet_test

import (
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

func BenchmarkLog_Append(b *testing.B) {
	l := replnet.NewLog()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Append(replnet.OpPut, "bench-key", "bench-value")
	}
}

func BenchmarkLog_Append_Parallel(b *testing.B) {
	l := replnet.NewLog()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Append(replnet.OpPut, "bench-key", "bench-value")
		}
	})
}

func BenchmarkLog_EntriesAfter_100(b *testing.B) {
	l := replnet.NewLog()
	for i := 0; i < 100; i++ {
		l.Append(replnet.OpPut, "k", "v")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.EntriesAfter(0, 0)
	}
}

func BenchmarkLog_EntriesAfter_1000(b *testing.B) {
	l := replnet.NewLog()
	for i := 0; i < 1000; i++ {
		l.Append(replnet.OpPut, "k", "v")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.EntriesAfter(0, 0)
	}
}

func BenchmarkLog_Stats(b *testing.B) {
	l := replnet.NewLog()
	for i := 0; i < 100; i++ {
		l.Append(replnet.OpPut, "k", "v")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Stats()
	}
}
