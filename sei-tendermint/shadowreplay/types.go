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

	Divergences []Divergence `json:"divergences"`
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

// CompareTxResults compares canonical vs replay transaction results for a
// single block and returns all detected divergences.
func CompareTxResults(canonical []*abci.ExecTxResult, replay []*abci.ExecTxResult, txHashes []string) []Divergence {
	var divs []Divergence

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
		divs = append(divs, compareSingleTx(i, txHash, want, got)...)
	}

	return divs
}

func compareSingleTx(idx int, txHash string, want, got *abci.ExecTxResult) []Divergence {
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
			Canonical: hex.EncodeToString(want.Data),
			Replay:    hex.EncodeToString(got.Data),
		})
	}

	if want.Codespace != got.Codespace {
		divs = append(divs, Divergence{
			Scope:     ScopeTx,
			Severity:  SeverityConcerning,
			Field:     "codespace",
			TxIndex:   idx,
			TxHash:    txHash,
			Canonical: want.Codespace,
			Replay:    got.Codespace,
		})
	}

	divs = append(divs, compareEvents(idx, txHash, want.Events, got.Events)...)

	return divs
}

func compareEvents(txIdx int, txHash string, want, got []abci.Event) []Divergence {
	var divs []Divergence

	if len(want) != len(got) {
		divs = append(divs, Divergence{
			Scope:     ScopeEvent,
			Severity:  SeverityConcerning,
			Field:     "event_count",
			TxIndex:   txIdx,
			TxHash:    txHash,
			Canonical: len(want),
			Replay:    len(got),
		})
	}

	minLen := len(want)
	if len(got) < minLen {
		minLen = len(got)
	}

	for i := 0; i < minLen; i++ {
		wantEv := want[i]
		gotEv := got[i]

		if wantEv.Type != gotEv.Type {
			divs = append(divs, Divergence{
				Scope:     ScopeEvent,
				Severity:  SeverityConcerning,
				Field:     "event_type",
				TxIndex:   txIdx,
				TxHash:    txHash,
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
			divs = append(divs, Divergence{
				Scope:     ScopeEvent,
				Severity:  SeverityConcerning,
				Field:     "event_attr_missing",
				TxIndex:   txIdx,
				TxHash:    txHash,
				EventType: evtType,
				EventIdx:  evtIdx,
				AttrKey:   k,
				Canonical: wantVal,
				Replay:    nil,
				Detail:    "attribute present in canonical but missing in replay",
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
			divs = append(divs, Divergence{
				Scope:     ScopeEvent,
				Severity:  SeverityBenign,
				Field:     "event_attr_extra",
				TxIndex:   txIdx,
				TxHash:    txHash,
				EventType: evtType,
				EventIdx:  evtIdx,
				AttrKey:   k,
				Canonical: nil,
				Replay:    gotVal,
				Detail:    "attribute present in replay but missing in canonical",
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
