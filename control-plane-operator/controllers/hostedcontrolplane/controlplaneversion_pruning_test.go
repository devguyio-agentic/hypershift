package hostedcontrolplane

import (
	"fmt"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// makeHistory creates a slice of ControlPlaneUpdateHistory entries for pruning tests.
// Entries are ordered newest-first (index 0 = most recent). Callers provide version
// strings in the form "4.17.2" and states (Completed or Partial).
func makeHistory(entries []struct {
	version string
	state   configv1.UpdateState
}) []hyperv1.ControlPlaneUpdateHistory {
	result := make([]hyperv1.ControlPlaneUpdateHistory, len(entries))
	for i, e := range entries {
		h := hyperv1.ControlPlaneUpdateHistory{
			State:       e.state,
			StartedTime: metav1.Now(),
			Version:     e.version,
			Image:       fmt.Sprintf("quay.io/ocp/release:%s", e.version),
		}
		if e.state == configv1.CompletedUpdate {
			now := metav1.Now()
			h.CompletionTime = &now
		}
		result[i] = h
	}
	return result
}

// TestPruneHistory_CapAt100 verifies AC8: when more than 100 history entries
// exist, pruning reduces the list to exactly 100.
func TestPruneHistory_CapAt100(t *testing.T) {
	// Build 105 entries: all Completed with incrementing z-stream versions
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 105)
	for i := 0; i < 105; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 104-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned := pruneHistory(history)

	if len(pruned) != maxHistory {
		t.Errorf("When more than 100 entries exist, it should prune to %d, got %d", maxHistory, len(pruned))
	}
}

// TestPruneHistory_ProtectedIndices verifies that entries at indices 0
// through maxFinalEntryIndex (0-4) are never pruned, regardless of their
// content.
func TestPruneHistory_ProtectedIndices(t *testing.T) {
	// Build 102 entries so pruning must remove 2. First 5 are z-stream
	// partials (would normally be pruned first), but they're at protected indices.
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 102)
	// Indices 0-4: z-stream partials (should be protected despite low weight)
	for i := 0; i < 5; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 100-i),
			state:   configv1.PartialUpdate,
		}
	}
	// Index 5 onwards: completed entries with incrementing z-streams
	for i := 5; i < 102; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 100-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned := pruneHistory(history)

	if len(pruned) != maxHistory {
		t.Fatalf("When pruning, it should reduce to %d entries, got %d", maxHistory, len(pruned))
	}
	// Verify the first 5 entries survived
	for i := 0; i < 5; i++ {
		if pruned[i].Version != entries[i].version {
			t.Errorf("When pruning, it should protect index %d (version %s), but got %s",
				i, entries[i].version, pruned[i].Version)
		}
	}
}

// TestPruneHistory_OldestEntryProtected verifies that the oldest entry (the
// one at position maxHistory when the list is exactly maxHistory+1) is
// protected with mostImportantWeight and is never pruned.
func TestPruneHistory_OldestEntryProtected(t *testing.T) {
	// Build 101 entries. The oldest (index 100) should survive.
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 101)
	for i := 0; i < 101; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 100-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)
	oldestVersion := history[100].Version

	pruned := pruneHistory(history)

	if len(pruned) != maxHistory {
		t.Fatalf("When pruning, it should reduce to %d entries, got %d", maxHistory, len(pruned))
	}
	// The oldest entry should still be present
	if pruned[len(pruned)-1].Version != oldestVersion {
		t.Errorf("When pruning, it should protect the oldest entry (version %s), but last entry is %s",
			oldestVersion, pruned[len(pruned)-1].Version)
	}
}

// TestPruneHistory_MostRecentCompletedProtected verifies that the most
// recently completed entry (first Completed scanning from index 0) receives
// mostImportantWeight and is never pruned.
func TestPruneHistory_MostRecentCompletedProtected(t *testing.T) {
	// Build 101 entries. Index 0 is Partial (in-progress), index 1 is the
	// most recently completed. Remaining are completed z-streams.
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 101)
	entries[0] = struct {
		version string
		state   configv1.UpdateState
	}{version: "4.18.0", state: configv1.PartialUpdate}
	entries[1] = struct {
		version string
		state   configv1.UpdateState
	}{version: "4.17.99", state: configv1.CompletedUpdate}
	for i := 2; i < 101; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 100-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned := pruneHistory(history)

	if len(pruned) != maxHistory {
		t.Fatalf("When pruning, it should reduce to %d entries, got %d", maxHistory, len(pruned))
	}
	// The most recently completed entry (4.17.99) must survive
	found := false
	for _, h := range pruned {
		if h.Version == "4.17.99" {
			found = true
			break
		}
	}
	if !found {
		t.Error("When pruning, it should protect the most recently completed entry (4.17.99)")
	}
}

