// Package proxy implements a stateless HTTP routing proxy for ShardForgeDB.
//
// Scope:
//   - Stateless client-side routing only. The proxy holds no data.
//   - Every HTTP request is routed to exactly one independent shardforge-node process
//     using the Phase 15 internal/gateway consistent-hash ring.
//   - No Raft. No consensus. No quorum replication. No automatic leader election.
//   - No distributed sharding inside nodes. No networked replication.
//   - No automatic failover. No retry to another node.
//     If the routed node is unavailable, the operation fails with a clear error.
//   - No shard migration. No resharding. No distributed vector search.
//   - /scan-node/{nodeID} is a per-node scan only; there is no global distributed scan.
package proxy

import (
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
)

// Options configures a proxy Server.
type Options struct {
	// Addr is the TCP address to listen on (e.g. "127.0.0.1:9200").
	// Defaults to "127.0.0.1:9200" when empty.
	Addr string

	// Nodes is the list of backend node configurations. Must be non-empty.
	Nodes []gateway.NodeConfig

	// VirtualNodes is the number of ring points per node (before weight scaling).
	// Default is 128 when zero.
	VirtualNodes int

	// Timeout is the per-request HTTP timeout for calls to backend nodes.
	// Default is 5 seconds when zero.
	Timeout time.Duration
}

// Status is a point-in-time snapshot of the proxy server state.
type Status struct {
	Addr      string        `json:"addr"`
	StartedAt time.Time     `json:"started_at"`
	Gateway   gateway.Stats `json:"gateway"`
	Scope     Scope         `json:"scope"`
}

// Scope documents what this proxy does and explicitly does not do.
// All boolean fields are always true — they exist for honest self-documentation.
type Scope struct {
	ClientSideRoutingOnly bool `json:"client_side_routing_only"`
	NoReplication         bool `json:"no_replication"`
	NoRaft                bool `json:"no_raft"`
	NoConsensus           bool `json:"no_consensus"`
	NoFailover            bool `json:"no_failover"`
}

// RouteResponse is returned by GET /route/{key}.
type RouteResponse struct {
	Key     string `json:"key"`
	NodeID  string `json:"node_id"`
	BaseURL string `json:"base_url"`
}
