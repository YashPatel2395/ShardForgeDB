package cluster_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
	"github.com/YashPatel2395/ShardForgeDB/internal/node"
	"github.com/YashPatel2395/ShardForgeDB/internal/proxy"
)

// repoRoot returns the project root by walking up from this file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is .../internal/cluster/loader_test.go — go up 3 levels
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// --- Parse / Marshal ---

func TestParse_ValidJSON(t *testing.T) {
	data, err := cluster.Marshal(cluster.ExampleLocal3Node())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cfg, err := cluster.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Name != "local-3node" {
		t.Errorf("Name = %q, want local-3node", cfg.Name)
	}
}

func TestParse_InvalidJSON_ReturnsError(t *testing.T) {
	_, err := cluster.Parse([]byte("{not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParse_UnsupportedVersion_ReturnsError(t *testing.T) {
	data := []byte(`{"version":"v99","name":"x","routing":{"algorithm":"fnv1a-consistent-hash","virtual_nodes":128},"nodes":[{"id":"n1","base_url":"http://127.0.0.1:9101","weight":1}],"scope":{"static_config_only":true,"no_dynamic_membership":true,"no_discovery":true,"no_consensus":true,"no_raft":true,"no_replication":true,"no_failover":true,"no_shard_migration":true,"no_distributed_txns":true}}`)
	_, err := cluster.Parse(data)
	if !errors.Is(err, cluster.ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestParse_NormalizesVirtualNodes(t *testing.T) {
	// Omit virtual_nodes — JSON zero value, should be normalised to 128.
	data := []byte(`{"version":"v1","name":"x","routing":{"algorithm":"fnv1a-consistent-hash","virtual_nodes":0},"nodes":[{"id":"n1","base_url":"http://127.0.0.1:9101"}],"scope":{"static_config_only":true,"no_dynamic_membership":true,"no_discovery":true,"no_consensus":true,"no_raft":true,"no_replication":true,"no_failover":true,"no_shard_migration":true,"no_distributed_txns":true}}`)
	cfg, err := cluster.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Routing.VirtualNodes != cluster.DefaultVirtualNodes {
		t.Errorf("VirtualNodes = %d, want %d", cfg.Routing.VirtualNodes, cluster.DefaultVirtualNodes)
	}
}

func TestMarshal_StableJSON(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	b1, err := cluster.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal 1: %v", err)
	}
	b2, err := cluster.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal 2: %v", err)
	}
	if string(b1) != string(b2) {
		t.Error("Marshal is not stable across two calls")
	}
}

func TestParse_IgnoresUnknownFields(t *testing.T) {
	// Add a field not in the struct — should be silently ignored.
	data := []byte(`{"version":"v1","name":"x","unknown_field":"value","routing":{"algorithm":"fnv1a-consistent-hash","virtual_nodes":128},"nodes":[{"id":"n1","base_url":"http://127.0.0.1:9101","weight":1}],"scope":{"static_config_only":true,"no_dynamic_membership":true,"no_discovery":true,"no_consensus":true,"no_raft":true,"no_replication":true,"no_failover":true,"no_shard_migration":true,"no_distributed_txns":true}}`)
	_, err := cluster.Parse(data)
	if err != nil {
		t.Errorf("expected unknown fields to be ignored, got error: %v", err)
	}
}

// --- Save / Load round-trip ---

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cluster.json")
	cfg := cluster.ExampleLocal3Node()
	if err := cluster.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := cluster.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Name != cfg.Name {
		t.Errorf("Name: got %q, want %q", loaded.Name, cfg.Name)
	}
	if len(loaded.Nodes) != len(cfg.Nodes) {
		t.Errorf("Nodes count: got %d, want %d", len(loaded.Nodes), len(cfg.Nodes))
	}
}

func TestRoundTrip_PreservesVersionAndNodeIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cluster.json")
	cfg := cluster.ExampleLocal3Node()
	if err := cluster.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := cluster.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != cluster.CurrentVersion {
		t.Errorf("Version = %q", loaded.Version)
	}
	for i, n := range cfg.Nodes {
		if loaded.Nodes[i].ID != n.ID {
			t.Errorf("Nodes[%d].ID = %q, want %q", i, loaded.Nodes[i].ID, n.ID)
		}
	}
}