// TestPruneHistory_ZStreamPartialsFirst verifies that z-stream partial
// entries (Partial state between completed entries of different z-stream
// versions within the same minor) are pruned first due to their negative
// partialZStreamWeight (-20.0).
func TestPruneHistory_ZStreamPartialsFirst(t *testing.T) {
	// Build 101 entries with a mix of completed and z-stream partials.
	// The z-stream partial (not in protected range) should be pruned.
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 101)
	for i := 0; i < 5; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 100-i),
			state:   configv1.CompletedUpdate,
		}
	}
	// Index 5: a z-stream partial between completed z-stream entries
	entries[5] = struct {
		version string
		state   configv1.UpdateState
	}{version: "4.17.94", state: configv1.PartialUpdate}
	// Index 6 onwards: completed
	for i := 6; i < 101; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 100-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned := pruneHistory(history)

	if len(pruned) != maxHistory {
		t.Fatalf("When pruning, it should reduce to %d entries, got %d", maxHistory, len(pruned))
	}
	// The z-stream partial should have been pruned
	for _, h := range pruned {
		if h.Version == "4.17.94" && h.State == configv1.PartialUpdate {
			t.Error("When z-stream partials exist, it should prune them first")
		}
	}
}

// TestPruneHistory_InterestingEntriesPreserved verifies that the first and
// last Completed entries in each minor version receive interestingWeight
// (30.0) and are preserved over lower-ranked entries.
func TestPruneHistory_InterestingEntriesPreserved(t *testing.T) {
	// Build 101 entries across two minor versions. The first and last
	// completed in each minor should survive pruning.
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 101)
	// 0-4: protected range (4.18.x completed)
	for i := 0; i < 5; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.18.%d", 4-i),
			state:   configv1.CompletedUpdate,
		}
	}
	// 5-50: 4.17.x completed (first=4.17.45, last=4.17.0)
	for i := 5; i < 51; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 50-i),
			state:   configv1.CompletedUpdate,
		}
	}
	// 51-100: 4.16.x completed (first=4.16.49, last=4.16.0)
	for i := 51; i < 101; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.16.%d", 100-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned := pruneHistory(history)

	if len(pruned) != maxHistory {
		t.Fatalf("When pruning, it should reduce to %d entries, got %d", maxHistory, len(pruned))
	}

	// The first completed in 4.17 (4.17.45 at original index 5) and
	// last completed in 4.17 (4.17.0 at original index 50) should survive
	// because they are "interesting" (first/last in minor).
	versionsInPruned := make(map[string]bool)
	for _, h := range pruned {
		versionsInPruned[h.Version] = true
	}

	// First completed in 4.17.x minor
	if !versionsInPruned["4.17.45"] {
		t.Error("When pruning, it should preserve the first completed entry in minor 4.17 (4.17.45)")
	}
	// Last completed in 4.17.x minor
	if !versionsInPruned["4.17.0"] {
		t.Error("When pruning, it should preserve the last completed entry in minor 4.17 (4.17.0)")
	}
	// First completed in 4.16.x minor
	if !versionsInPruned["4.16.49"] {
		t.Error("When pruning, it should preserve the first completed entry in minor 4.16 (4.16.49)")
	}
	// Last completed in 4.16.x minor (also oldest, so doubly protected)
	if !versionsInPruned["4.16.0"] {
		t.Error("When pruning, it should preserve the last completed entry in minor 4.16 (4.16.0)")
	}
}

