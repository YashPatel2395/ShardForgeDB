package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/engine"
	"github.com/YashPatel2395/ShardForgeDB/internal/replica"
	"github.com/YashPatel2395/ShardForgeDB/internal/shard"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func openTempEngine(t *testing.T) *engine.Engine {
	t.Helper()
	dir := t.TempDir()
	eng, err := engine.Open(engine.Options{Dir: dir})
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func openTempShard(t *testing.T, count int) *shard.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := shard.Open(shard.Options{Dir: dir, ShardCount: count})
	if err != nil {
		t.Fatalf("shard.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func openTempReplica(t *testing.T, count int) *replica.Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "dashboard-replica-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	st, err := replica.Open(replica.Options{Dir: dir, ReplicaCount: count})
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("replica.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		os.RemoveAll(dir)
	})
	return st
}

type staticCollector struct {
	snap Snapshot
}

func (s *staticCollector) Snapshot() Snapshot { return s.snap }

func staticSnap(title, componentName string, typ ComponentType) Snapshot {
	return Snapshot{
		GeneratedAt: time.Now(),
		Title:       title,
		Components: []ComponentSnapshot{
			{Name: componentName, Type: typ, Health: HealthOK, Stats: map[string]any{"k": "v"}},
		},
		Events: []TimelineEvent{
			{Time: time.Now(), Source: "test", Action: "init", Message: "hello"},
		},
	}
}

func newTestServer(t *testing.T, col Collector) *Server {
	t.Helper()
	srv, err := NewServer(Options{}, col)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func getBody(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// ── NewServer tests ───────────────────────────────────────────────────────────

// Test 1: NewServer rejects nil collector.
func TestNewServer_NilCollector(t *testing.T) {
	_, err := NewServer(Options{}, nil)
	if err == nil {
		t.Fatal("expected error for nil collector")
	}
}

// Test 2: NewServer defaults Addr and Title.
func TestNewServer_Defaults(t *testing.T) {
	col := &staticCollector{snap: staticSnap("t", "c", ComponentEngine)}
	srv, err := NewServer(Options{}, col)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if srv.opts.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr default = %q, want 127.0.0.1:8080", srv.opts.Addr)
	}
	if srv.opts.Title != "ShardForgeDB Dashboard" {
		t.Errorf("Title default = %q, want ShardForgeDB Dashboard", srv.opts.Title)
	}
}

// ── Handler endpoint tests ────────────────────────────────────────────────────

// Test 3: /healthz returns JSON {"status":"ok"}.
func TestHandler_Healthz(t *testing.T) {
	col := &staticCollector{snap: staticSnap("t", "c", ComponentEngine)}
	srv := newTestServer(t, col)
	code, body := getBody(t, srv.Handler(), "/healthz")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["status"] != "ok" {
		t.Errorf("status = %q, want ok", m["status"])
	}
}

// Test 4: /status returns JSON Snapshot.
func TestHandler_Status(t *testing.T) {
	snap := staticSnap("MyTitle", "MyComp", ComponentShard)
	col := &staticCollector{snap: snap}
	srv := newTestServer(t, col)
	code, body := getBody(t, srv.Handler(), "/status")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	var s Snapshot
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Title != "MyTitle" {
		t.Errorf("Title = %q, want MyTitle", s.Title)
	}
	if len(s.Components) != 1 {
		t.Errorf("len(Components) = %d, want 1", len(s.Components))
	}
	if s.Components[0].Name != "MyComp" {
		t.Errorf("Component.Name = %q, want MyComp", s.Components[0].Name)
	}
}

// Test 5: /events returns JSON array of timeline events.
func TestHandler_Events(t *testing.T) {
	snap := staticSnap("t", "c", ComponentReplica)
	col := &staticCollector{snap: snap}
	srv := newTestServer(t, col)
	code, body := getBody(t, srv.Handler(), "/events")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	var events []TimelineEvent
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("len(events) = %d, want 1", len(events))
	}
	if events[0].Source != "test" {
		t.Errorf("Source = %q, want test", events[0].Source)
	}
}

// Test 5b: /events returns empty JSON array when no events.
func TestHandler_Events_Empty(t *testing.T) {
	snap := Snapshot{GeneratedAt: time.Now(), Title: "t"}
	col := &staticCollector{snap: snap}
	srv := newTestServer(t, col)
	code, body := getBody(t, srv.Handler(), "/events")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !strings.Contains(body, "[]") {
		t.Errorf("empty events should return [], got %q", body)
	}
}

// Test 6: / returns HTML with title and component name.
func TestHandler_Root_HTML(t *testing.T) {
	snap := staticSnap("DashTitle", "EngineComp", ComponentEngine)
	col := &staticCollector{snap: snap}
	srv := newTestServer(t, col)
	code, body := getBody(t, srv.Handler(), "/")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !strings.Contains(body, "DashTitle") {
		t.Errorf("HTML missing title DashTitle")
	}
	if !strings.Contains(body, "EngineComp") {
		t.Errorf("HTML missing component name EngineComp")
	}
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("response not HTML")
	}
}

