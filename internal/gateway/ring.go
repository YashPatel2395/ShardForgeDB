package gateway

import (
	"fmt"
	"hash/fnv"
	"sort"
)

// ringPoint is one virtual node on the consistent-hash ring.
type ringPoint struct {
	hash   uint64
	nodeID string
}

// hashRing is a sorted consistent-hash ring backed by FNV-1a 64-bit hashing.
// It is immutable once constructed.
type hashRing struct {
	points []ringPoint    // sorted by hash ascending
	index  map[string]int // nodeID → index in nodes slice
	nodes  []NodeConfig   // original node configs
}

// newHashRing constructs a deterministic consistent-hash ring.
// virtualNodes is the base number of ring points per node (before weight scaling).
func newHashRing(nodes []NodeConfig, virtualNodes int) (*hashRing, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: at least one node is required", ErrInvalidOptions)
	}
	if virtualNodes <= 0 {
		virtualNodes = 128
	}

	// Validate: no duplicate IDs or BaseURLs.
	seenIDs := make(map[string]bool, len(nodes))
	seenURLs := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("%w: node ID must not be empty", ErrInvalidOptions)
		}
		if n.BaseURL == "" {
			return nil, fmt.Errorf("%w: node BaseURL must not be empty (node %q)", ErrInvalidOptions, n.ID)
		}
		if seenIDs[n.ID] {
			return nil, fmt.Errorf("%w: duplicate node ID %q", ErrInvalidOptions, n.ID)
		}
		if seenURLs[n.BaseURL] {
			return nil, fmt.Errorf("%w: duplicate node BaseURL %q", ErrInvalidOptions, n.BaseURL)
		}
		seenIDs[n.ID] = true
		seenURLs[n.BaseURL] = true
	}

	// Build ring points.
	var points []ringPoint
	for _, n := range nodes {
		w := n.Weight
		if w <= 0 {
			w = 1
		}
		count := virtualNodes * w
		for i := 0; i < count; i++ {
			h := fnv1a64(fmt.Sprintf("%s:%d", n.ID, i))
			points = append(points, ringPoint{hash: h, nodeID: n.ID})
		}
	}

	// Sort by hash for O(log n) lookup.
	sort.Slice(points, func(i, j int) bool {
		return points[i].hash < points[j].hash
	})

	// Build nodeID index.
	idx := make(map[string]int, len(nodes))
	for i, n := range nodes {
		idx[n.ID] = i
	}

	return &hashRing{points: points, index: idx, nodes: nodes}, nil
}

// nodeForKey returns the NodeConfig responsible for key using clockwise lookup.
// Returns ErrInvalidKey if key is empty.
func (r *hashRing) nodeForKey(key []byte) (NodeConfig, error) {
	if len(key) == 0 {
		return NodeConfig{}, ErrInvalidKey
	}
	h := fnv1a64(string(key))

	// Binary search for first point with hash >= h.
	i := sort.Search(len(r.points), func(i int) bool {
		return r.points[i].hash >= h
	})
	if i >= len(r.points) {
		i = 0 // wrap around
	}
	nodeID := r.points[i].nodeID
	return r.nodes[r.index[nodeID]], nil
}

// nodeByID returns the NodeConfig for a given ID, or an error if not found.
func (r *hashRing) nodeByID(id string) (NodeConfig, error) {
	idx, ok := r.index[id]
	if !ok {
		return NodeConfig{}, fmt.Errorf("%w: %q", ErrUnknownNode, id)
	}
	return r.nodes[idx], nil
}

// allNodes returns all configured nodes.
func (r *hashRing) allNodes() []NodeConfig {
	out := make([]NodeConfig, len(r.nodes))
	copy(out, r.nodes)
	return out
}

// fnv1a64 hashes s using FNV-1a 64-bit.
func fnv1a64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
