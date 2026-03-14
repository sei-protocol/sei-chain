package shadowreplay

import (
	"testing"
)

func TestNoopMetrics_DoesNotPanic(t *testing.T) {
	m := NoopMetrics()

	comp := &BlockComparison{
		Height:       100,
		AppHashMatch: true,
		GasUsedTotal: 50000,
		ElapsedMs:    85,
		Divergences: []Divergence{
			{Scope: ScopeTx, Severity: SeverityConcerning, Module: "evm", Field: "gas_used"},
		},
	}

	// Should not panic.
	m.RecordBlock(comp)
	m.BlocksPerSecond.Set(10.5)
	m.Height.Set(100)
	m.ChainTip.Set(200)
	m.BlocksBehind.Set(100)
}

func TestNewMetrics_RecordBlock(t *testing.T) {
	m := NewMetrics("test-chain")
	defer m.Stop()

	comp := &BlockComparison{
		Height:       500,
		AppHashMatch: false,
		GasUsedTotal: 120000,
		ElapsedMs:    200,
		Divergences: []Divergence{
			{Scope: ScopeBlock, Severity: SeverityCritical, Field: "app_hash"},
			{Scope: ScopeTx, Severity: SeverityConcerning, Module: "bank", Field: "gas_used"},
		},
	}

	// Should not panic.
	m.RecordBlock(comp)
}

func TestMetrics_ServeAndStop(t *testing.T) {
	m := NewMetrics("test-chain")

	if err := m.Serve(":0"); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	m.Stop()
	// Double stop should be safe.
	m.Stop()
}

func TestMetrics_ServeEmptyAddr(t *testing.T) {
	m := NoopMetrics()
	if err := m.Serve(""); err != nil {
		t.Fatalf("Serve with empty addr should be no-op, got: %v", err)
	}
}