// Test 7: unknown route returns 404.
func TestHandler_Unknown_404(t *testing.T) {
	col := &staticCollector{snap: staticSnap("t", "c", ComponentEngine)}
	srv := newTestServer(t, col)
	code, _ := getBody(t, srv.Handler(), "/not-a-real-path")
	if code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

// ── Collector tests ───────────────────────────────────────────────────────────

// Test 8: EngineCollector returns engine stats.
func TestEngineCollector_Stats(t *testing.T) {
	eng := openTempEngine(t)
	_ = eng.Put([]byte("k"), []byte("v"))
	col := NewEngineCollector("test-engine", eng)
	snap := col.Snapshot()
	if len(snap.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(snap.Components))
	}
	c := snap.Components[0]
	if c.Type != ComponentEngine {
		t.Errorf("Type = %q, want engine", c.Type)
	}
	if c.Health != HealthOK {
		t.Errorf("Health = %q, want ok", c.Health)
	}
	if _, ok := c.Stats["memtable_entries"]; !ok {
		t.Error("missing memtable_entries stat")
	}
	if _, ok := c.Stats["sstable_count"]; !ok {
		t.Error("missing sstable_count stat")
	}
}

// Test 9: ShardCollector returns shard stats.
func TestShardCollector_Stats(t *testing.T) {
	st := openTempShard(t, 2)
	col := NewShardCollector("test-shard", st)
	snap := col.Snapshot()
	if len(snap.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(snap.Components))
	}
	c := snap.Components[0]
	if c.Type != ComponentShard {
		t.Errorf("Type = %q, want shard", c.Type)
	}
	if _, ok := c.Stats["shard_count"]; !ok {
		t.Error("missing shard_count stat")
	}
}

// Test 10: ReplicaCollector returns replica stats.
func TestReplicaCollector_Stats(t *testing.T) {
	st := openTempReplica(t, 2)
	col := NewReplicaCollector("test-replica", st)
	snap := col.Snapshot()
	if len(snap.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(snap.Components))
	}
	c := snap.Components[0]
	if c.Type != ComponentReplica {
		t.Errorf("Type = %q, want replica", c.Type)
	}
	if _, ok := c.Stats["replica_count"]; !ok {
		t.Error("missing replica_count stat")
	}
	if _, ok := c.Stats["commit_index"]; !ok {
		t.Error("missing commit_index stat")
	}
}

// Test 11: MultiCollector merges components.
func TestMultiCollector_MergesComponents(t *testing.T) {
	c1 := &staticCollector{snap: staticSnap("t1", "Comp1", ComponentEngine)}
	c2 := &staticCollector{snap: staticSnap("t2", "Comp2", ComponentShard)}
	multi := NewMultiCollector("merged", c1, c2)
	snap := multi.Snapshot()
	if len(snap.Components) != 2 {
		t.Fatalf("len(Components) = %d, want 2", len(snap.Components))
	}
	names := map[string]bool{snap.Components[0].Name: true, snap.Components[1].Name: true}
	if !names["Comp1"] || !names["Comp2"] {
		t.Errorf("components missing: %v", names)
	}
}

