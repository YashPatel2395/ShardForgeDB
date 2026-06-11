package dashboard

import (
	"os"
	"sort"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/replica"
)

// openScenarioStore opens a fresh 3-replica store for scenario tests.
func openScenarioStore(t *testing.T) *replica.Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "scenario-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	st, err := replica.Open(replica.Options{Dir: dir, ReplicaCount: 3})
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("replica.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		os.RemoveAll(dir)
	})
	return st
}

// ── Test 16: bad input returns failed, not panic ───────────────────────────────

func TestScenario_BadInput_NilStore(t *testing.T) {
	target := ReplicaScenarioTarget{Store: nil, FollowerID: 1}
	for _, run := range []func(ReplicaScenarioTarget) ScenarioResult{
		RunFollowerPauseScenario,
		RunFollowerLagScenario,
		RunFollowerCatchupScenario,
	} {
		result := run(target)
		if result.Status != ScenarioFailed {
			t.Errorf("expected ScenarioFailed, got %q", result.Status)
		}
		if result.Error == "" {
			t.Error("expected non-empty Error")
		}
	}
}

// Test 22: invalid follower ID returns failed result with error.
func TestScenario_InvalidFollowerID(t *testing.T) {
	st := openScenarioStore(t)
	target := ReplicaScenarioTarget{Store: st, FollowerID: 999}
	result := RunFollowerPauseScenario(target)
	if result.Status != ScenarioFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if result.Error == "" {
		t.Error("expected non-empty Error for invalid follower ID")
	}
}

// Test 22b: leader ID as follower returns failed.
func TestScenario_LeaderAsFollower(t *testing.T) {
	st := openScenarioStore(t)
	// Leader is replica 0 by default.
	target := ReplicaScenarioTarget{Store: st, FollowerID: 0}
	result := RunFollowerPauseScenario(target)
	if result.Status != ScenarioFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
}

// ── Test 17: FollowerPauseScenario passes ─────────────────────────────────────

func TestRunFollowerPauseScenario_Passes(t *testing.T) {
	st := openScenarioStore(t)
	target := ReplicaScenarioTarget{Store: st, FollowerID: 1}
	result := RunFollowerPauseScenario(target)
	if result.Status != ScenarioPassed {
		t.Errorf("status = %q, want passed; error: %s", result.Status, result.Error)
	}
}

// ── Test 18: FollowerLagScenario passes ───────────────────────────────────────

func TestRunFollowerLagScenario_Passes(t *testing.T) {
	st := openScenarioStore(t)
	target := ReplicaScenarioTarget{Store: st, FollowerID: 1}
	result := RunFollowerLagScenario(target)
	if result.Status != ScenarioPassed {
		t.Errorf("status = %q, want passed; error: %s", result.Status, result.Error)
	}
}

// ── Test 19: FollowerCatchupScenario passes ───────────────────────────────────

func TestRunFollowerCatchupScenario_Passes(t *testing.T) {
	st := openScenarioStore(t)
	target := ReplicaScenarioTarget{Store: st, FollowerID: 1}
	result := RunFollowerCatchupScenario(target)
	if result.Status != ScenarioPassed {
		t.Errorf("status = %q, want passed; error: %s", result.Status, result.Error)
	}
}

// ── Test 20: Scenario events are ordered ─────────────────────────────────────

func TestScenario_EventsOrdered(t *testing.T) {
	st := openScenarioStore(t)
	target := ReplicaScenarioTarget{Store: st, FollowerID: 1}
	result := RunFollowerPauseScenario(target)
	if result.Status != ScenarioPassed {
		t.Fatalf("scenario failed: %s", result.Error)
	}
	if len(result.Events) == 0 {
		t.Fatal("no events")
	}
	// Use strict Before (not !After) as the less function for sort.SliceIsSorted.
	// !After is equivalent to <=, which causes SliceIsSorted to return false when
	// two adjacent events have identical nanosecond timestamps (both less(i,j) and
	// less(j,i) would be true). Before correctly handles equal timestamps as
	// "neither before nor after" — the sort check passes for equal adjacent events.
	if !sort.SliceIsSorted(result.Events, func(i, j int) bool {
		return result.Events[i].Time.Before(result.Events[j].Time)
	}) {
		t.Error("events are not in non-decreasing time order")
	}
}

// ── Test 21: Scenario result includes steps ───────────────────────────────────

