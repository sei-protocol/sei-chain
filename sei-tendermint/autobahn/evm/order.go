package evm

import (
	"cmp"
	"encoding/binary"
	"math/big"
	"math/bits"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

/*
Canonical Transaction Ordering
A `CommitQC` finalizes a cut of lane blocks. Implementations MAY enumerate the cut's headers in a separate deterministic lane and block order for certificate verification, payload retrieval, persistence, and block identifiers. That enumeration is not the canonical EVM transaction order. Lane position, lane local transaction order, validator index, leader ordering, and payload arrival time MUST NOT be used as hidden transaction ordering tie breakers. Let T(C) be the multiset of transaction occurrences in all newly committed lane blocks of cut C. Every correct node MUST derive the same canonical transaction sequence from T(C) by the following procedure. An implementation is available at [BM26].

1. Normalize metadata. For each prevalidated transaction occurrence, derive its transaction hash, recovered sender, nonce, and effective priority tip using the common fee context for the ordering batch. For a valid EIP-1559 transaction, the effective tip is:

	min(max_priority_fee_per_gas, max_fee_per_gas - base_fee)

	Fee validity MUST be checked before the subtraction. The ordering input MUST also include the canonical parent state's next executable nonce for every represented sender. Missing or inconsistent metadata is invalid input and MUST NOT be resolved using local observation.

2. Remove exact duplicates. If the same transaction hash occurs more than once, retain one canonical occurrence. Equal hashes MUST have identical canonical metadata under a collision resistant hash function, otherwise the ordering batch is invalid. Because later ordering does not depend on lane identity, selecting one occurrence MUST NOT affect execution order.

3. Resolve nonce slot conflicts. Distinct transaction hashes with the same `(sender, nonce)` are replacement conflicts, not exact duplicates. Retain the transaction with the greater effective tip, if effective tips are equal, retain the transaction with the lexicographically lower hash. This rule MUST be applied identically by every validator.

4. Apply parent state nonces. For each sender, discard transactions whose nonce is lower than the sender's next executable nonce in the canonical parent state. Starting at that next nonce, retain only the contiguous prefix of populated nonce slots. At the first gap, exclude every later transaction from that sender from this ordering batch.

5. Construct nonce dependencies. For every retained sender prefix, transaction nonce n + 1 depends on transaction nonce n. These per-sender chains are the complete dependency graph for this ordering rule.

6. Run priority Kahn ordering. Initialize the ready set with the first transaction in each non-empty sender chain. Repeatedly choose the ready transaction with the greatest effective tip, breaking an equal tip tie by the lexicographically lower transaction hash. Emit it, add its immediate same sender successor to the ready set if one exists, and repeat until the ready set is empty.

The result is the priority maximal topological order of the retained transactions. Step 6 is a dynamic ready set algorithm, not a sort of static topological layers, a newly unlocked high tip successor can execute before a lower tip transaction that was already ready from another sender. For example, if sender A has (nonce 0, tip 5) followed by (nonce 1, tip 1), while sender B has (nonce 0, tip 4) followed by (nonce 1, tip 10), the tip sequence is `5, 4, 10, 1`.
*/
type CanonicalOrder struct {
	BaseFee  *big.Int
	GetNonce func(common.Address) uint64
	Txs      []EvmTx
}

// nTipHash orders tips descending and transaction hashes ascending.
type nTipHash [8]uint64

func newNTipHash(tip uint256.Int, hash common.Hash) nTipHash {
	// Complementing the tip reverses unsigned order, including zero, without carries.
	return nTipHash{
		^tip[3], ^tip[2], ^tip[1], ^tip[0],
		binary.BigEndian.Uint64(hash[0:8]),
		binary.BigEndian.Uint64(hash[8:16]),
		binary.BigEndian.Uint64(hash[16:24]),
		binary.BigEndian.Uint64(hash[24:32]),
	}
}

func (a nTipHash) Cmp(b nTipHash) int {
	for i := range a {
		if a[i] != b[i] {
			_, borrow := bits.Sub64(a[i], b[i], 0)
			return 1 - 2*int(borrow)
		}
	}
	return 0
}

func (o CanonicalOrder) Run() []EvmTx {
	keys := make([]nTipHash, len(o.Txs))
	ord := make([]int, 0, len(o.Txs))
	for i, tx := range o.Txs {
		tip, ok := tx.EffectiveGasTip(o.BaseFee)
		if !ok {
			continue
		}
		keys[i] = newNTipHash(tip, tx.Hash())
		ord = append(ord, i)
	}
	// Sort by (nonce,-tip,hash).
	slices.SortFunc(ord, func(a, b int) int {
		if x := cmp.Compare(o.Txs[a].Nonce(), o.Txs[b].Nonce()); x != 0 {
			return x
		}
		return keys[a].Cmp(keys[b])
	})
	// Retain contiguous sender prefixes and clamp each key to its dependencies.
	last := map[common.Address]int{}
	next := 0
	for _, i := range ord {
		s := o.Txs[i].Sender()
		if j, ok := last[s]; ok {
			if o.Txs[j].Nonce()+1 != o.Txs[i].Nonce() {
				continue
			}
			if keys[i].Cmp(keys[j]) < 0 {
				keys[i] = keys[j]
			}
		} else if o.GetNonce(s) != o.Txs[i].Nonce() {
			continue
		}
		last[s] = i
		ord[next] = i
		next++
	}
	ord = ord[:next]
	// Stable sorting preserves nonce order among transactions sharing a clamped key.
	slices.SortStableFunc(ord, func(a, b int) int {
		return keys[a].Cmp(keys[b])
	})
	txs := make([]EvmTx, next)
	for pos, i := range ord {
		txs[pos] = o.Txs[i]
	}
	return txs
}
