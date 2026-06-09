package bloom

import (
	"fmt"
	"testing"
)

// BenchmarkNew_1k measures filter construction for 1,000 expected items.
func BenchmarkNew_1k(b *testing.B) {
	opts := Options{ExpectedItems: 1_000, FalsePositiveRate: 0.01}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New(opts)
	}
}

// BenchmarkNew_1M measures filter construction for 1,000,000 expected items.
func BenchmarkNew_1M(b *testing.B) {
	opts := Options{ExpectedItems: 1_000_000, FalsePositiveRate: 0.01}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New(opts)
	}
}

// BenchmarkAdd measures a single Add call on a pre-built filter.
func BenchmarkAdd(b *testing.B) {
	f, _ := New(Options{ExpectedItems: 100_000, FalsePositiveRate: 0.01})
	key := []byte("benchmark-add-key")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Add(key)
	}
}

// BenchmarkMightContain_Existing measures MightContain for a key that is present.
func BenchmarkMightContain_Existing(b *testing.B) {
	f, _ := New(Options{ExpectedItems: 100_000, FalsePositiveRate: 0.01})
	key := []byte("existing-key")
	f.Add(key)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.MightContain(key)
	}
}

// BenchmarkMightContain_Missing measures MightContain for a key never inserted.
func BenchmarkMightContain_Missing(b *testing.B) {
	f, _ := New(Options{ExpectedItems: 100_000, FalsePositiveRate: 0.01})
	key := []byte("definitely-not-added")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.MightContain(key)
	}
}

// BenchmarkMarshalBinary measures serialization of a populated filter.
func BenchmarkMarshalBinary(b *testing.B) {
	f, _ := New(Options{ExpectedItems: 10_000, FalsePositiveRate: 0.01})
	for i := 0; i < 10_000; i++ {
		f.Add([]byte(fmt.Sprintf("key-%08d", i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.MarshalBinary()
	}
}

// BenchmarkUnmarshalBinary measures deserialization of a serialized filter.
func BenchmarkUnmarshalBinary(b *testing.B) {
	f, _ := New(Options{ExpectedItems: 10_000, FalsePositiveRate: 0.01})
	for i := 0; i < 10_000; i++ {
		f.Add([]byte(fmt.Sprintf("key-%08d", i)))
	}
	data, _ := f.MarshalBinary()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		UnmarshalBinary(data)
	}
}

// BenchmarkAdd_100k measures bulk insertion of 100,000 keys.
func BenchmarkAdd_100k(b *testing.B) {
	keys := make([][]byte, 100_000)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("bulk-%09d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, _ := New(Options{ExpectedItems: 100_000, FalsePositiveRate: 0.01})
		for _, k := range keys {
			f.Add(k)
		}
	}
}

// BenchmarkQuery_100k measures bulk querying of 100,000 keys after insertion.
func BenchmarkQuery_100k(b *testing.B) {
	f, _ := New(Options{ExpectedItems: 100_000, FalsePositiveRate: 0.01})
	keys := make([][]byte, 100_000)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("query-%09d", i))
		f.Add(keys[i])
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range keys {
			f.MightContain(k)
		}
	}
}
