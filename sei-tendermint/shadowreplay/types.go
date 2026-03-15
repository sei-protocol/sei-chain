package shadowreplay

import (
	"encoding/hex"
	"fmt"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// Severity levels for divergences, ordered from most to least critical.
const (
	SeverityCritical   = "critical"
	SeverityConcerning = "concerning"
	SeverityBenign     = "benign"
	SeverityInfo       = "info"
)

// Divergence scopes indicate where in the block a divergence was observed.
const (
	ScopeBlock = "block"
	ScopeTx    = "tx"
	ScopeEvent = "event"
)

// Epoch represents the divergence tracking state.
const (
	EpochClean    = "clean"
	EpochDiverged = "diverged"
)

// BlockComparison is the enriched per-block output record written as NDJSON.
type BlockComparison struct {
	Height       int64  `json:"height"`
	Timestamp    string `json:"timestamp"`
	AppHashMatch bool   `json:"app_hash_match"`
	CanonicalApp string `json:"canonical_app_hash"`
	ReplayApp    string `json:"replay_app_hash"`
	TxCount      int    `json:"tx_count"`
	GasUsedTotal int64  `json:"gas_used_total"`
	ElapsedMs    int64  `json:"elapsed_ms"`

	Epoch                string `json:"epoch"`
	DivergenceOrigin     int64  `json:"divergence_origin_height,omitempty"`
	BlocksSinceDivergent int64  `json:"blocks_since_divergence,omitempty"`

	Summary     BlockSummary `json:"summary"`
	Divergences []Divergence `json:"divergences"`
}

// BlockSummary provides aggregate counts by tx type and divergence severity.
type BlockSummary struct {
	EVMTxCount      int `json:"evm_tx_count"`
	CosmosTxCount   int `json:"cosmos_tx_count"`
	FallbackTxCount int `json:"fallback_tx_count"`
	CriticalCount   int `json:"critical_count"`
	ConcerningCount int `json:"concerning_count"`
	InfoCount       int `json:"info_count"`
	BenignCount     int `json:"benign_count"`
}

// Divergence captures a single field-level difference between canonical and
// replay execution.
type Divergence struct {
	Scope     string `json:"scope"`
	Severity  string `json:"severity"`
	Module    string `json:"module,omitempty"`
	Field     string `json:"field"`
	TxIndex   int    `json:"tx_index,omitempty"`
	TxHash    string `json:"tx_hash,omitempty"`
	TxType    string `json:"tx_type,omitempty"`
	EventType string `json:"event_type,omitempty"`
	EventIdx  int    `json:"event_index,omitempty"`
	AttrKey   string `json:"attr_key,omitempty"`

	Canonical interface{} `json:"canonical"`
	Replay    interface{} `json:"replay"`
	Detail    string      `json:"detail,omitempty"`
}

// EpochState tracks the divergence epoch state machine (clean ↔ diverged).
type EpochState struct {
	Current     string
	OriginHeight int64
}

// NewEpochState returns an epoch state starting in clean mode.
func NewEpochState() *EpochState {
	return &EpochState{Current: EpochClean}
}

// Transition updates epoch state based on an app hash comparison.
// Returns whether this block is a new divergence origin (first mismatch).
func (e *EpochState) Transition(appHashMatch bool, height int64) (isNewOrigin bool) {
	if appHashMatch && e.Current == EpochClean {
		return false
	}
	if !appHashMatch && e.Current == EpochClean {
		e.Current = EpochDiverged
		e.OriginHeight = height
		return true
	}
	// Already diverged — cascading.
	return false
}

// BlocksSince returns how many blocks have passed since the divergence origin.
func (e *EpochState) BlocksSince(currentHeight int64) int64 {
	if e.Current != EpochDiverged {
		return 0
	}
	return currentHeight - e.OriginHeight
}

// TxClass identifies the execution path a transaction took through the Giga engine.
type TxClass string

const (
	TxClassEVM      TxClass = "evm"
	TxClassCosmos   TxClass = "cosmos"
	TxClassFallback TxClass = "fallback"
)

// classifyTx determines whether a tx was processed by the Giga EVM executor,
// the Cosmos SDK path, or fell back from Giga to V2.
//
// Heuristic: if canonical has events but replay has none, Giga's EVM path
// executed the tx (it doesn't emit ABCI events). If both have events but with
// ordering differences, the block likely fell back to V2. If both have zero
// events, it's a Cosmos tx with no events.
func classifyTx(canonical, replay *abci.ExecTxResult) TxClass {
	cEvents := len(canonical.Events)
	rEvents := len(replay.Events)

	if cEvents > 0 && rEvents == 0 {
		return TxClassEVM
	}
	if cEvents > 0 && rEvents > 0 && rEvents != cEvents {
		return TxClassFallback
	}
	return TxClassCosmos
}

// CompareTxResults compares canonical vs replay transaction results for a
// single block and returns all detected divergences plus a block summary.
func CompareTxResults(canonical []*abci.ExecTxResult, replay []*abci.ExecTxResult, txHashes []string) ([]Divergence, BlockSummary) {
	var divs []Divergence
	var summary BlockSummary

	minLen := len(canonical)
	if len(replay) < minLen {
		minLen = len(replay)
	}

	if len(canonical) != len(replay) {
		divs = append(divs, Divergence{
			Scope:     ScopeBlock,
			Severity:  SeverityCritical,
			Field:     "tx_count",
			Canonical: len(canonical),
			Replay:    len(replay),
			Detail:    fmt.Sprintf("canonical has %d txs, replay has %d", len(canonical), len(replay)),
		})
	}

	for i := 0; i < minLen; i++ {
		want := canonical[i]
		got := replay[i]
		txHash := ""
		if i < len(txHashes) {
			txHash = txHashes[i]
		}

		txClass := classifyTx(want, got)
		switch txClass {
		case TxClassEVM:
			summary.EVMTxCount++
		case TxClassCosmos:
			summary.CosmosTxCount++
		case TxClassFallback:
			summary.FallbackTxCount++
		}

		divs = append(divs, compareSingleTx(i, txHash, string(txClass), want, got)...)
	}

	for _, d := range divs {
		switch d.Severity {
		case SeverityCritical:
			summary.CriticalCount++
		case SeverityConcerning:
			summary.ConcerningCount++
		case SeverityInfo:
			summary.InfoCount++
		case SeverityBenign:
			summary.BenignCount++
		}
	}

	return divs, summary
}

func compareSingleTx(idx int, txHash string, txType string, want, got *abci.ExecTxResult) []Divergence {
	var divs []Divergence

	if want.Code != got.Code {
		sev := SeverityConcerning
		if (want.Code == 0) != (got.Code == 0) {
			sev = SeverityCritical
		}
		divs = append(divs, Divergence{
			Scope:     ScopeTx,
			Severity:  sev,
			Field:     "code",
			TxIndex:   idx,
			TxHash:    txHash,
			TxType:    txType,
			Canonical: want.Code,
			Replay:    got.Code,
		})
	}

	if want.GasUsed != got.GasUsed {
		sev := SeverityInfo
		var delta float64
		if want.GasUsed != 0 {
			delta = float64(got.GasUsed-want.GasUsed) / float64(want.GasUsed)
			if delta > 0.01 || delta < -0.01 {
				sev = SeverityConcerning
			}
		} else {
			sev = SeverityConcerning
		}
		divs = append(divs, Divergence{
			Scope:     ScopeTx,
			Severity:  sev,
			Field:     "gas_used",
			TxIndex:   idx,
			TxHash:    txHash,
			TxType:    txType,
			Canonical: want.GasUsed,
			Replay:    got.GasUsed,
			Detail:    fmt.Sprintf("delta=%.4f%%", delta*100),
		})
	}

	if string(want.Data) != string(got.Data) {
		divs = append(divs, Divergence{
			Scope:     ScopeTx,
			Severity:  SeverityConcerning,
			Field:     "data",
			TxIndex:   idx,
			TxHash:    txHash,
			TxType:    txType,
			Canonical: hex.EncodeToString(want.Data),
			Replay:    hex.EncodeToString(got.Data),
		})
	}

	if want.Codespace != got.Codespace {
		sev := SeverityConcerning
		// Giga doesn't propagate the SDK codespace for failed EVM txs.
		if txType == string(TxClassEVM) && want.Codespace == "sdk" && got.Codespace == "" {
			sev = SeverityInfo
		}
		divs = append(divs, Divergence{
			Scope:     ScopeTx,
			Severity:  sev,
			Field:     "codespace",
			TxIndex:   idx,
			TxHash:    txHash,
			TxType:    txType,
			Canonical: want.Codespace,
			Replay:    got.Codespace,
		})
	}

	divs = append(divs, compareEvents(idx, txHash, txType, want.Events, got.Events)...)

	return divs
}

// isAnteHandlerSwap detects the known tx/signer event ordering difference
// between canonical and Giga's Cosmos path. Returns true if events at indices
// i and i+1 are a complementary swap of the "tx" and "signer" event types.
func isAnteHandlerSwap(want, got []abci.Event, i int) bool {
	if i+1 >= len(want) || i+1 >= len(got) {
		return false
	}
	return want[i].Type == "tx" && want[i+1].Type == "signer" &&
		got[i].Type == "signer" && got[i+1].Type == "tx"
}

func compareEvents(txIdx int, txHash string, txType string, want, got []abci.Event) []Divergence {
	var divs []Divergence

	if len(want) != len(got) {
		sev := SeverityConcerning
		// Giga EVM path intentionally skips ABCI event emission.
		if txType == string(TxClassEVM) && len(got) == 0 {
			sev = SeverityInfo
		}
		divs = append(divs, Divergence{
			Scope:     ScopeEvent,
			Severity:  sev,
			Field:     "event_count",
			TxIndex:   txIdx,
			TxHash:    txHash,
			TxType:    txType,
			Canonical: len(want),
			Replay:    len(got),
			Detail:    fmt.Sprintf("tx_type=%s", txType),
		})
	}

	// For EVM txs with zero replay events, skip per-event comparison entirely
	// since we already captured the count divergence above.
	if txType == string(TxClassEVM) && len(got) == 0 {
		return divs
	}

	minLen := len(want)
	if len(got) < minLen {
		minLen = len(got)
	}

	skipNext := false
	for i := 0; i < minLen; i++ {
		if skipNext {
			skipNext = false
			continue
		}

		wantEv := want[i]
		gotEv := got[i]

		if wantEv.Type != gotEv.Type {
			// Detect known ante-handler tx/signer swap and collapse into
			// a single benign divergence instead of two concerning ones.
			if isAnteHandlerSwap(want, got, i) {
				divs = append(divs, Divergence{
					Scope:     ScopeEvent,
					Severity:  SeverityBenign,
					Field:     "event_type_swap",
					TxIndex:   txIdx,
					TxHash:    txHash,
					TxType:    txType,
					EventIdx:  i,
					Canonical: fmt.Sprintf("%s,%s", want[i].Type, want[i+1].Type),
					Replay:    fmt.Sprintf("%s,%s", got[i].Type, got[i+1].Type),
					Detail:    "ante-handler event reordering (benign)",
				})
				skipNext = true
				continue
			}

			divs = append(divs, Divergence{
				Scope:     ScopeEvent,
				Severity:  SeverityConcerning,
				Field:     "event_type",
				TxIndex:   txIdx,
				TxHash:    txHash,
				TxType:    txType,
				EventType: wantEv.Type,
				EventIdx:  i,
				Canonical: wantEv.Type,
				Replay:    gotEv.Type,
			})
			continue
		}

		divs = append(divs, compareEventAttrs(txIdx, txHash, i, wantEv.Type, wantEv.Attributes, gotEv.Attributes)...)
	}

	return divs
}

// anteHandlerAttrs are attributes that move between events due to ante-handler
// reordering in Giga. Missing/extra occurrences of these keys are benign.
var anteHandlerAttrs = map[string]bool{
	"fee": true, "fee_payer": true,
	"acc_seq": true, "signature": true,
}

func compareEventAttrs(txIdx int, txHash string, evtIdx int, evtType string, want, got []abci.EventAttribute) []Divergence {
	var divs []Divergence

	wantMap := make(map[string]string, len(want))
	for _, attr := range want {
		wantMap[string(attr.Key)] = string(attr.Value)
	}

	gotMap := make(map[string]string, len(got))
	for _, attr := range got {
		gotMap[string(attr.Key)] = string(attr.Value)
	}

	for k, wantVal := range wantMap {
		gotVal, ok := gotMap[k]
		if !ok {
			sev := SeverityConcerning
			detail := "attribute present in canonical but missing in replay"
			if anteHandlerAttrs[k] {
				sev = SeverityBenign
				detail = "ante-handler attribute reshuffled (benign)"
			}
			divs = append(divs, Divergence{
				Scope:     ScopeEvent,
				Severity:  sev,
				Field:     "event_attr_missing",
				TxIndex:   txIdx,
				TxHash:    txHash,
				EventType: evtType,
				EventIdx:  evtIdx,
				AttrKey:   k,
				Canonical: wantVal,
				Replay:    nil,
				Detail:    detail,
			})
		} else if wantVal != gotVal {
			divs = append(divs, Divergence{
				Scope:     ScopeEvent,
				Severity:  SeverityConcerning,
				Field:     "event_attr_value",
				TxIndex:   txIdx,
				TxHash:    txHash,
				EventType: evtType,
				EventIdx:  evtIdx,
				AttrKey:   k,
				Canonical: wantVal,
				Replay:    gotVal,
			})
		}
	}

	for k, gotVal := range gotMap {
		if _, ok := wantMap[k]; !ok {
			sev := SeverityBenign
			detail := "attribute present in replay but missing in canonical"
			if anteHandlerAttrs[k] {
				detail = "ante-handler attribute reshuffled (benign)"
			}
			divs = append(divs, Divergence{
				Scope:     ScopeEvent,
				Severity:  sev,
				Field:     "event_attr_extra",
				TxIndex:   txIdx,
				TxHash:    txHash,
				EventType: evtType,
				EventIdx:  evtIdx,
				AttrKey:   k,
				Canonical: nil,
				Replay:    gotVal,
				Detail:    detail,
			})
		}
	}

	return divs
}

// MaxSeverity returns the highest severity found among a set of divergences.
func MaxSeverity(divs []Divergence) string {
	order := map[string]int{
		SeverityCritical:   4,
		SeverityConcerning: 3,
		SeverityBenign:     2,
		SeverityInfo:       1,
	}
	max := 0
	maxSev := ""
	for _, d := range divs {
		if o := order[d.Severity]; o > max {
			max = o
			maxSev = d.Severity
		}
	}
	return maxSev
}
