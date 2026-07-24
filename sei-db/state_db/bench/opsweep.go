package bench

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
)

// This file builds controlled write sets that isolate a single write
// operation (insert / update / delete) so the replay benchmark can measure
// the per-op storage cost asymmetry — the empirical input to the
// insert-vs-update-vs-delete pricing question (Jeremy's new-key premium, the
// delete-refund idea). v1 scope is the EVM storage key family; the generator
// takes a key family so account/code can be added without shape changes.

// OpKind is the write operation whose cost a generated write set isolates.
type OpKind string

const (
	// OpInsert writes keys that do not already exist (fresh slots).
	OpInsert OpKind = "insert"
	// OpUpdate overwrites keys that already exist (a warm-up block seeds them).
	OpUpdate OpKind = "update"
	// OpDelete deletes keys that already exist (a warm-up block seeds them).
	OpDelete OpKind = "delete"
)

// OpSweepSpec describes one controlled scenario.
type OpSweepSpec struct {
	Op OpKind
	// KeyKind is the write-set entry kind (WriteKindStorage/Code/... ). v1 uses
	// storage; the generator only supports fixed-shape families here.
	KeyKind string
	// ValueBytes is the value width for non-delete writes. For storage/codehash
	// it must be 32; for nonce, 8. Ignored for deletes.
	ValueBytes int
	// KeysPerBlock is the number of writes committed together in each block.
	KeysPerBlock int
	// TimedBlocks is the number of measured blocks (samples in the distribution).
	TimedBlocks int
}

// keyIndexStride keeps generated addresses/slots from colliding across blocks
// and scenarios: each write gets a globally unique index.
type keyCursor struct{ next uint64 }

func (c *keyCursor) take() uint64 {
	i := c.next
	c.next++
	return i
}

// GenerateOpWriteSet builds a write set that isolates spec.Op, plus the number
// of leading warm-up (untimed) blocks the caller must pass to
// ReplayWriteSetSampled.
//
//   - insert: TimedBlocks blocks of fresh keys, no warm-up (warmup=0).
//   - update: one warm-up block writing every key the timed blocks will
//     overwrite, then TimedBlocks blocks overwriting those same keys.
//   - delete: one warm-up block writing every key, then TimedBlocks blocks
//     deleting them.
//
// For update/delete the warm-up block is a single block containing all
// TimedBlocks*KeysPerBlock keys; each timed block then re-touches its own
// KeysPerBlock-sized slice of them, so a timed block operates entirely on
// pre-existing keys.
func GenerateOpWriteSet(spec OpSweepSpec) (*WriteSet, int, error) {
	if spec.KeysPerBlock <= 0 || spec.TimedBlocks <= 0 {
		return nil, 0, fmt.Errorf("keys-per-block and timed-blocks must be positive")
	}
	if spec.KeyKind != WriteKindStorage {
		return nil, 0, fmt.Errorf("op sweep v1 supports only %q keys, got %q", WriteKindStorage, spec.KeyKind)
	}
	if want := valueLenForKind(spec.KeyKind); want > 0 && spec.ValueBytes != want {
		return nil, 0, fmt.Errorf("%s value must be %d bytes, got %d", spec.KeyKind, want, spec.ValueBytes)
	}

	total := spec.KeysPerBlock * spec.TimedBlocks
	cursor := &keyCursor{}

	// Pre-generate the (address, slot) pairs the timed blocks will touch so
	// update/delete can seed exactly those keys in the warm-up block.
	type slotKey struct{ addr, slot string }
	touched := make([]slotKey, total)
	for i := range touched {
		idx := cursor.take()
		touched[i] = slotKey{addr: deterministicAddr(idx), slot: deterministicSlot(idx)}
	}

	value := hex.EncodeToString(make([]byte, spec.ValueBytes))

	mkWrite := func(k slotKey, del bool) WriteSetEntry {
		e := WriteSetEntry{Kind: WriteKindStorage, Address: k.addr, Slot: k.slot, Delete: del}
		if !del {
			e.Value = value
		}
		return e
	}

	var blocks []WriteSetBlock
	warmup := 0

	if spec.Op == OpUpdate || spec.Op == OpDelete {
		// One warm-up block seeds every key the timed blocks operate on.
		seed := make([]WriteSetEntry, total)
		for i, k := range touched {
			seed[i] = mkWrite(k, false)
		}
		blocks = append(blocks, WriteSetBlock{Writes: seed})
		warmup = 1
	}

	for b := 0; b < spec.TimedBlocks; b++ {
		writes := make([]WriteSetEntry, spec.KeysPerBlock)
		for j := 0; j < spec.KeysPerBlock; j++ {
			k := touched[b*spec.KeysPerBlock+j]
			switch spec.Op {
			case OpInsert, OpUpdate:
				writes[j] = mkWrite(k, false)
			case OpDelete:
				writes[j] = mkWrite(k, true)
			default:
				return nil, 0, fmt.Errorf("unknown op %q", spec.Op)
			}
		}
		blocks = append(blocks, WriteSetBlock{Writes: writes})
	}

	ws := &WriteSet{Module: "evm", Blocks: blocks}
	if err := ws.Validate(); err != nil {
		return nil, 0, fmt.Errorf("generated write set invalid: %w", err)
	}
	return ws, warmup, nil
}

// deterministicAddr derives a stable 20-byte EVM address hex from an index.
func deterministicAddr(idx uint64) string {
	var in [9]byte
	binary.LittleEndian.PutUint64(in[1:], idx)
	sum := sha256.Sum256(in[:])
	return hex.EncodeToString(sum[:20])
}

// deterministicSlot derives a stable 32-byte slot hex from an index.
func deterministicSlot(idx uint64) string {
	var in [9]byte
	in[0] = 1
	binary.LittleEndian.PutUint64(in[1:], idx)
	sum := sha256.Sum256(in[:])
	return hex.EncodeToString(sum[:])
}

// percentile returns the q-quantile (0..1) of samples using nearest-rank on a
// sorted copy. Returns 0 for an empty input.
func percentile(samples []float64, q float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := make([]float64, len(samples))
	copy(sorted, samples)
	sort.Float64s(sorted)
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}
