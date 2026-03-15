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
	divs, summary := CompareTxResults(canonical, replay, []string{"txhash1"})
	if len(divs) != 0 {
		t.Errorf("expected no divergences, got %d: %+v", len(divs), divs)
	}
	if summary.CosmosTxCount != 1 {
		t.Errorf("expected 1 cosmos tx, got %d", summary.CosmosTxCount)
	}
}

func TestCompareTxResults_CodeDifference(t *testing.T) {
	canonical := []*abci.ExecTxResult{{Code: 0, GasUsed: 100}}
	replay := []*abci.ExecTxResult{{Code: 5, GasUsed: 100}}

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})
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

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})
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

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})
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

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})
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

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0", "tx1"})

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

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})

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

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})

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

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})

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

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0", "tx1"})
	if len(divs) < 2 {
		t.Errorf("expected at least 2 divergences, got %d: %+v", len(divs), divs)
	}
}

// --- EVM tx classification tests ---

func TestCompareTxResults_EVMTxZeroEvents(t *testing.T) {
	canonical := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 21000,
		Events: []abci.Event{{Type: "signer"}, {Type: "tx"}, {Type: "transfer"}, {Type: "message"}, {Type: "coin_spent"}, {Type: "coin_received"}, {Type: "transfer"}},
	}}
	replay := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 21000,
		Events: nil,
	}}

	divs, summary := CompareTxResults(canonical, replay, []string{"tx0"})

	if summary.EVMTxCount != 1 {
		t.Errorf("expected 1 EVM tx, got %d", summary.EVMTxCount)
	}

	if len(divs) != 1 {
		t.Fatalf("expected 1 divergence (event_count), got %d: %+v", len(divs), divs)
	}
	if divs[0].Severity != SeverityInfo {
		t.Errorf("EVM event_count=0 should be info, got %s", divs[0].Severity)
	}
	if divs[0].TxType != string(TxClassEVM) {
		t.Errorf("expected tx_type=evm, got %s", divs[0].TxType)
	}
}

func TestCompareTxResults_EVMCodespaceMismatch(t *testing.T) {
	canonical := []*abci.ExecTxResult{{
		Code: 11, GasUsed: 21000, Codespace: "sdk",
		Events: []abci.Event{{Type: "signer"}},
	}}
	replay := []*abci.ExecTxResult{{
		Code: 11, GasUsed: 21000, Codespace: "",
		Events: nil,
	}}

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})

	var codespacDiv *Divergence
	for i, d := range divs {
		if d.Field == "codespace" {
			codespacDiv = &divs[i]
			break
		}
	}
	if codespacDiv == nil {
		t.Fatal("expected codespace divergence")
	}
	if codespacDiv.Severity != SeverityInfo {
		t.Errorf("EVM codespace sdk->'' should be info, got %s", codespacDiv.Severity)
	}
}

func TestCompareTxResults_AnteHandlerSwap(t *testing.T) {
	canonical := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 100,
		Events: []abci.Event{
			{Type: "coin_spent"},
			{Type: "coin_received"},
			{Type: "tx", Attributes: []abci.EventAttribute{{Key: []byte("fee"), Value: []byte("1000usei")}}},
			{Type: "signer", Attributes: []abci.EventAttribute{{Key: []byte("addr"), Value: []byte("sei1abc")}}},
		},
	}}
	replay := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 100,
		Events: []abci.Event{
			{Type: "coin_spent"},
			{Type: "coin_received"},
			{Type: "signer", Attributes: []abci.EventAttribute{{Key: []byte("addr"), Value: []byte("sei1abc")}}},
			{Type: "tx", Attributes: []abci.EventAttribute{{Key: []byte("fee"), Value: []byte("1000usei")}}},
		},
	}}

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})

	var swapDiv *Divergence
	for i, d := range divs {
		if d.Field == "event_type_swap" {
			swapDiv = &divs[i]
			break
		}
	}
	if swapDiv == nil {
		t.Fatal("expected event_type_swap divergence for tx/signer swap")
	}
	if swapDiv.Severity != SeverityBenign {
		t.Errorf("ante-handler swap should be benign, got %s", swapDiv.Severity)
	}
}

