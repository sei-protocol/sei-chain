package shadowreplay

import (
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

func TestCompareTxResults_AllMatch(t *testing.T) {
	canonical := []*abci.ExecTxResult{
		{Code: 0, GasUsed: 100000, Data: []byte("ok"), Codespace: "", Events: []abci.Event{{Type: "transfer", Attributes: []abci.EventAttribute{{Key: []byte("amount"), Value: []byte("100usei")}}}}},
	}
	replay := []*abci.ExecTxResult{
		{Code: 0, GasUsed: 100000, Data: []byte("ok"), Codespace: "", Events: []abci.Event{{Type: "transfer", Attributes: []abci.EventAttribute{{Key: []byte("amount"), Value: []byte("100usei")}}}}},
	}
	divs := CompareTxResults(canonical, replay, []string{"txhash1"})
	if len(divs) != 0 {
		t.Errorf("expected no divergences, got %d: %+v", len(divs), divs)
	}
}

func TestCompareTxResults_CodeDifference(t *testing.T) {
	canonical := []*abci.ExecTxResult{{Code: 0, GasUsed: 100}}
	replay := []*abci.ExecTxResult{{Code: 5, GasUsed: 100}}

	divs := CompareTxResults(canonical, replay, []string{"tx0"})
	if len(divs) != 1 {
		t.Fatalf("expected 1 divergence, got %d", len(divs))
	}
	if divs[0].Severity != SeverityCritical {
		t.Errorf("success->failure should be critical, got %s", divs[0].Severity)
	}
	if divs[0].Field != "code" {
		t.Errorf("expected field 'code', got %s", divs[0].Field)
	}
}

func TestCompareTxResults_CodeBothNonZero(t *testing.T) {
	canonical := []*abci.ExecTxResult{{Code: 3, GasUsed: 100}}
	replay := []*abci.ExecTxResult{{Code: 7, GasUsed: 100}}

	divs := CompareTxResults(canonical, replay, []string{"tx0"})
	if len(divs) != 1 {
		t.Fatalf("expected 1 divergence, got %d", len(divs))
	}
	if divs[0].Severity != SeverityConcerning {
		t.Errorf("non-zero->non-zero code difference should be concerning, got %s", divs[0].Severity)
	}
}

func TestCompareTxResults_GasDelta(t *testing.T) {
	canonical := []*abci.ExecTxResult{{Code: 0, GasUsed: 100000}}
	replay := []*abci.ExecTxResult{{Code: 0, GasUsed: 100500}}

	divs := CompareTxResults(canonical, replay, []string{"tx0"})
	if len(divs) != 1 {
		t.Fatalf("expected 1 divergence, got %d", len(divs))
	}
	if divs[0].Severity != SeverityInfo {
		t.Errorf("small gas delta (<1%%) should be info, got %s", divs[0].Severity)
	}
}

func TestCompareTxResults_LargeGasDelta(t *testing.T) {
	canonical := []*abci.ExecTxResult{{Code: 0, GasUsed: 100000}}
	replay := []*abci.ExecTxResult{{Code: 0, GasUsed: 110000}}

	divs := CompareTxResults(canonical, replay, []string{"tx0"})
	if len(divs) != 1 {
		t.Fatalf("expected 1 divergence, got %d", len(divs))
	}
	if divs[0].Severity != SeverityConcerning {
		t.Errorf("large gas delta (>1%%) should be concerning, got %s", divs[0].Severity)
	}
}

func TestCompareTxResults_TxCountMismatch(t *testing.T) {
	canonical := []*abci.ExecTxResult{{Code: 0, GasUsed: 100}, {Code: 0, GasUsed: 200}}
	replay := []*abci.ExecTxResult{{Code: 0, GasUsed: 100}}

	divs := CompareTxResults(canonical, replay, []string{"tx0", "tx1"})

	var found bool
	for _, d := range divs {
		if d.Field == "tx_count" && d.Severity == SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected critical tx_count divergence, got: %+v", divs)
	}
}

func TestCompareTxResults_EventCountMismatch(t *testing.T) {
	canonical := []*abci.ExecTxResult{{Code: 0, GasUsed: 100, Events: []abci.Event{{Type: "a"}, {Type: "b"}}}}
	replay := []*abci.ExecTxResult{{Code: 0, GasUsed: 100, Events: []abci.Event{{Type: "a"}}}}

	divs := CompareTxResults(canonical, replay, []string{"tx0"})

	var found bool
	for _, d := range divs {
		if d.Field == "event_count" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected event_count divergence, got: %+v", divs)
	}
}

func TestCompareTxResults_EventAttrValueDiff(t *testing.T) {
	canonical := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 100,
		Events: []abci.Event{{
			Type: "transfer",
			Attributes: []abci.EventAttribute{
				{Key: []byte("amount"), Value: []byte("1000usei")},
			},
		}},
	}}
	replay := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 100,
		Events: []abci.Event{{
			Type: "transfer",
			Attributes: []abci.EventAttribute{
				{Key: []byte("amount"), Value: []byte("999usei")},
			},
		}},
	}}

	divs := CompareTxResults(canonical, replay, []string{"tx0"})

	var found bool
	for _, d := range divs {
		if d.Field == "event_attr_value" && d.AttrKey == "amount" {
			found = true
			if d.Canonical != "1000usei" || d.Replay != "999usei" {
				t.Errorf("unexpected values: canonical=%v, replay=%v", d.Canonical, d.Replay)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected event_attr_value divergence for 'amount', got: %+v", divs)
	}
}

func TestCompareTxResults_EventAttrMissing(t *testing.T) {
	canonical := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 100,
		Events: []abci.Event{{
			Type: "transfer",
			Attributes: []abci.EventAttribute{
				{Key: []byte("sender"), Value: []byte("sei1abc")},
				{Key: []byte("recipient"), Value: []byte("sei1xyz")},
			},
		}},
	}}
	replay := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 100,
		Events: []abci.Event{{
			Type: "transfer",
			Attributes: []abci.EventAttribute{
				{Key: []byte("sender"), Value: []byte("sei1abc")},
			},
		}},
	}}

	divs := CompareTxResults(canonical, replay, []string{"tx0"})

	var found bool
	for _, d := range divs {
		if d.Field == "event_attr_missing" && d.AttrKey == "recipient" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected event_attr_missing for 'recipient', got: %+v", divs)
	}
}