// Test 12: MultiCollector merges events.
func TestMultiCollector_MergesEvents(t *testing.T) {
	c1 := &staticCollector{snap: staticSnap("t1", "c1", ComponentEngine)}
	c2 := &staticCollector{snap: staticSnap("t2", "c2", ComponentShard)}
	multi := NewMultiCollector("merged", c1, c2)
	snap := multi.Snapshot()
	// Each staticSnap adds 1 event; multi should have 2.
	if len(snap.Events) != 2 {
		t.Errorf("len(Events) = %d, want 2", len(snap.Events))
	}
}

// Test 13: HTML escapes component names.
func TestHTML_EscapesComponentName(t *testing.T) {
	malicious := `<script>alert(1)</script>`
	snap := Snapshot{
		GeneratedAt: time.Now(),
		Title:       "Test",
		Components: []ComponentSnapshot{
			{Name: malicious, Type: ComponentEngine, Health: HealthOK, Stats: map[string]any{}},
		},
	}
	html, err := renderHTML(snap)
	if err != nil {
		t.Fatalf("renderHTML: %v", err)
	}
	if strings.Contains(string(html), "<script>alert(1)</script>") {
		t.Error("HTML template did not escape component name")
	}
}

// Test 14: Snapshot timestamps are non-zero.
func TestSnapshot_TimestampsNonZero(t *testing.T) {
	eng := openTempEngine(t)
	col := NewEngineCollector("e", eng)
	snap := col.Snapshot()
	if snap.GeneratedAt.IsZero() {
		t.Error("GeneratedAt is zero")
	}
}

// Test 15: Health is ok for normal components.
func TestHealth_OKForNormalComponents(t *testing.T) {
	eng := openTempEngine(t)
	col := NewEngineCollector("e", eng)
	snap := col.Snapshot()
	if snap.Components[0].Health != HealthOK {
		t.Errorf("Health = %q, want ok", snap.Components[0].Health)
	}
}

// Test 31: No external assets required (HTML self-contained).
func TestHTML_SelfContained(t *testing.T) {
	snap := staticSnap("T", "C", ComponentEngine)
	html, err := renderHTML(snap)
	if err != nil {
		t.Fatalf("renderHTML: %v", err)
	}
	body := string(html)
	// No external script/link tags.
	if strings.Contains(body, `src="http`) || strings.Contains(body, `href="http`) {
		t.Error("HTML references external resources")
	}
}

// Test 32: Footer states local-only / no Raft / no consensus.
func TestHTML_Footer(t *testing.T) {
	snap := staticSnap("T", "C", ComponentEngine)
	html, err := renderHTML(snap)
	if err != nil {
		t.Fatalf("renderHTML: %v", err)
	}
	body := string(html)
	for _, phrase := range []string{"no networking", "no Raft", "no consensus", "no distributed cluster"} {
		if !strings.Contains(body, phrase) {
			t.Errorf("footer missing phrase %q", phrase)
		}
	}
}

// ── Server Start/Close tests ──────────────────────────────────────────────────

// Test 24: Server Start/Close works on 127.0.0.1:0.
func TestServer_StartClose(t *testing.T) {
	col := &staticCollector{snap: staticSnap("t", "c", ComponentEngine)}
	srv, err := NewServer(Options{Addr: "127.0.0.1:0"}, col)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	started := make(chan struct{})
	go func() {
		close(started)
		_ = srv.Start()
	}()
	<-started
	// Give the goroutine time to bind.
	time.Sleep(20 * time.Millisecond)
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// Test 25: Server Close is idempotent.
func TestServer_Close_Idempotent(t *testing.T) {
	col := &staticCollector{snap: staticSnap("t", "c", ComponentEngine)}
	srv, err := NewServer(Options{Addr: "127.0.0.1:0"}, col)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// Test 26: Concurrent /status requests are race-safe.
func TestHandler_ConcurrentStatus_RaceSafe(t *testing.T) {
	eng := openTempEngine(t)
	col := NewEngineCollector("e", eng)
	srv := newTestServer(t, col)
	h := srv.Handler()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/status", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
		}()
	}
	wg.Wait()
}