func TestCompareTxResults_AnteHandlerAttrReshuffle(t *testing.T) {
	canonical := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 100,
		Events: []abci.Event{{
			Type: "tx",
			Attributes: []abci.EventAttribute{
				{Key: []byte("fee"), Value: []byte("1000usei")},
				{Key: []byte("fee_payer"), Value: []byte("sei1abc")},
			},
		}},
	}}
	replay := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 100,
		Events: []abci.Event{{
			Type: "tx",
			Attributes: []abci.EventAttribute{
				{Key: []byte("acc_seq"), Value: []byte("sei1abc/42")},
			},
		}},
	}}

	divs, _ := CompareTxResults(canonical, replay, []string{"tx0"})

	for _, d := range divs {
		if d.Field == "event_attr_missing" && (d.AttrKey == "fee" || d.AttrKey == "fee_payer") {
			if d.Severity != SeverityBenign {
				t.Errorf("ante-handler attr %q missing should be benign, got %s", d.AttrKey, d.Severity)
			}
		}
		if d.Field == "event_attr_extra" && d.AttrKey == "acc_seq" {
			if d.Severity != SeverityBenign {
				t.Errorf("ante-handler attr %q extra should be benign, got %s", d.AttrKey, d.Severity)
			}
		}
	}
}

func TestCompareTxResults_BlockSummary(t *testing.T) {
	evmEvents := []abci.Event{{Type: "signer"}, {Type: "tx"}, {Type: "transfer"}}
	canonical := []*abci.ExecTxResult{
		{Code: 0, GasUsed: 21000, Events: evmEvents},
		{Code: 0, GasUsed: 21000, Events: evmEvents},
		{Code: 0, GasUsed: 50000, Events: []abci.Event{{Type: "message"}}},
	}
	replay := []*abci.ExecTxResult{
		{Code: 0, GasUsed: 21000, Events: nil},
		{Code: 0, GasUsed: 21000, Events: nil},
		{Code: 0, GasUsed: 50000, Events: []abci.Event{{Type: "message"}}},
	}

	_, summary := CompareTxResults(canonical, replay, []string{"tx0", "tx1", "tx2"})

	if summary.EVMTxCount != 2 {
		t.Errorf("expected 2 EVM txs, got %d", summary.EVMTxCount)
	}
	if summary.CosmosTxCount != 1 {
		t.Errorf("expected 1 Cosmos tx, got %d", summary.CosmosTxCount)
	}
	if summary.InfoCount != 2 {
		t.Errorf("expected 2 info divergences (EVM event counts), got %d", summary.InfoCount)
	}
}

func TestCompareTxResults_FallbackDetection(t *testing.T) {
	canonical := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 21000,
		Events: []abci.Event{{Type: "signer"}, {Type: "tx"}, {Type: "transfer"}},
	}}
	replay := []*abci.ExecTxResult{{
		Code: 0, GasUsed: 21000,
		Events: []abci.Event{{Type: "tx"}, {Type: "transfer"}},
	}}

	_, summary := CompareTxResults(canonical, replay, []string{"tx0"})
	if summary.FallbackTxCount != 1 {
		t.Errorf("expected 1 fallback tx, got %d", summary.FallbackTxCount)
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

func TestClassifyTx(t *testing.T) {
	tests := []struct {
		name     string
		canon    *abci.ExecTxResult
		replay   *abci.ExecTxResult
		expected TxClass
	}{
		{"EVM - canonical events, replay none", &abci.ExecTxResult{Events: []abci.Event{{Type: "a"}}}, &abci.ExecTxResult{}, TxClassEVM},
		{"Cosmos - both same events", &abci.ExecTxResult{Events: []abci.Event{{Type: "a"}}}, &abci.ExecTxResult{Events: []abci.Event{{Type: "a"}}}, TxClassCosmos},
		{"Cosmos - both no events", &abci.ExecTxResult{}, &abci.ExecTxResult{}, TxClassCosmos},
		{"Fallback - different event counts", &abci.ExecTxResult{Events: []abci.Event{{Type: "a"}, {Type: "b"}, {Type: "c"}}}, &abci.ExecTxResult{Events: []abci.Event{{Type: "a"}, {Type: "b"}}}, TxClassFallback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyTx(tt.canon, tt.replay)
			if got != tt.expected {
				t.Errorf("classifyTx() = %s, want %s", got, tt.expected)
			}
		})
	}
}
