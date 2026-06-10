// Package gateway implements a client-side routing gateway for ShardForgeDB.
//
// Scope:
//   - Client-side consistent-hash routing over independent Phase 14 shardforge-node processes.
//   - Each Put/Get/Delete chooses the target node deterministically by key.
//   - Nodes do NOT coordinate. Nodes do NOT replicate. No Raft. No consensus.
//   - No quorum replication. No automatic leader election. No shard migration.
//   - No distributed transactions. No distributed vector search.
//   - Retrying to a different node is NOT supported: without replication a key
//     written to node-A cannot be found on node-B, so retrying would silently
//     miss data. Callers must handle node failures explicitly.
package gateway

import (
	"time"
)

// NodeConfig describes one node in the gateway ring.
type NodeConfig struct {
	// ID is a unique human-readable identifier for this node (e.g. "node-1").
	ID string `json:"id"`

	// BaseURL is the HTTP base URL of the shardforge-node process (e.g. "http://127.0.0.1:9101").
	BaseURL string `json:"base_url"`

	// Weight controls how many virtual ring points this node receives relative to others.
	// If Weight <= 0 it is treated as 1. Effective points = VirtualNodes * Weight.
	Weight int `json:"weight,omitempty"`
}

// Options configures a Gateway.
type Options struct {
	// Nodes is the list of node backends. Must be non-empty. IDs and BaseURLs must be unique.
	Nodes []NodeConfig `json:"nodes"`

	// VirtualNodes is the number of ring points per node (before weight scaling).
	// Default is 128 when zero.
	VirtualNodes int `json:"virtual_nodes,omitempty"`

	// Timeout is the per-request HTTP timeout for all node clients.
	// Default is 5 seconds when zero.
	Timeout time.Duration
}

// RoutedNode describes the node selected for a given key.
type RoutedNode struct {
	ID      string `json:"id"`
	BaseURL string `json:"base_url"`
}

// Stats is a snapshot of gateway ring configuration.
type Stats struct {
	NodeCount    int      `json:"node_count"`
	VirtualNodes int      `json:"virtual_nodes"`
	Nodes        []string `json:"nodes"`
}

// ForwardResult holds the result of forwarding a single request to a node.
type ForwardResult struct {
	// Body is the decoded JSON response body (nil on error).
	Body map[string]any
	// Err is non-nil when the request or decoding failed.
	Err error
}
