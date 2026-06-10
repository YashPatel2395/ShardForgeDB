// Package node implements a real networked database node for ShardForgeDB.
//
// Scope: Each node is an independent single-process key-value store backed by a local Engine.
// Nodes communicate over HTTP/JSON. This is NOT distributed consensus, NOT Raft,
// NOT quorum replication, NOT automatic leader election. It is a networked node runtime
// foundation — each node owns its own data directory and serves a simple key-value API.
package node

import (
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// NodeID identifies a node in the network. It is a human-readable string such as "node-1".
type NodeID string

// ReplicationOptions configures optional networked replication for a node.
// Leave zero-valued for standalone mode (no replication).
type ReplicationOptions struct {
	// Role is the replication role: replnet.RolePrimary or replnet.RoleFollower.
	// Empty means standalone (no replication).
	Role replnet.Role

	// PrimaryBaseURL is the HTTP base URL of the primary node.
	// Required when Role == replnet.RoleFollower; ignored otherwise.
	PrimaryBaseURL string
}

// Options configures a node Server.
type Options struct {
	// NodeID is a unique identifier for this node. Required.
	NodeID string

	// Addr is the TCP address the HTTP server listens on (e.g. "127.0.0.1:9101").
	// Required; must be non-empty.
	Addr string

	// DataDir is the directory where this node's Engine files are stored.
	// Required; will be created if it does not exist.
	DataDir string

	// WALSyncOnWrite enables fsync after every WAL append.
	// Increases durability at the cost of write throughput.
	WALSyncOnWrite bool

	// MemTableMaxBytes is the MemTable size threshold that triggers flush.
	// Zero means the engine default (64 MiB).
	MemTableMaxBytes uint64

	// Replication configures optional networked replication.
	// Zero value means standalone mode.
	Replication ReplicationOptions
}

// Status is returned by GET /status and the Server.Status() method.
type Status struct {
	NodeID      string               `json:"node_id"`
	Addr        string               `json:"addr"`
	DataDir     string               `json:"data_dir"`
	StartedAt   time.Time            `json:"started_at"`
	Engine      EngineStatus         `json:"engine"`
	Replication replnet.ReplicaStatus `json:"replication,omitempty"`
}

// EngineStatus holds a point-in-time snapshot of the local engine counters.
type EngineStatus struct {
	MemTableEntries     int    `json:"memtable_entries"`
	MemTableApproxBytes uint64 `json:"memtable_approx_bytes"`
	SSTableCount        int    `json:"sstable_count"`
	NextSeq             uint64 `json:"next_seq"`
	FlushCount          uint64 `json:"flush_count"`
	CompactionCount     uint64 `json:"compaction_count"`
}

// Entry is a key-value pair returned by GET /scan.
type Entry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// putRequest is the JSON body for PUT /kv/{key}.
type putRequest struct {
	Value string `json:"value"`
}

// putResponse is the JSON body returned by PUT /kv/{key}.
type putResponse struct {
	OK     bool   `json:"ok"`
	NodeID string `json:"node_id"`
}

// getResponse is the JSON body returned by GET /kv/{key}.
type getResponse struct {
	Found  bool   `json:"found"`
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	NodeID string `json:"node_id"`
}

// deleteResponse is the JSON body returned by DELETE /kv/{key}.
type deleteResponse struct {
	OK     bool   `json:"ok"`
	NodeID string `json:"node_id"`
}

// healthResponse is the JSON body returned by GET /healthz.
type healthResponse struct {
	Status string `json:"status"`
	NodeID string `json:"node_id"`
}

// opResponse is the JSON body returned by POST /flush and POST /compact.
type opResponse struct {
	OK     bool   `json:"ok"`
	NodeID string `json:"node_id"`
	Error  string `json:"error,omitempty"`
}

// scanResponse is the JSON body returned by GET /scan.
type scanResponse struct {
	NodeID  string  `json:"node_id"`
	Entries []Entry `json:"entries"`
}

// errorResponse is returned on any handler error.
type errorResponse struct {
	Error  string `json:"error"`
	NodeID string `json:"node_id,omitempty"`
}
