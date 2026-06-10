// Package shard implements single-process key-value sharding using multiple
// local Engine instances and deterministic consistent hashing.
//
// This is a local, single-process sharding layer only. There is no networking,
// no replication, no distributed cluster, and no Raft.
package shard

import (
	"fmt"
	"hash/fnv"
	"sort"
)

// ringToken is one position on the consistent hash ring.
type ringToken struct {
	hash    uint64
	shardID int
	vnode   int
}

// Ring is a deterministic consistent hash ring using FNV-1a 64-bit.
//
// For shard ID i and virtual node v, the token label is:
//
//	"shard-XXXX#vnode-XXXX"
//
// Ties in hash value are broken deterministically by shardID then vnode index.
type Ring struct {
	tokens []ringToken
}

// newRing builds a consistent hash ring for shardCount shards with
// virtualNodes virtual nodes per shard. Both arguments must be > 0.
func newRing(shardCount, virtualNodes int) *Ring {
	tokens := make([]ringToken, 0, shardCount*virtualNodes)
	for i := 0; i < shardCount; i++ {
		for v := 0; v < virtualNodes; v++ {
			label := fmt.Sprintf("shard-%04d#vnode-%04d", i, v)
			h := fnv1a64([]byte(label))
			tokens = append(tokens, ringToken{hash: h, shardID: i, vnode: v})
		}
	}
	// Sort by hash, then shardID, then vnode for deterministic tie-breaking.
	sort.Slice(tokens, func(a, b int) bool {
		if tokens[a].hash != tokens[b].hash {
			return tokens[a].hash < tokens[b].hash
		}
		if tokens[a].shardID != tokens[b].shardID {
			return tokens[a].shardID < tokens[b].shardID
		}
		return tokens[a].vnode < tokens[b].vnode
	})
	return &Ring{tokens: tokens}
}

// shardIndex returns the shard ID responsible for the given key.
// The algorithm: hash key with FNV-1a 64-bit, binary-search for the first
// ring token with hash >= key hash, wrap to token 0 if none found.
func (r *Ring) shardIndex(key []byte) int {
	h := fnv1a64(key)
	n := len(r.tokens)
	idx := sort.Search(n, func(i int) bool {
		return r.tokens[i].hash >= h
	})
	if idx == n {
		idx = 0 // wrap around to first token
	}
	return r.tokens[idx].shardID
}

// fnv1a64 returns the FNV-1a 64-bit hash of data.
func fnv1a64(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64()
}
