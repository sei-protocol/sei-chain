package verifier

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// ScanMismatch describes one key where FlatKV's exporter row could not be
// matched in memiavl. These are grouped by cause so alerting can distinguish
// "FlatKV has extra data" (expected never) from "value diverges" (bug).
type ScanMismatch struct {
	Kind  ScanMismatchKind
	Key   []byte
	Flat  []byte
	MemVal []byte
}

// ScanMismatchKind categorizes Oracle 2 / Oracle 4 forward-subset failures.
type ScanMismatchKind int

const (
	// MismatchValue: key exists on both sides but values differ.
	MismatchValue ScanMismatchKind = iota + 1
	// MismatchMemiavlMissing: flatkv has the key, memiavl does not.
	MismatchMemiavlMissing
	// MismatchDecode: the exporter row failed to decode.
	MismatchDecode
)

// ScanResult summarizes one forward-subset pass.
type ScanResult struct {
	RowsExamined int64
	Mismatches   []ScanMismatch
}

// RunForwardSubsetScan drains the supplied FlatKV exporter and for each
// decoded (module, key, value) entry calls evmStore.Get(key) and byte-compares.
// Implements Oracle 2 (latest version) and is the core of Oracle 4
// (historical version).
//
// The invariant under dual_write with no backfill is that FlatKV is a subset
// of memiavl — every FlatKV row must round-trip to an equal memiavl value.
// memiavl keys missing from FlatKV are NOT errors (FlatKV can start empty
// after a restart and only accumulates dual-written keys going forward).
//
// sampleLimit > 0 stops after that many rows and returns what was checked.
func RunForwardSubsetScan(
	ctx context.Context,
	evmStore sctypes.CommitKVStore,
	exporter sctypes.Exporter,
	sampleLimit int64,
) (ScanResult, error) {
	if evmStore == nil {
		return ScanResult{}, fmt.Errorf("nil memiavl evm store")
	}
	if exporter == nil {
		return ScanResult{}, fmt.Errorf("nil flatkv exporter")
	}

	var res ScanResult
	for {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if sampleLimit > 0 && res.RowsExamined >= sampleLimit {
			break
		}

		item, err := exporter.Next()
		if err != nil {
			if errors.Is(err, errorutils.ErrorExportDone) {
				break
			}
			return res, fmt.Errorf("exporter.Next: %w", err)
		}

		node, ok := item.(*sctypes.SnapshotNode)
		if !ok {
			return res, fmt.Errorf("unexpected exporter item type %T", item)
		}

		decoded, err := DecodeFlatKVNode(node)
		if err != nil {
			res.Mismatches = append(res.Mismatches, ScanMismatch{
				Kind: MismatchDecode,
				Key:  bytes.Clone(node.Key),
				Flat: bytes.Clone(node.Value),
			})
			continue
		}

		for _, kv := range decoded {
			res.RowsExamined++
			// The helper currently only decodes EVM-module rows into
			// memiavl-format keys scoped to the evm store; non-EVM
			// module rows are skipped by the decoder.
			mem := evmStore.Get(kv.Key)
			if mem == nil {
				res.Mismatches = append(res.Mismatches, ScanMismatch{
					Kind: MismatchMemiavlMissing,
					Key:  kv.Key,
					Flat: kv.Value,
				})
				continue
			}
			if !bytes.Equal(mem, kv.Value) {
				res.Mismatches = append(res.Mismatches, ScanMismatch{
					Kind:   MismatchValue,
					Key:    kv.Key,
					Flat:   kv.Value,
					MemVal: bytes.Clone(mem),
				})
			}
		}
	}

	return res, nil
}

// ForwardSubsetLatest is the convenience entry for Oracle 2: take the live
// flatkv store, open an exporter at `version`, and diff against evmStore.
// The caller provides `version` (normally the just-committed version).
func ForwardSubsetLatest(
	ctx context.Context,
	evmStore sctypes.CommitKVStore,
	flatkvStore flatkv.Store,
	version int64,
	sampleLimit int64,
) (ScanResult, error) {
	exp, err := flatkvStore.Exporter(version)
	if err != nil {
		return ScanResult{}, fmt.Errorf("open flatkv exporter at v=%d: %w", version, err)
	}
	defer func() { _ = exp.Close() }()

	return RunForwardSubsetScan(ctx, evmStore, exp, sampleLimit)
}