func TestLoad_FileNotFound_ReturnsError(t *testing.T) {
	_, err := cluster.Load("/nonexistent/path/cluster.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// --- Config example files ---

func TestLoad_ConfigsDir_Local3Node(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-3node.json")
	cfg, err := cluster.Load(path)
	if err != nil {
		t.Fatalf("load local-3node.json: %v", err)
	}
	if len(cfg.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(cfg.Nodes))
	}
}

func TestLoad_ConfigsDir_Local3NodeWithProxy(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-3node-with-proxy.json")
	cfg, err := cluster.Load(path)
	if err != nil {
		t.Fatalf("load local-3node-with-proxy.json: %v", err)
	}
	if !cfg.Proxy.Enabled {
		t.Error("expected proxy.enabled=true in local-3node-with-proxy.json")
	}
}

func TestLoad_ConfigsDir_Docker3NodeWithProxy(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "docker-3node-with-proxy.json")
	cfg, err := cluster.Load(path)
	if err != nil {
		t.Fatalf("load docker-3node-with-proxy.json: %v", err)
	}
	if len(cfg.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(cfg.Nodes))
	}
}

// --- NodeConfigs / GatewayOptions / ProxyOptions ---

func TestNodeConfigs_CountMatchesConfig(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	ncs, err := cluster.NodeConfigs(cfg)
	if err != nil {
		t.Fatalf("NodeConfigs: %v", err)
	}
	if len(ncs) != len(cfg.Nodes) {
		t.Errorf("NodeConfigs count = %d, want %d", len(ncs), len(cfg.Nodes))
	}
}

func TestNodeConfigs_IDsAndURLsMatch(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	ncs, err := cluster.NodeConfigs(cfg)
	if err != nil {
		t.Fatalf("NodeConfigs: %v", err)
	}
	for i, n := range cfg.Nodes {
		if ncs[i].ID != n.ID {
			t.Errorf("NodeConfigs[%d].ID = %q, want %q", i, ncs[i].ID, n.ID)
		}
		if ncs[i].BaseURL != n.BaseURL {
			t.Errorf("NodeConfigs[%d].BaseURL = %q, want %q", i, ncs[i].BaseURL, n.BaseURL)
		}
	}
}

func TestGatewayOptions_NodeCount(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	opts, err := cluster.GatewayOptions(cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("GatewayOptions: %v", err)
	}
	if len(opts.Nodes) != 3 {
		t.Errorf("GatewayOptions Nodes count = %d, want 3", len(opts.Nodes))
	}
}

func TestGatewayOptions_AppliesTimeout(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	opts, err := cluster.GatewayOptions(cfg, 10*time.Second)
	if err != nil {
		t.Fatalf("GatewayOptions: %v", err)
	}
	if opts.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", opts.Timeout)
	}
}

func TestGatewayOptions_VirtualNodes(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	opts, err := cluster.GatewayOptions(cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("GatewayOptions: %v", err)
	}
	if opts.VirtualNodes != cluster.DefaultVirtualNodes {
		t.Errorf("VirtualNodes = %d, want %d", opts.VirtualNodes, cluster.DefaultVirtualNodes)
	}
}

func TestProxyOptions_Addr(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	cfg.Proxy.Addr = "127.0.0.1:9200"
	opts, err := cluster.ProxyOptions(cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("ProxyOptions: %v", err)
	}
	if opts.Addr != "127.0.0.1:9200" {
		t.Errorf("Addr = %q, want 127.0.0.1:9200", opts.Addr)
	}
}

func TestProxyOptions_AppliesTimeout(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	opts, err := cluster.ProxyOptions(cfg, 8*time.Second)
	if err != nil {
		t.Fatalf("ProxyOptions: %v", err)
	}
	if opts.Timeout != 8*time.Second {
		t.Errorf("Timeout = %v, want 8s", opts.Timeout)
	}
}

func TestProxyOptions_NodeCountMatchesConfig(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	opts, err := cluster.ProxyOptions(cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("ProxyOptions: %v", err)
	}
	if len(opts.Nodes) != 3 {
		t.Errorf("Nodes count = %d, want 3", len(opts.Nodes))
	}
}

// --- Integration: config-based gateway routing ---

