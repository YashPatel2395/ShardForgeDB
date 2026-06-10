// Package ops implements an operations and simulation layer for ShardForgeDB.
//
// Scope:
//   - Node health visibility via HTTP /healthz polling.
//   - Failure simulation: route keys on a subset of nodes to show impact.
//   - Manual rebalance planning: show key movement when nodes are added/removed.
//   - All functions are pure and stateless. No background goroutines.
//   - No automatic failover. No automatic rebalancing. No data movement.
//   - No shard migration. No Raft. No consensus. No quorum. No leader election.
//   - No dynamic membership. No service discovery. No gossip.
//   - Manual operator action is required for any real cluster change.
package ops

import "errors"

// ErrInvalidSimulation is returned when a simulation request is malformed.
var ErrInvalidSimulation = errors.New("ops: invalid simulation")

// ErrUnknownNode is returned when a node ID is not found in the cluster config.
var ErrUnknownNode = errors.New("ops: unknown node")

// ErrNoHealthyNodes is returned when no healthy/available nodes remain after filtering.
var ErrNoHealthyNodes = errors.New("ops: no healthy nodes")