// TestPruneHistory_DeterministicResults verifies that the pruning algorithm
// produces deterministic results when called multiple times with the same
// input (the -1.01 index weight breaks ties).
func TestPruneHistory_DeterministicResults(t *testing.T) {
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 105)
	for i := 0; i < 105; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 104-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned1 := pruneHistory(history)
	pruned2 := pruneHistory(makeHistory(entries))

	if len(pruned1) != len(pruned2) {
		t.Fatalf("When pruning the same input twice, it should produce same length, got %d vs %d",
			len(pruned1), len(pruned2))
	}
	for i := range pruned1 {
		if pruned1[i].Version != pruned2[i].Version {
			t.Errorf("When pruning the same input twice, it should produce deterministic results at index %d: %s vs %s",
				i, pruned1[i].Version, pruned2[i].Version)
		}
	}
}

// TestPruneHistory_Under100Unchanged verifies that when history has fewer
// than MaxHistory entries, pruning is a no-op.
func TestPruneHistory_Under100Unchanged(t *testing.T) {
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 50)
	for i := 0; i < 50; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 49-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned := pruneHistory(history)

	if len(pruned) != 50 {
		t.Errorf("When fewer than %d entries exist, it should not prune, got %d entries", maxHistory, len(pruned))
	}
}

// TestPruneHistory_Exactly100Unchanged verifies that when history has exactly
// MaxHistory entries, pruning is a no-op.
func TestPruneHistory_Exactly100Unchanged(t *testing.T) {
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 100)
	for i := 0; i < 100; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 99-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned := pruneHistory(history)

	if len(pruned) != 100 {
		t.Errorf("When exactly %d entries exist, it should not prune, got %d entries", maxHistory, len(pruned))
	}
}

// TestPruneHistory_MinorTransitionPartialPreserved verifies that a Partial
// entry between completed entries of different minor versions receives
// partialMinorWeight (20.0) and is preserved over z-stream partials.
func TestPruneHistory_MinorTransitionPartialPreserved(t *testing.T) {
	// Build 101 entries. Place a minor-transition partial at index 6
	// (between 4.18 completed and 4.17 completed) and a z-stream partial
	// at index 7 (between 4.17.x completed entries). The z-stream partial
	// should be pruned first.
	entries := make([]struct {
		version string
		state   configv1.UpdateState
	}, 101)
	// 0-4: protected (4.18.x)
	for i := 0; i < 5; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.18.%d", 4-i),
			state:   configv1.CompletedUpdate,
		}
	}
	// 5: last 4.18 completed
	entries[5] = struct {
		version string
		state   configv1.UpdateState
	}{version: "4.18.0", state: configv1.CompletedUpdate}
	// 6: minor-transition partial (between 4.18 and 4.17 completed)
	entries[6] = struct {
		version string
		state   configv1.UpdateState
	}{version: "4.17.99", state: configv1.PartialUpdate}
	// 7: z-stream partial (between 4.17.x completed entries)
	entries[7] = struct {
		version string
		state   configv1.UpdateState
	}{version: "4.17.50", state: configv1.PartialUpdate}
	// 8: 4.17 completed
	entries[8] = struct {
		version string
		state   configv1.UpdateState
	}{version: "4.17.49", state: configv1.CompletedUpdate}
	// 9-100: remaining 4.17.x completed
	for i := 9; i < 101; i++ {
		entries[i] = struct {
			version string
			state   configv1.UpdateState
		}{
			version: fmt.Sprintf("4.17.%d", 100-i),
			state:   configv1.CompletedUpdate,
		}
	}
	history := makeHistory(entries)

	pruned := pruneHistory(history)

	if len(pruned) != maxHistory {
		t.Fatalf("When pruning, it should reduce to %d entries, got %d", maxHistory, len(pruned))
	}
	// Minor transition partial should survive
	minorPartialFound := false
	zstreamPartialFound := false
	for _, h := range pruned {
		if h.Version == "4.17.99" && h.State == configv1.PartialUpdate {
			minorPartialFound = true
		}
		if h.Version == "4.17.50" && h.State == configv1.PartialUpdate {
			zstreamPartialFound = true
		}
	}
	if !minorPartialFound {
		t.Error("When pruning, it should preserve minor-transition partials over z-stream partials")
	}
	if zstreamPartialFound {
		t.Error("When pruning, it should prune z-stream partials before minor-transition partials")
	}
}
