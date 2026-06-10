package cluster_test

import (
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
)

// exampleJSON is the serialised form of ExampleLocal3Node, cached for benchmarks.
var exampleJSON []byte

func init() {
	var err error
	exampleJSON, err = cluster.Marshal(cluster.ExampleLocal3Node())
	if err != nil {
		panic("bench init: " + err.Error())
	}
}

// BenchmarkCluster_Parse measures JSON decode + Normalize + Validate.
func BenchmarkCluster_Parse(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := cluster.Parse(exampleJSON)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCluster_Validate measures Validate alone on a pre-normalised config.
func BenchmarkCluster_Validate(b *testing.B) {
	cfg := cluster.ExampleLocal3Node()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := cluster.Validate(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCluster_GatewayOptions measures building gateway.Options from a config.
func BenchmarkCluster_GatewayOptions(b *testing.B) {
	cfg := cluster.ExampleLocal3Node()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := cluster.GatewayOptions(cfg, 5*time.Second)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCluster_ProxyOptions measures building proxy.Options from a config.
func BenchmarkCluster_ProxyOptions(b *testing.B) {
	cfg := cluster.ExampleLocal3Node()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := cluster.ProxyOptions(cfg, 5*time.Second)
		if err != nil {
			b.Fatal(err)
		}
	}
}