// Test 27: Concurrent snapshot collection is race-safe.
func TestCollector_ConcurrentSnapshot_RaceSafe(t *testing.T) {
	st := openTempReplica(t, 3)
	_, _ = st.Put([]byte("k"), []byte("v"))
	col := NewReplicaCollector("r", st)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = col.Snapshot()
		}()
	}
	wg.Wait()
}

// Test 30: JSON output is deterministic enough for tests.
func TestJSON_Determinism(t *testing.T) {
	snap := Snapshot{
		GeneratedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:       "Deterministic",
		Components: []ComponentSnapshot{
			{Name: "C", Type: ComponentEngine, Health: HealthOK, Stats: map[string]any{"x": 42}},
		},
	}
	col := &staticCollector{snap: snap}
	srv := newTestServer(t, col)

	code1, body1 := getBody(t, srv.Handler(), "/status")
	code2, body2 := getBody(t, srv.Handler(), "/status")
	if code1 != http.StatusOK || code2 != http.StatusOK {
		t.Fatalf("status: %d %d", code1, code2)
	}
	// Title and component name must be identical.
	var s1, s2 map[string]any
	_ = json.Unmarshal([]byte(body1), &s1)
	_ = json.Unmarshal([]byte(body2), &s2)
	if s1["title"] != s2["title"] {
		t.Errorf("title mismatch: %v vs %v", s1["title"], s2["title"])
	}
}

// Test: MultiCollector title is used.
func TestMultiCollector_Title(t *testing.T) {
	c1 := &staticCollector{snap: staticSnap("t1", "c1", ComponentEngine)}
	multi := NewMultiCollector("MyMergedTitle", c1)
	snap := multi.Snapshot()
	if snap.Title != "MyMergedTitle" {
		t.Errorf("Title = %q, want MyMergedTitle", snap.Title)
	}
}

// Test: /status ComponentType serialisation.
func TestHandler_Status_ComponentType(t *testing.T) {
	snap := staticSnap("T", "C", ComponentReplica)
	col := &staticCollector{snap: snap}
	srv := newTestServer(t, col)
	_, body := getBody(t, srv.Handler(), "/status")
	if !strings.Contains(body, `"replica"`) && !strings.Contains(body, "replica") {
		t.Errorf("status body missing component type 'replica': %s", body)
	}
}

// Test: ReplicaCollector degraded health when follower lags.
func TestReplicaCollector_DegradedHealth(t *testing.T) {
	st := openTempReplica(t, 2)
	// Write a key (commits to leader).
	_, _ = st.Put([]byte("k"), []byte("v"))
	// Don't replicate — follower is behind.
	col := NewReplicaCollector("r", st)
	snap := col.Snapshot()
	if snap.Components[0].Health != HealthDegraded {
		t.Errorf("Health = %q, want degraded when follower lags", snap.Components[0].Health)
	}
}

// Test: ScenarioCollector surfaces events.
func TestScenarioCollector_Events(t *testing.T) {
	result := ScenarioResult{
		Name:   "TestScenario",
		Status: ScenarioPassed,
		Events: []TimelineEvent{
			{Time: time.Now(), Source: "s", Action: "a", Message: "m"},
		},
	}
	col := NewScenarioCollector(result)
	snap := col.Snapshot()
	if len(snap.Events) != 1 {
		t.Errorf("len(Events) = %d, want 1", len(snap.Events))
	}
	if snap.Events[0].Source != "s" {
		t.Errorf("Source = %q, want s", snap.Events[0].Source)
	}
}

