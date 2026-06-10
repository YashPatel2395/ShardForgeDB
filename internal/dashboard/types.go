// Package dashboard implements a local observability dashboard and
// chaos/failure simulation layer for ShardForgeDB components.
//
// # What this is
//
// A local, in-process HTTP dashboard that collects stats from Engine, Shard,
// and Replica stores, renders an HTML view, exposes JSON status endpoints, and
// runs deterministic local failure simulation scenarios.
//
// # What this is NOT
//
// This is NOT production monitoring. It is NOT distributed chaos testing. It
// is NOT networking between database nodes. It is NOT RPC. It is NOT Raft. It
// is NOT consensus. All components run inside a single OS process.
package dashboard

import (
	"time"
)

// ── ComponentType ────────────────────────────────────────────────────────────

// ComponentType identifies the kind of local component being observed.
type ComponentType string

const (
	ComponentEngine  ComponentType = "engine"
	ComponentShard   ComponentType = "shard"
	ComponentReplica ComponentType = "replica"
)

// ── HealthStatus ─────────────────────────────────────────────────────────────

// HealthStatus is the reported health of a local component.
type HealthStatus string

const (
	HealthOK       HealthStatus = "ok"
	HealthDegraded HealthStatus = "degraded"
	HealthFailed   HealthStatus = "failed"
)

// ── Options ───────────────────────────────────────────────────────────────────

// Options configures the dashboard Server.
type Options struct {
	// Addr is the TCP address to listen on. Default: "127.0.0.1:8080".
	Addr string

	// Title is the page title shown in the HTML dashboard.
	// Default: "ShardForgeDB Dashboard".
	Title string
}

func (o *Options) applyDefaults() {
	if o.Addr == "" {
		o.Addr = "127.0.0.1:8080"
	}
	if o.Title == "" {
		o.Title = "ShardForgeDB Dashboard"
	}
}

// ── ComponentSnapshot ────────────────────────────────────────────────────────

// ComponentSnapshot captures the state of one local component at a point in
// time. Stats values are arbitrary primitives (string, int, float64, bool).
type ComponentSnapshot struct {
	Name   string
	Type   ComponentType
	Health HealthStatus

	// Stats holds component-specific key-value statistics. Values must be
	// JSON-serialisable primitive types.
	Stats map[string]any
}

// ── TimelineEvent ─────────────────────────────────────────────────────────────

// TimelineEvent records one observable event that occurred during collection
// or a scenario run.
type TimelineEvent struct {
	Time    time.Time `json:"time"`
	Source  string    `json:"source"`
	Action  string    `json:"action"`
	Message string    `json:"message"`
}

// ── Snapshot ──────────────────────────────────────────────────────────────────

// Snapshot is a point-in-time view of all registered local components and
// recent events.
type Snapshot struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Title       string              `json:"title"`
	Components  []ComponentSnapshot `json:"components"`
	Events      []TimelineEvent     `json:"events"`
}

// ── Collector ─────────────────────────────────────────────────────────────────

// Collector produces a Snapshot of one or more local components. Collectors
// must not mutate the underlying stores — they only read stats.
type Collector interface {
	Snapshot() Snapshot
}

// ── ScenarioStatus ───────────────────────────────────────────────────────────

// ScenarioStatus is the lifecycle state of a chaos scenario run.
type ScenarioStatus string

const (
	ScenarioPending ScenarioStatus = "pending"
	ScenarioRunning ScenarioStatus = "running"
	ScenarioPassed  ScenarioStatus = "passed"
	ScenarioFailed  ScenarioStatus = "failed"
)

// ── ScenarioStep ─────────────────────────────────────────────────────────────

// ScenarioStep describes one step inside a scenario — what action was taken
// and what was expected.
type ScenarioStep struct {
	Name        string `json:"name"`
	Action      string `json:"action"`
	Expectation string `json:"expectation"`
}

// ── ScenarioResult ───────────────────────────────────────────────────────────

// ScenarioResult is the outcome of running one chaos/failure scenario.
type ScenarioResult struct {
	Name   string          `json:"name"`
	Status ScenarioStatus  `json:"status"`
	Steps  []ScenarioStep  `json:"steps"`
	Events []TimelineEvent `json:"events"`
	// Error is non-empty only when Status == ScenarioFailed.
	Error string `json:"error,omitempty"`
}
