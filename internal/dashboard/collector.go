package dashboard

import (
	"fmt"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/engine"
	"github.com/YashPatel2395/ShardForgeDB/internal/replica"
	"github.com/YashPatel2395/ShardForgeDB/internal/shard"
)

// ── Engine collector ──────────────────────────────────────────────────────────

type engineCollector struct {
	name string
	eng  *engine.Engine
}

// NewEngineCollector returns a Collector that reads stats from a local Engine.
// It does not mutate the engine.
func NewEngineCollector(name string, eng *engine.Engine) Collector {
	return &engineCollector{name: name, eng: eng}
}

func (c *engineCollector) Snapshot() Snapshot {
	st := c.eng.Stats()
	stats := map[string]any{
		"memtable_entries":               st.MemTableEntries,
		"memtable_approx_bytes":          st.MemTableApproxBytes,
		"sstable_count":                  st.SSTableCount,
		"next_seq":                       st.NextSeq,
		"flush_count":                    st.FlushCount,
		"bloom_checks":                   st.BloomChecks,
		"bloom_negative_skips":           st.BloomNegativeSkips,
		"compaction_count":               st.CompactionCount,
		"last_compaction_input_tables":   st.LastCompactionInputTables,
		"last_compaction_output_entries": st.LastCompactionOutputEntries,
	}
	return Snapshot{
		GeneratedAt: time.Now(),
		Title:       c.name,
		Components: []ComponentSnapshot{
			{
				Name:   c.name,
				Type:   ComponentEngine,
				Health: HealthOK,
				Stats:  stats,
			},
		},
	}
}

// ── Shard collector ───────────────────────────────────────────────────────────

type shardCollector struct {
	name  string
	store *shard.Store
}

// NewShardCollector returns a Collector that reads stats from a local shard
// Store. It does not mutate the store.
func NewShardCollector(name string, store *shard.Store) Collector {
	return &shardCollector{name: name, store: store}
}

func (c *shardCollector) Snapshot() Snapshot {
	st := c.store.Stats()
	stats := map[string]any{
		"shard_count":                 st.ShardCount,
		"virtual_nodes":               st.VirtualNodes,
		"total_memtable_entries":      st.TotalMemTableEntries,
		"total_memtable_approx_bytes": st.TotalMemTableApproxBytes,
		"total_sstable_count":         st.TotalSSTableCount,
		"total_flush_count":           st.TotalFlushCount,
		"total_compaction_count":      st.TotalCompactionCount,
	}
	for i, sh := range st.Shards {
		pfx := fmt.Sprintf("shard_%d_", i)
		stats[pfx+"name"] = sh.Name
		stats[pfx+"memtable_entries"] = sh.MemTableEntries
		stats[pfx+"sstable_count"] = sh.SSTableCount
		stats[pfx+"flush_count"] = sh.FlushCount
	}
	return Snapshot{
		GeneratedAt: time.Now(),
		Title:       c.name,
		Components: []ComponentSnapshot{
			{
				Name:   c.name,
				Type:   ComponentShard,
				Health: HealthOK,
				Stats:  stats,
			},
		},
	}
}

// ── Replica collector ─────────────────────────────────────────────────────────

type replicaCollector struct {
	name  string
	store *replica.Store
}

// NewReplicaCollector returns a Collector that reads stats from a local
// replica Store. It does not mutate the store.
func NewReplicaCollector(name string, store *replica.Store) Collector {
	return &replicaCollector{name: name, store: store}
}

func (c *replicaCollector) Snapshot() Snapshot {
	st := c.store.Stats()
	stats := map[string]any{
		"replica_count": st.ReplicaCount,
		"leader_id":     st.LeaderID,
		"commit_index":  uint64(st.CommitIndex),
		"applied_index": uint64(st.AppliedIndex),
	}
	health := HealthOK
	for _, r := range st.Replicas {
		pfx := fmt.Sprintf("replica_%d_", r.ID)
		stats[pfx+"name"] = r.Name
		stats[pfx+"role"] = string(r.Role)
		stats[pfx+"applied_index"] = uint64(r.AppliedIndex)
		stats[pfx+"sstable_count"] = r.EngineSSTableCount
		stats[pfx+"flush_count"] = r.EngineFlushCount
		// If any follower is behind the commit index, mark degraded.
		if r.Role == replica.RoleFollower && r.AppliedIndex < st.CommitIndex {
			health = HealthDegraded
		}
	}
	return Snapshot{
		GeneratedAt: time.Now(),
		Title:       c.name,
		Components: []ComponentSnapshot{
			{
				Name:   c.name,
				Type:   ComponentReplica,
				Health: health,
				Stats:  stats,
			},
		},
	}
}

// ── Multi collector ───────────────────────────────────────────────────────────

type multiCollector struct {
	title      string
	collectors []Collector
}

// NewMultiCollector returns a Collector that merges snapshots from all
// provided collectors into a single Snapshot. The title is used for the
// merged snapshot; individual collector titles are ignored.
func NewMultiCollector(title string, collectors ...Collector) Collector {
	return &multiCollector{title: title, collectors: collectors}
}

func (c *multiCollector) Snapshot() Snapshot {
	merged := Snapshot{
		GeneratedAt: time.Now(),
		Title:       c.title,
	}
	for _, col := range c.collectors {
		s := col.Snapshot()
		merged.Components = append(merged.Components, s.Components...)
		merged.Events = append(merged.Events, s.Events...)
	}
	return merged
}

// ── Scenario collector ────────────────────────────────────────────────────────

// ScenarioCollector wraps a ScenarioResult so its events appear in the
// dashboard event timeline.
type ScenarioCollector struct {
	result ScenarioResult
}

// NewScenarioCollector returns a Collector that exposes the events from a
// completed ScenarioResult.
func NewScenarioCollector(result ScenarioResult) Collector {
	return &ScenarioCollector{result: result}
}

func (c *ScenarioCollector) Snapshot() Snapshot {
	health := HealthOK
	if c.result.Status == ScenarioFailed {
		health = HealthDegraded
	}
	stats := map[string]any{
		"scenario":    c.result.Name,
		"status":      string(c.result.Status),
		"step_count":  len(c.result.Steps),
		"event_count": len(c.result.Events),
		"error":       c.result.Error,
	}
	return Snapshot{
		GeneratedAt: time.Now(),
		Title:       c.result.Name,
		Components: []ComponentSnapshot{
			{
				Name:   c.result.Name,
				Type:   ComponentReplica,
				Health: health,
				Stats:  stats,
			},
		},
		Events: c.result.Events,
	}
}