// Test: Dashboard can render scenario events (Test 23).
func TestDashboard_RenderScenarioEvents(t *testing.T) {
	result := ScenarioResult{
		Name:   "PauseScenario",
		Status: ScenarioPassed,
		Events: []TimelineEvent{
			{Time: time.Now(), Source: "chaos", Action: "pause", Message: "follower paused"},
		},
	}
	col := NewScenarioCollector(result)
	snap := col.Snapshot()
	html, err := renderHTML(snap)
	if err != nil {
		t.Fatalf("renderHTML: %v", err)
	}
	if !strings.Contains(string(html), "chaos") {
		t.Error("HTML missing scenario event source 'chaos'")
	}
}

// BenchmarkSnapshot_EngineCollector measures Snapshot() throughput.
func BenchmarkSnapshot_EngineCollector(b *testing.B) {
	dir := b.TempDir()
	eng, err := engine.Open(engine.Options{Dir: dir})
	if err != nil {
		b.Fatalf("engine.Open: %v", err)
	}
	defer eng.Close()
	for i := 0; i < 100; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("k%d", i)), []byte("v"))
	}
	col := NewEngineCollector("bench-engine", eng)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = col.Snapshot()
	}
}

// BenchmarkSnapshot_ReplicaCollector measures Snapshot() on replica store.
func BenchmarkSnapshot_ReplicaCollector(b *testing.B) {
	dir, err := os.MkdirTemp("", "bench-replica-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)
	st, err := replica.Open(replica.Options{Dir: dir, ReplicaCount: 3})
	if err != nil {
		b.Fatalf("replica.Open: %v", err)
	}
	defer st.Close()
	for i := 0; i < 50; i++ {
		_, _ = st.Put([]byte(fmt.Sprintf("k%d", i)), []byte("v"))
	}
	_ = st.ReplicateAll()
	col := NewReplicaCollector("bench-replica", st)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = col.Snapshot()
	}
}

// BenchmarkSnapshot_MultiCollector measures multi-collector snapshot.
func BenchmarkSnapshot_MultiCollector(b *testing.B) {
	dir1 := b.TempDir()
	e1, _ := engine.Open(engine.Options{Dir: dir1})
	defer e1.Close()
	dir2 := b.TempDir()
	e2, _ := engine.Open(engine.Options{Dir: dir2})
	defer e2.Close()
	dir3 := b.TempDir()
	e3, _ := engine.Open(engine.Options{Dir: dir3})
	defer e3.Close()

	multi := NewMultiCollector("bench",
		NewEngineCollector("e1", e1),
		NewEngineCollector("e2", e2),
		NewEngineCollector("e3", e3),
	)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = multi.Snapshot()
	}
}

// BenchmarkRenderHTML measures HTML template rendering.
func BenchmarkRenderHTML(b *testing.B) {
	snap := Snapshot{
		GeneratedAt: time.Now(),
		Title:       "Bench Dashboard",
		Components: []ComponentSnapshot{
			{Name: "engine-1", Type: ComponentEngine, Health: HealthOK,
				Stats: map[string]any{"memtable_entries": 100, "sstable_count": 5}},
			{Name: "shard-store", Type: ComponentShard, Health: HealthOK,
				Stats: map[string]any{"shard_count": 4, "total_sstable_count": 20}},
		},
		Events: []TimelineEvent{
			{Time: time.Now(), Source: "bench", Action: "init", Message: "started"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = renderHTML(snap)
	}
}

// BenchmarkEncodeStatusJSON measures /status JSON encoding.
func BenchmarkEncodeStatusJSON(b *testing.B) {
	dir := b.TempDir()
	eng, _ := engine.Open(engine.Options{Dir: dir})
	defer eng.Close()
	col := NewEngineCollector("e", eng)
	srv, _ := NewServer(Options{}, col)
	h := srv.Handler()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}
