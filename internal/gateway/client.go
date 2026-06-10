package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/node"
)

const defaultTimeout = 5 * time.Second
const defaultVirtualNodes = 128

// Gateway is a client-side routing layer over independent shardforge-node HTTP servers.
//
// Routing: Put/Get/Delete choose the target node deterministically by key using a
// consistent-hash ring. No retry to another node is attempted because without
// replication a key written to node-A cannot be found on node-B.
//
// Scope: no distributed sharding inside nodes, no networked replication, no Raft,
// no consensus, no quorum replication, no automatic leader election, no shard migration,
// no distributed transactions, no failover.
type Gateway struct {
	ring    *hashRing
	clients map[string]*node.Client // nodeID → client
	opts    Options
	mu      sync.Mutex
	closed  bool
}

// Open validates opts, constructs the consistent-hash ring, and creates one node.Client
// per configured node.
func Open(opts Options) (*Gateway, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	vn := opts.VirtualNodes
	if vn <= 0 {
		vn = defaultVirtualNodes
	}

	ring, err := newHashRing(opts.Nodes, vn)
	if err != nil {
		return nil, err
	}

	clients := make(map[string]*node.Client, len(opts.Nodes))
	for _, n := range opts.Nodes {
		clients[n.ID] = node.NewClient(n.BaseURL, timeout)
	}

	return &Gateway{
		ring:    ring,
		clients: clients,
		opts:    opts,
	}, nil
}

// validateOptions checks Options before ring construction.
// Ring construction performs its own node-level validation.
func validateOptions(opts Options) error {
	if len(opts.Nodes) == 0 {
		return fmt.Errorf("%w: at least one node is required", ErrInvalidOptions)
	}
	return nil
}

// NodeForKey returns the RoutedNode selected for key by the consistent-hash ring.
// Returns ErrInvalidKey if key is empty.
// Returns ErrClosed if the Gateway has been closed.
func (g *Gateway) NodeForKey(key []byte) (RoutedNode, error) {
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	if closed {
		return RoutedNode{}, ErrClosed
	}
	nc, err := g.ring.nodeForKey(key)
	if err != nil {
		return RoutedNode{}, err
	}
	return RoutedNode{ID: nc.ID, BaseURL: nc.BaseURL}, nil
}

// Put routes the key to its responsible node and calls node.Client.Put.
// Returns ErrInvalidKey if key is empty. Returns ErrClosed if Gateway is closed.
// No retry to another node is performed.
func (g *Gateway) Put(ctx context.Context, key, value []byte) error {
	c, err := g.clientForKey(key)
	if err != nil {
		return err
	}
	return c.Put(ctx, key, value)
}

// Get routes the key to its responsible node and calls node.Client.Get.
// Returns ErrInvalidKey if key is empty. Returns ErrClosed if Gateway is closed.
// No retry to another node is performed.
func (g *Gateway) Get(ctx context.Context, key []byte) ([]byte, bool, error) {
	c, err := g.clientForKey(key)
	if err != nil {
		return nil, false, err
	}
	return c.Get(ctx, key)
}

// Delete routes the key to its responsible node and calls node.Client.Delete.
// Returns ErrInvalidKey if key is empty. Returns ErrClosed if Gateway is closed.
// No retry to another node is performed.
func (g *Gateway) Delete(ctx context.Context, key []byte) error {
	c, err := g.clientForKey(key)
	if err != nil {
		return err
	}
	return c.Delete(ctx, key)
}

// ScanNode issues a Scan to the specified node only.
// Returns ErrUnknownNode if nodeID is not in the configured set.
// Returns ErrClosed if Gateway is closed.
//
// Note: this is a single-node scan. There is no global distributed scan because
// there is no replication — keys are partitioned across nodes by the ring.
func (g *Gateway) ScanNode(ctx context.Context, nodeID string, start, end []byte) ([]node.Entry, error) {
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	if closed {
		return nil, ErrClosed
	}
	c, ok := g.clients[nodeID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownNode, nodeID)
	}
	return c.Scan(ctx, start, end)
}

// FlushAll calls Flush on every configured node. All errors are collected; the first
// non-nil error is returned after all nodes have been attempted.
// Returns ErrClosed if Gateway is closed.
func (g *Gateway) FlushAll(ctx context.Context) error {
	clients, err := g.allClients()
	if err != nil {
		return err
	}
	var firstErr error
	for nodeID, c := range clients {
		if err := c.Flush(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("flush node %s: %w", nodeID, err)
		}
	}
	return firstErr
}

// CompactAll calls Compact on every configured node. All errors are collected; the
// first non-nil error is returned after all nodes have been attempted.
// Returns ErrClosed if Gateway is closed.
func (g *Gateway) CompactAll(ctx context.Context) error {
	clients, err := g.allClients()
	if err != nil {
		return err
	}
	var firstErr error
	for nodeID, c := range clients {
		if err := c.Compact(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("compact node %s: %w", nodeID, err)
		}
	}
	return firstErr
}

// HealthAll checks every configured node and returns a map of nodeID → error.
// A nil error value means the node is healthy. Returns ErrClosed if Gateway is closed.
func (g *Gateway) HealthAll(ctx context.Context) map[string]error {
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	if closed {
		result := make(map[string]error, len(g.clients))
		for id := range g.clients {
			result[id] = ErrClosed
		}
		return result
	}
	result := make(map[string]error, len(g.clients))
	for id, c := range g.clients {
		result[id] = c.Health(ctx)
	}
	return result
}

// Stats returns a snapshot of gateway ring configuration.
func (g *Gateway) Stats() Stats {
	nodes := g.ring.allNodes()
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	vn := g.opts.VirtualNodes
	if vn <= 0 {
		vn = defaultVirtualNodes
	}
	return Stats{
		NodeCount:    len(nodes),
		VirtualNodes: vn,
		Nodes:        ids,
	}
}

// Close marks the Gateway as closed. node.Client instances do not hold open connections
// that require cleanup, but Close prevents further operations and maintains API symmetry.
// Safe to call multiple times.
func (g *Gateway) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	return nil
}

// clientForKey selects the node.Client for key via the ring.
func (g *Gateway) clientForKey(key []byte) (*node.Client, error) {
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	if closed {
		return nil, ErrClosed
	}
	nc, err := g.ring.nodeForKey(key)
	if err != nil {
		return nil, err
	}
	return g.clients[nc.ID], nil
}

// allClients returns a snapshot copy of the client map, or ErrClosed.
func (g *Gateway) allClients() (map[string]*node.Client, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, ErrClosed
	}
	out := make(map[string]*node.Client, len(g.clients))
	for k, v := range g.clients {
		out[k] = v
	}
	return out, nil
}
