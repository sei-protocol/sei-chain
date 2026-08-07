package main

import (
	"math"
	"testing"
)

func TestRoundRobinInterleaves(t *testing.T) {
	packages := []string{"a", "b", "c", "d", "e", "f", "g"}
	shards := roundRobin(packages, 3)

	if got, want := len(shards), 3; got != want {
		t.Fatalf("len(shards) = %d, want %d", got, want)
	}
	want := [][]string{{"a", "d", "g"}, {"b", "e"}, {"c", "f"}}
	for i := range want {
		if !equalSlices(shards[i], want[i]) {
			t.Errorf("shard %d = %v, want %v", i, shards[i], want[i])
		}
	}
}

func TestBinPackBalancesKnownDurations(t *testing.T) {
	packages := []string{"slow", "medium1", "medium2", "tiny1", "tiny2", "tiny3"}
	timings := map[string]float64{
		"slow":    30,
		"medium1": 25,
		"medium2": 25,
		"tiny1":   5,
		"tiny2":   5,
		"tiny3":   5,
	}

	shards := binPack(packages, timings, 3)
	if got, want := len(shards), 3; got != want {
		t.Fatalf("len(shards) = %d, want %d", got, want)
	}

	totals := make([]float64, 3)
	seen := map[string]bool{}
	for i, shard := range shards {
		for _, pkg := range shard {
			totals[i] += timings[pkg]
			seen[pkg] = true
		}
	}
	for _, pkg := range packages {
		if !seen[pkg] {
			t.Errorf("package %q missing from output", pkg)
		}
	}

	maxTotal, minTotal := totals[0], totals[0]
	for _, tot := range totals {
		maxTotal = math.Max(maxTotal, tot)
		minTotal = math.Min(minTotal, tot)
	}
	if maxTotal-minTotal > 10 {
		t.Errorf("shard totals too imbalanced: %v (spread %.0f)", totals, maxTotal-minTotal)
	}
}

func TestBinPackEstimatesUnknownAsMean(t *testing.T) {
	packages := []string{"known1", "known2", "unknown"}
	timings := map[string]float64{"known1": 10, "known2": 30}
	// mean of known durations is 20, so "unknown" should behave like a 20s package.

	shards := binPack(packages, timings, 2)
	totals := make([]float64, 2)
	for i, shard := range shards {
		for _, pkg := range shard {
			dur, ok := timings[pkg]
			if !ok {
				dur = 20
			}
			totals[i] += dur
		}
	}
	if math.Abs(totals[0]-totals[1]) > 1e-9 {
		t.Errorf("expected balanced totals treating unknown as mean, got %v", totals)
	}
}

func TestTimingCoverage(t *testing.T) {
	packages := []string{"a", "b", "c", "d"}
	timings := map[string]float64{"a": 1, "b": 2}

	if got, want := timingCoverage(packages, timings), 0.5; got != want {
		t.Errorf("timingCoverage() = %v, want %v", got, want)
	}
	if got := timingCoverage(nil, timings); got != 0 {
		t.Errorf("timingCoverage(nil, ...) = %v, want 0", got)
	}
}

func TestMeanDuration(t *testing.T) {
	if got := meanDuration(nil); got != 0 {
		t.Errorf("meanDuration(nil) = %v, want 0", got)
	}
	timings := map[string]float64{"a": 10, "b": 30}
	if got, want := meanDuration(timings), 20.0; got != want {
		t.Errorf("meanDuration() = %v, want %v", got, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