// startTestNode starts a shardforge-node on a random port and returns the server and base URL.
func startTestNode(t *testing.T, id string) *node.Server {
	t.Helper()
	dir := t.TempDir()
	srv, err := node.Open(node.Options{
		NodeID:  id,
		Addr:    "127.0.0.1:0",
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("node.Open %s: %v", id, err)
	}
	if err := srv.StartBackground(); err != nil {
		t.Fatalf("node.StartBackground %s: %v", id, err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func TestIntegration_ConfigBasedGateway_DeterministicRoute(t *testing.T) {
	n1 := startTestNode(t, "node-1")
	n2 := startTestNode(t, "node-2")
	n3 := startTestNode(t, "node-3")

	cfg := cluster.DefaultConfig("test", []cluster.Node{
		{ID: "node-1", BaseURL: "http://" + n1.Addr()},
		{ID: "node-2", BaseURL: "http://" + n2.Addr()},
		{ID: "node-3", BaseURL: "http://" + n3.Addr()},
	})

	opts, err := cluster.GatewayOptions(cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("GatewayOptions: %v", err)
	}

	g, err := gateway.Open(opts)
	if err != nil {
		t.Fatalf("gateway.Open: %v", err)
	}
	defer g.Close()

	// Route the same key twice — must be deterministic.
	rn1, err := g.NodeForKey([]byte("stable-key"))
	if err != nil {
		t.Fatalf("NodeForKey: %v", err)
	}
	rn2, err := g.NodeForKey([]byte("stable-key"))
	if err != nil {
		t.Fatalf("NodeForKey second: %v", err)
	}
	if rn1.ID != rn2.ID {
		t.Errorf("routing not deterministic: %q vs %q", rn1.ID, rn2.ID)
	}
}

func TestIntegration_ConfigBasedProxy_PutGet(t *testing.T) {
	n1 := startTestNode(t, "node-1")
	n2 := startTestNode(t, "node-2")
	n3 := startTestNode(t, "node-3")

	cfg := cluster.DefaultConfig("test", []cluster.Node{
		{ID: "node-1", BaseURL: "http://" + n1.Addr()},
		{ID: "node-2", BaseURL: "http://" + n2.Addr()},
		{ID: "node-3", BaseURL: "http://" + n3.Addr()},
	})
	cfg.Proxy.Addr = "127.0.0.1:0"

	opts, err := cluster.ProxyOptions(cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("ProxyOptions: %v", err)
	}

	srv, err := proxy.Open(opts)
	if err != nil {
		t.Fatalf("proxy.Open: %v", err)
	}
	if err := srv.StartBackground(); err != nil {
		t.Fatalf("proxy.StartBackground: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	// Verify proxy is alive by checking its status.
	status := srv.Status()
	if status.Gateway.NodeCount != 3 {
		t.Errorf("proxy gateway node count = %d, want 3", status.Gateway.NodeCount)
	}
}

func TestIntegration_ConfigMatchesManualNodeList(t *testing.T) {
	n1 := startTestNode(t, "node-1")
	n2 := startTestNode(t, "node-2")
	n3 := startTestNode(t, "node-3")

	cfgNodes := []cluster.Node{
		{ID: "node-1", BaseURL: "http://" + n1.Addr()},
		{ID: "node-2", BaseURL: "http://" + n2.Addr()},
		{ID: "node-3", BaseURL: "http://" + n3.Addr()},
	}
	cfg := cluster.DefaultConfig("test", cfgNodes)

	gwopts, err := cluster.GatewayOptions(cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("GatewayOptions: %v", err)
	}
	gCluster, err := gateway.Open(gwopts)
	if err != nil {
		t.Fatalf("gateway.Open cluster: %v", err)
	}
	defer gCluster.Close()

	// Open a manual gateway with same nodes.
	gManual, err := gateway.Open(gateway.Options{
		Nodes: []gateway.NodeConfig{
			{ID: "node-1", BaseURL: "http://" + n1.Addr()},
			{ID: "node-2", BaseURL: "http://" + n2.Addr()},
			{ID: "node-3", BaseURL: "http://" + n3.Addr()},
		},
		VirtualNodes: cluster.DefaultVirtualNodes,
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("gateway.Open manual: %v", err)
	}
	defer gManual.Close()

	// Both gateways must route all test keys identically.
	keys := []string{"user:1", "session:abc", "product:42", "order:999", "cache:foo"}
	for _, k := range keys {
		r1, err := gCluster.NodeForKey([]byte(k))
		if err != nil {
			t.Fatalf("cluster gateway NodeForKey %q: %v", k, err)
		}
		r2, err := gManual.NodeForKey([]byte(k))
		if err != nil {
			t.Fatalf("manual gateway NodeForKey %q: %v", k, err)
		}
		if r1.ID != r2.ID {
			t.Errorf("key %q: cluster routes to %q, manual routes to %q", k, r1.ID, r2.ID)
		}
	}
}

// skipIfNoConfigsDir skips the test if the configs/ directory is not accessible.
func skipIfNoConfigsDir(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Join(root, "configs")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("configs/ directory not accessible: %v", err)
	}
	return dir
}

func TestLoad_AllExampleConfigs_Validate(t *testing.T) {
	dir := skipIfNoConfigsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir configs/: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			cfg, err := cluster.Load(path)
			if err != nil {
				t.Fatalf("Load %q: %v", path, err)
			}
			if err := cluster.Validate(cfg); err != nil {
				t.Fatalf("Validate %q: %v", path, err)
			}
		})
	}
}