func TestScenario_IncludesSteps(t *testing.T) {
	for _, run := range []struct {
		name string
		fn   func(ReplicaScenarioTarget) ScenarioResult
	}{
		{"Pause", RunFollowerPauseScenario},
		{"Lag", RunFollowerLagScenario},
		{"Catchup", RunFollowerCatchupScenario},
	} {
		t.Run(run.name, func(t *testing.T) {
			// Each scenario needs a fresh store to avoid state interference.
			fresh := openScenarioStore(t)
			result := run.fn(ReplicaScenarioTarget{Store: fresh, FollowerID: 1})
			if len(result.Steps) == 0 {
				t.Error("no steps in result")
			}
			for _, s := range result.Steps {
				if s.Name == "" {
					t.Error("step has empty Name")
				}
				if s.Action == "" {
					t.Error("step has empty Action")
				}
			}
		})
	}
}

// Test: scenario name is set correctly.
func TestScenario_Name(t *testing.T) {
	st := openScenarioStore(t)
	cases := []struct {
		fn   func(ReplicaScenarioTarget) ScenarioResult
		want string
	}{
		{RunFollowerPauseScenario, "FollowerPauseScenario"},
		{RunFollowerLagScenario, "FollowerLagScenario"},
		{RunFollowerCatchupScenario, "FollowerCatchupScenario"},
	}
	for _, tc := range cases {
		result := tc.fn(ReplicaScenarioTarget{Store: st, FollowerID: 1})
		if result.Name != tc.want {
			t.Errorf("Name = %q, want %q", result.Name, tc.want)
		}
		// re-open for next; avoid state from previous scenario.
		st = openScenarioStore(t)
	}
}

// Test: scenario events have non-empty Source/Action/Message.
func TestScenario_EventFields(t *testing.T) {
	st := openScenarioStore(t)
	target := ReplicaScenarioTarget{Store: st, FollowerID: 1}
	result := RunFollowerCatchupScenario(target)
	if result.Status != ScenarioPassed {
		t.Fatalf("scenario failed: %s", result.Error)
	}
	for _, e := range result.Events {
		if e.Source == "" {
			t.Error("event has empty Source")
		}
		if e.Action == "" {
			t.Error("event has empty Action")
		}
		if e.Time.IsZero() {
			t.Error("event has zero Time")
		}
	}
}

// Test: follower 2 also works as target.
func TestRunFollowerPauseScenario_Follower2(t *testing.T) {
	st := openScenarioStore(t)
	target := ReplicaScenarioTarget{Store: st, FollowerID: 2}
	result := RunFollowerPauseScenario(target)
	if result.Status != ScenarioPassed {
		t.Errorf("status = %q; error: %s", result.Status, result.Error)
	}
}

// Test 29: scenario does not mutate global state.
func TestScenario_NoGlobalMutation(t *testing.T) {
	st1 := openScenarioStore(t)
	st2 := openScenarioStore(t)

	r1 := RunFollowerPauseScenario(ReplicaScenarioTarget{Store: st1, FollowerID: 1})
	r2 := RunFollowerPauseScenario(ReplicaScenarioTarget{Store: st2, FollowerID: 1})

	if r1.Status != ScenarioPassed {
		t.Errorf("r1 failed: %s", r1.Error)
	}
	if r2.Status != ScenarioPassed {
		t.Errorf("r2 failed: %s", r2.Error)
	}
}

// Benchmark scenarios.
func BenchmarkRunFollowerPauseScenario(b *testing.B) {
	for i := 0; i < b.N; i++ {
		dir, _ := os.MkdirTemp("", "bench-pause-*")
		st, _ := replica.Open(replica.Options{Dir: dir, ReplicaCount: 3})
		target := ReplicaScenarioTarget{Store: st, FollowerID: 1}
		RunFollowerPauseScenario(target)
		_ = st.Close()
		os.RemoveAll(dir)
	}
}

func BenchmarkRunFollowerLagScenario(b *testing.B) {
	for i := 0; i < b.N; i++ {
		dir, _ := os.MkdirTemp("", "bench-lag-*")
		st, _ := replica.Open(replica.Options{Dir: dir, ReplicaCount: 3})
		target := ReplicaScenarioTarget{Store: st, FollowerID: 1}
		RunFollowerLagScenario(target)
		_ = st.Close()
		os.RemoveAll(dir)
	}
}

func BenchmarkRunFollowerCatchupScenario(b *testing.B) {
	for i := 0; i < b.N; i++ {
		dir, _ := os.MkdirTemp("", "bench-catchup-*")
		st, _ := replica.Open(replica.Options{Dir: dir, ReplicaCount: 3})
		target := ReplicaScenarioTarget{Store: st, FollowerID: 1}
		RunFollowerCatchupScenario(target)
		_ = st.Close()
		os.RemoveAll(dir)
	}
}
