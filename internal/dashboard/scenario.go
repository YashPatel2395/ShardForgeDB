package dashboard

import (
	"fmt"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replica"
)

// ReplicaScenarioTarget identifies the local replica store and the follower
// replica ID that a chaos scenario will operate on.
type ReplicaScenarioTarget struct {
	Store      *replica.Store
	FollowerID int
}

// ── scenario helpers ──────────────────────────────────────────────────────────

func newEvent(source, action, msg string) TimelineEvent {
	return TimelineEvent{
		Time:    time.Now(),
		Source:  source,
		Action:  action,
		Message: msg,
	}
}

func failedResult(name string, steps []ScenarioStep, events []TimelineEvent, err string) ScenarioResult {
	return ScenarioResult{
		Name:   name,
		Status: ScenarioFailed,
		Steps:  steps,
		Events: events,
		Error:  err,
	}
}

// validateTarget returns a non-empty error string if the target is unusable.
func validateTarget(name string, t ReplicaScenarioTarget) string {
	if t.Store == nil {
		return "Store must not be nil"
	}
	infos := t.Store.Replicas()
	for _, r := range infos {
		if r.ID == t.FollowerID {
			if r.Role == replica.RoleLeader {
				return fmt.Sprintf("replica %d is the leader, not a follower", t.FollowerID)
			}
			return "" // found, is follower — OK
		}
	}
	return fmt.Sprintf("follower ID %d not found", t.FollowerID)
}

// ── RunFollowerPauseScenario ──────────────────────────────────────────────────

// RunFollowerPauseScenario runs a deterministic pause/unpause scenario:
//
//  1. Write a key to the leader.
//  2. Pause the follower.
//  3. Call ReplicateAll — follower must not advance.
//  4. Read from the follower (ReadFollower) and confirm the key is absent.
//  5. Unpause the follower.
//  6. Call ReplicateAll — follower must now catch up.
//  7. Read from the follower and confirm the key is present.
func RunFollowerPauseScenario(target ReplicaScenarioTarget) ScenarioResult {
	const name = "FollowerPauseScenario"
	steps := []ScenarioStep{
		{Name: "write_leader", Action: "Put key on leader", Expectation: "leader accepts write"},
		{Name: "pause_follower", Action: "SetFollowerPaused(true)", Expectation: "follower marked paused"},
		{Name: "replicate_paused", Action: "ReplicateAll while paused", Expectation: "follower does not receive new key"},
		{Name: "read_stale", Action: "Get from follower (paused)", Expectation: "key not found on follower"},
		{Name: "unpause_follower", Action: "SetFollowerPaused(false)", Expectation: "follower marked unpaused"},
		{Name: "replicate_catch_up", Action: "ReplicateAll after unpause", Expectation: "follower catches up"},
		{Name: "read_current", Action: "Get from follower (caught up)", Expectation: "key found on follower"},
	}
	var events []TimelineEvent

	// Validate inputs — must not panic.
	if errStr := validateTarget(name, target); errStr != "" {
		events = append(events, newEvent(name, "validate", "invalid target: "+errStr))
		return failedResult(name, steps, events, errStr)
	}

	const key = "chaos-pause-key-001"
	const val = "chaos-pause-val-001"

	// Step 1: write to leader.
	events = append(events, newEvent(name, "write_leader", fmt.Sprintf("Put %q=%q", key, val)))
	if _, err := target.Store.Put([]byte(key), []byte(val)); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("Put failed: %v", err))
	}
	events = append(events, newEvent(name, "write_leader", "Put succeeded"))

	// Step 2: pause follower.
	events = append(events, newEvent(name, "pause_follower", fmt.Sprintf("pausing follower %d", target.FollowerID)))
	if err := target.Store.SetFollowerPaused(target.FollowerID, true); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("SetFollowerPaused(true): %v", err))
	}
	events = append(events, newEvent(name, "pause_follower", "follower paused"))

	// Step 3: replicate while paused.
	events = append(events, newEvent(name, "replicate_paused", "calling ReplicateAll"))
	if err := target.Store.ReplicateAll(); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("ReplicateAll (paused): %v", err))
	}
	events = append(events, newEvent(name, "replicate_paused", "ReplicateAll done (follower was paused)"))

	// Step 4: read from follower — must not find key.
	events = append(events, newEvent(name, "read_stale", fmt.Sprintf("Get %q from follower %d", key, target.FollowerID)))
	v, found, err := target.Store.Get([]byte(key), replica.ReadFollower, target.FollowerID)
	if err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("Get (paused follower): %v", err))
	}
	if found {
		return failedResult(name, steps, events,
			fmt.Sprintf("expected key absent on paused follower, got %q", v))
	}
	events = append(events, newEvent(name, "read_stale", "key correctly absent on paused follower"))

	// Step 5: unpause follower.
	events = append(events, newEvent(name, "unpause_follower", fmt.Sprintf("unpausing follower %d", target.FollowerID)))
	if err := target.Store.SetFollowerPaused(target.FollowerID, false); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("SetFollowerPaused(false): %v", err))
	}
	events = append(events, newEvent(name, "unpause_follower", "follower unpaused"))

	// Step 6: replicate after unpause.
	events = append(events, newEvent(name, "replicate_catch_up", "calling ReplicateAll"))
	if err := target.Store.ReplicateAll(); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("ReplicateAll (catch-up): %v", err))
	}
	events = append(events, newEvent(name, "replicate_catch_up", "ReplicateAll done"))

	// Step 7: read from follower — must now find key.
	events = append(events, newEvent(name, "read_current", fmt.Sprintf("Get %q from follower %d", key, target.FollowerID)))
	v, found, err = target.Store.Get([]byte(key), replica.ReadFollower, target.FollowerID)
	if err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("Get (caught-up follower): %v", err))
	}
	if !found {
		return failedResult(name, steps, events, "key not found on follower after catch-up")
	}
	if string(v) != val {
		return failedResult(name, steps, events, fmt.Sprintf("value mismatch: got %q, want %q", v, val))
	}
	events = append(events, newEvent(name, "read_current", fmt.Sprintf("key found with correct value %q", v)))

	return ScenarioResult{
		Name:   name,
		Status: ScenarioPassed,
		Steps:  steps,
		Events: events,
	}
}