func TestCompareTxResults_MultipleTxMultipleDivergences(t *testing.T) {
	canonical := []*abci.ExecTxResult{
		{Code: 0, GasUsed: 100000, Data: []byte("ok")},
		{Code: 0, GasUsed: 200000, Codespace: "bank"},
	}
	replay := []*abci.ExecTxResult{
		{Code: 0, GasUsed: 100000, Data: []byte("different")},
		{Code: 0, GasUsed: 200000, Codespace: "evm"},
	}

	divs := CompareTxResults(canonical, replay, []string{"tx0", "tx1"})
	if len(divs) < 2 {
		t.Errorf("expected at least 2 divergences, got %d: %+v", len(divs), divs)
	}
}

func TestMaxSeverity(t *testing.T) {
	tests := []struct {
		divs     []Divergence
		expected string
	}{
		{nil, ""},
		{[]Divergence{{Severity: SeverityInfo}}, SeverityInfo},
		{[]Divergence{{Severity: SeverityInfo}, {Severity: SeverityConcerning}}, SeverityConcerning},
		{[]Divergence{{Severity: SeverityCritical}, {Severity: SeverityBenign}}, SeverityCritical},
	}

	for _, tt := range tests {
		got := MaxSeverity(tt.divs)
		if got != tt.expected {
			t.Errorf("MaxSeverity(%v) = %s, want %s", tt.divs, got, tt.expected)
		}
	}
}

func TestEpochState_CleanToClean(t *testing.T) {
	e := NewEpochState()
	isNew := e.Transition(true, 100)
	if isNew {
		t.Error("matching app hash should not be a new origin")
	}
	if e.Current != EpochClean {
		t.Errorf("expected clean, got %s", e.Current)
	}
}

func TestEpochState_CleanToDiverged(t *testing.T) {
	e := NewEpochState()
	isNew := e.Transition(false, 100)
	if !isNew {
		t.Error("first mismatch should be a new origin")
	}
	if e.Current != EpochDiverged {
		t.Errorf("expected diverged, got %s", e.Current)
	}
	if e.OriginHeight != 100 {
		t.Errorf("expected origin 100, got %d", e.OriginHeight)
	}
}

func TestEpochState_CascadingNotNewOrigin(t *testing.T) {
	e := NewEpochState()
	e.Transition(false, 100) // first divergence

	isNew := e.Transition(false, 101)
	if isNew {
		t.Error("cascading mismatch should not be a new origin")
	}
	if e.OriginHeight != 100 {
		t.Errorf("origin should remain 100, got %d", e.OriginHeight)
	}
}

func TestEpochState_BlocksSince(t *testing.T) {
	e := NewEpochState()
	if bs := e.BlocksSince(100); bs != 0 {
		t.Errorf("clean epoch should return 0 blocks since, got %d", bs)
	}

	e.Transition(false, 100)
	if bs := e.BlocksSince(150); bs != 50 {
		t.Errorf("expected 50 blocks since divergence, got %d", bs)
	}
}