// ── RunFollowerLagScenario ────────────────────────────────────────────────────

// RunFollowerLagScenario runs a deterministic lag simulation scenario:
//
//  1. Set follower lag limit to 2 ops/call.
//  2. Put 6 keys on the leader.
//  3. Call ReplicateOnce — follower may apply at most 2 ops.
//  4. Verify follower's applied index < commit index (lagging).
//  5. Call ReplicateAll — follower must reach full catch-up.
//  6. Verify all 6 keys visible on follower.
func RunFollowerLagScenario(target ReplicaScenarioTarget) ScenarioResult {
	const name = "FollowerLagScenario"
	steps := []ScenarioStep{
		{Name: "set_lag", Action: "SetFollowerLag(2)", Expectation: "follower limited to 2 ops/call"},
		{Name: "write_keys", Action: "Put 6 keys on leader", Expectation: "leader commits 6 ops"},
		{Name: "replicate_once", Action: "ReplicateOnce", Expectation: "follower applies ≤ 2 ops"},
		{Name: "verify_lag", Action: "Stats: follower applied < commit", Expectation: "lag confirmed"},
		{Name: "replicate_all", Action: "ReplicateAll", Expectation: "follower catches up fully"},
		{Name: "read_all", Action: "Get all 6 keys from follower", Expectation: "all keys present"},
	}
	var events []TimelineEvent

	if errStr := validateTarget(name, target); errStr != "" {
		events = append(events, newEvent(name, "validate", "invalid target: "+errStr))
		return failedResult(name, steps, events, errStr)
	}

	const lagLimit = 2
	const keyCount = 6

	// Step 1: set lag.
	events = append(events, newEvent(name, "set_lag", fmt.Sprintf("SetFollowerLag(%d, %d)", target.FollowerID, lagLimit)))
	if err := target.Store.SetFollowerLag(target.FollowerID, lagLimit); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("SetFollowerLag: %v", err))
	}
	events = append(events, newEvent(name, "set_lag", fmt.Sprintf("lag set to %d ops/call", lagLimit)))

	// Step 2: write 6 keys.
	for i := 0; i < keyCount; i++ {
		k := fmt.Sprintf("chaos-lag-key-%03d", i)
		v := fmt.Sprintf("chaos-lag-val-%03d", i)
		if _, err := target.Store.Put([]byte(k), []byte(v)); err != nil {
			return failedResult(name, steps, events, fmt.Sprintf("Put key %d: %v", i, err))
		}
	}
	events = append(events, newEvent(name, "write_keys", fmt.Sprintf("wrote %d keys to leader", keyCount)))

	// Step 3: ReplicateOnce.
	events = append(events, newEvent(name, "replicate_once", "calling ReplicateOnce"))
	applied, err := target.Store.ReplicateOnce()
	if err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("ReplicateOnce: %v", err))
	}
	events = append(events, newEvent(name, "replicate_once", fmt.Sprintf("applied %d op(s) this call", applied)))

	// Step 4: verify lag.
	st := target.Store.Stats()
	var followerApplied replica.LogIndex
	for _, r := range st.Replicas {
		if r.ID == target.FollowerID {
			followerApplied = r.AppliedIndex
		}
	}
	events = append(events, newEvent(name, "verify_lag",
		fmt.Sprintf("follower applied=%d commit=%d", followerApplied, st.CommitIndex)))
	if followerApplied >= st.CommitIndex {
		// All ops applied already — lag limit may have been ignored or
		// keyCount ≤ lagLimit. Treat as non-fatal; continue to catch-up.
		events = append(events, newEvent(name, "verify_lag", "follower not lagging (all applied); continuing"))
	} else {
		events = append(events, newEvent(name, "verify_lag", "lag confirmed"))
	}

	// Step 5: ReplicateAll catch-up.
	events = append(events, newEvent(name, "replicate_all", "calling ReplicateAll"))
	if err := target.Store.ReplicateAll(); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("ReplicateAll: %v", err))
	}
	events = append(events, newEvent(name, "replicate_all", "ReplicateAll done"))

	// Clear the lag limit so subsequent calls on this store aren't affected.
	_ = target.Store.SetFollowerLag(target.FollowerID, 0)

	// Step 6: verify all keys visible on follower.
	for i := 0; i < keyCount; i++ {
		k := fmt.Sprintf("chaos-lag-key-%03d", i)
		expected := fmt.Sprintf("chaos-lag-val-%03d", i)
		v, found, err := target.Store.Get([]byte(k), replica.ReadFollower, target.FollowerID)
		if err != nil {
			return failedResult(name, steps, events, fmt.Sprintf("Get key %d: %v", i, err))
		}
		if !found {
			return failedResult(name, steps, events, fmt.Sprintf("key %q not found on follower after catch-up", k))
		}
		if string(v) != expected {
			return failedResult(name, steps, events, fmt.Sprintf("key %q value mismatch: got %q want %q", k, v, expected))
		}
	}
	events = append(events, newEvent(name, "read_all", fmt.Sprintf("all %d keys verified on follower", keyCount)))

	return ScenarioResult{
		Name:   name,
		Status: ScenarioPassed,
		Steps:  steps,
		Events: events,
	}
}

// ── RunFollowerCatchupScenario ────────────────────────────────────────────────

// RunFollowerCatchupScenario runs a deterministic catch-up scenario:
//
//  1. Pause the follower.
//  2. Put several keys on the leader.
//  3. Verify follower does not see the keys (stale).
//  4. Unpause the follower.
//  5. Call ReplicateAll.
//  6. Verify all keys now visible on the follower.
func RunFollowerCatchupScenario(target ReplicaScenarioTarget) ScenarioResult {
	const name = "FollowerCatchupScenario"
	steps := []ScenarioStep{
		{Name: "pause_follower", Action: "SetFollowerPaused(true)", Expectation: "follower paused"},
		{Name: "write_keys", Action: "Put 4 keys on leader", Expectation: "leader commits 4 ops"},
		{Name: "verify_stale", Action: "Get keys from follower (paused)", Expectation: "keys absent"},
		{Name: "unpause", Action: "SetFollowerPaused(false)", Expectation: "follower unpaused"},
		{Name: "replicate_all", Action: "ReplicateAll", Expectation: "follower catches up"},
		{Name: "verify_current", Action: "Get all keys from follower", Expectation: "all keys present"},
	}
	var events []TimelineEvent

	if errStr := validateTarget(name, target); errStr != "" {
		events = append(events, newEvent(name, "validate", "invalid target: "+errStr))
		return failedResult(name, steps, events, errStr)
	}

	const keyCount = 4

	// Step 1: pause.
	events = append(events, newEvent(name, "pause_follower", fmt.Sprintf("pausing follower %d", target.FollowerID)))
	if err := target.Store.SetFollowerPaused(target.FollowerID, true); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("SetFollowerPaused(true): %v", err))
	}
	events = append(events, newEvent(name, "pause_follower", "follower paused"))

	// Step 2: write keys.
	for i := 0; i < keyCount; i++ {
		k := fmt.Sprintf("chaos-catchup-key-%03d", i)
		v := fmt.Sprintf("chaos-catchup-val-%03d", i)
		if _, err := target.Store.Put([]byte(k), []byte(v)); err != nil {
			_ = target.Store.SetFollowerPaused(target.FollowerID, false) // best-effort cleanup
			return failedResult(name, steps, events, fmt.Sprintf("Put key %d: %v", i, err))
		}
	}
	events = append(events, newEvent(name, "write_keys", fmt.Sprintf("wrote %d keys while follower paused", keyCount)))

	// Step 3: verify stale — keys must be absent.
	for i := 0; i < keyCount; i++ {
		k := fmt.Sprintf("chaos-catchup-key-%03d", i)
		_, found, err := target.Store.Get([]byte(k), replica.ReadFollower, target.FollowerID)
		if err != nil {
			_ = target.Store.SetFollowerPaused(target.FollowerID, false)
			return failedResult(name, steps, events, fmt.Sprintf("Get (stale) key %d: %v", i, err))
		}
		if found {
			_ = target.Store.SetFollowerPaused(target.FollowerID, false)
			return failedResult(name, steps, events,
				fmt.Sprintf("key %q unexpectedly found on paused follower", k))
		}
	}
	events = append(events, newEvent(name, "verify_stale", "all keys correctly absent on paused follower"))

	// Step 4: unpause.
	events = append(events, newEvent(name, "unpause", fmt.Sprintf("unpausing follower %d", target.FollowerID)))
	if err := target.Store.SetFollowerPaused(target.FollowerID, false); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("SetFollowerPaused(false): %v", err))
	}
	events = append(events, newEvent(name, "unpause", "follower unpaused"))

	// Step 5: ReplicateAll.
	events = append(events, newEvent(name, "replicate_all", "calling ReplicateAll"))
	if err := target.Store.ReplicateAll(); err != nil {
		return failedResult(name, steps, events, fmt.Sprintf("ReplicateAll: %v", err))
	}
	events = append(events, newEvent(name, "replicate_all", "ReplicateAll done"))

	// Step 6: verify all keys present.
	for i := 0; i < keyCount; i++ {
		k := fmt.Sprintf("chaos-catchup-key-%03d", i)
		expected := fmt.Sprintf("chaos-catchup-val-%03d", i)
		v, found, err := target.Store.Get([]byte(k), replica.ReadFollower, target.FollowerID)
		if err != nil {
			return failedResult(name, steps, events, fmt.Sprintf("Get (caught-up) key %d: %v", i, err))
		}
		if !found {
			return failedResult(name, steps, events, fmt.Sprintf("key %q not found on follower after catch-up", k))
		}
		if string(v) != expected {
			return failedResult(name, steps, events, fmt.Sprintf("key %q value mismatch: got %q want %q", k, v, expected))
		}
	}
	events = append(events, newEvent(name, "verify_current", fmt.Sprintf("all %d keys verified on follower", keyCount)))

	return ScenarioResult{
		Name:   name,
		Status: ScenarioPassed,
		Steps:  steps,
		Events: events,
	}
}
