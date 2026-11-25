package evm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// referenceCanonicalOrder implements the documented nonce-slot selection and priority Kahn walk.
func referenceCanonicalOrder(o CanonicalOrder) []EvmTx {
	slots := map[common.Address]map[uint64]orderEntry{}
	seen := map[common.Hash]struct{}{}
	for _, tx := range o.Txs {
		tip, ok := tx.EffectiveGasTip(o.BaseFee)
		if !ok {
			continue
		}
		entry := orderEntry{tx: tx, tip: tip, hash: tx.Hash()}
		if _, duplicate := seen[entry.hash]; duplicate {
			continue
		}
		seen[entry.hash] = struct{}{}
		sender := tx.Sender()
		if slots[sender] == nil {
			slots[sender] = map[uint64]orderEntry{}
		}
		previous, exists := slots[sender][tx.Nonce()]
		if !exists || entry.before(previous) {
			slots[sender][tx.Nonce()] = entry
		}
	}
	ready := orderReadyHeap{}
	for sender, chain := range slots {
		if head, ok := chain[o.GetNonce(sender)]; ok {
			ready.push(head)
		}
	}
	ordered := make([]EvmTx, 0, len(o.Txs))
	for len(ready) > 0 {
		entry := ready.pop()
		ordered = append(ordered, entry.tx)
		if entry.tx.Nonce() == math.MaxUint64 {
			continue
		}
		if successor, ok := slots[entry.tx.Sender()][entry.tx.Nonce()+1]; ok {
			ready.push(successor)
		}
	}
	return ordered
}

type orderEntry struct {
	tx   EvmTx
	tip  uint256.Int
	hash common.Hash
}

func (a orderEntry) before(b orderEntry) bool {
	if comparison := a.tip.Cmp(&b.tip); comparison != 0 {
		return comparison > 0
	}
	return bytes.Compare(a.hash[:], b.hash[:]) < 0
}

// orderReadyHeap is a typed binary heap of the currently executable sender heads.
type orderReadyHeap []orderEntry

func (h *orderReadyHeap) push(entry orderEntry) {
	*h = append(*h, entry)
	for i := len(*h) - 1; i > 0; {
		parent := (i - 1) / 2
		if !(*h)[i].before((*h)[parent]) {
			break
		}
		(*h)[i], (*h)[parent] = (*h)[parent], (*h)[i]
		i = parent
	}
}

func (h *orderReadyHeap) pop() orderEntry {
	first := (*h)[0]
	last := len(*h) - 1
	(*h)[0] = (*h)[last]
	(*h)[last] = orderEntry{}
	*h = (*h)[:last]
	for i := 0; 2*i+1 < len(*h); {
		child := 2*i + 1
		if child+1 < len(*h) && (*h)[child+1].before((*h)[child]) {
			child++
		}
		if !(*h)[child].before((*h)[i]) {
			break
		}
		(*h)[i], (*h)[child] = (*h)[child], (*h)[i]
		i = child
	}
	return first
}

func orderTestTx(sender common.Address, nonce uint64, tip, feeCap int64, variant uint64) EvmTx {
	return TestEvmTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID: big.NewInt(testChainID), Nonce: nonce, Gas: 21_000,
		GasTipCap: big.NewInt(tip), GasFeeCap: big.NewInt(feeCap),
		Data: binary.BigEndian.AppendUint64(nil, variant),
	}), sender)
}

func orderHashes(txs []EvmTx) []common.Hash {
	hashes := make([]common.Hash, len(txs))
	for i, tx := range txs {
		hashes[i] = tx.Hash()
	}
	return hashes
}

func TestCanonicalOrder(t *testing.T) {
	a, b := testAddress(1), testAddress(2)
	a0 := orderTestTx(a, 0, 5, 200, 0)
	a1 := orderTestTx(a, 1, 1, 200, 0)
	b0 := orderTestTx(b, 0, 4, 200, 1)
	b1 := orderTestTx(b, 1, 10, 200, 1)
	tie := []EvmTx{a0, orderTestTx(b, 0, 5, 200, 1)}
	slices.SortFunc(tie, func(a, b EvmTx) int { return a.Hash().Cmp(b.Hash()) })
	lowHash, highHash := tie[0], tie[1]
	successor := orderTestTx(lowHash.Sender(), 1, 100, 200, 2)
	replacements := []EvmTx{a0, orderTestTx(a, 0, 5, 200, 3)}
	slices.SortFunc(replacements, func(a, b EvmTx) int { return a.Hash().Cmp(b.Hash()) })
	atParent := orderTestTx(a, 5, 5, 200, 0)
	afterGap := orderTestTx(a, 7, 20, 200, 0)
	negative := orderTestTx(a, 0, 5, 9, 0)
	zero := orderTestTx(a, 0, 5, 10, 0)
	capped := orderTestTx(a, 0, 100, 12, 0)
	uncapped := orderTestTx(b, 0, 3, 200, 0)
	maxNonce := orderTestTx(a, math.MaxUint64, 5, 200, 0)
	cases := []struct {
		name   string
		txs    []EvmTx
		parent uint64
		want   []EvmTx
	}{
		{name: "empty"},
		{name: "dynamic ready set", txs: []EvmTx{a1, b1, b0, a0}, want: []EvmTx{a0, b0, b1, a1}},
		{name: "exact duplicates", txs: []EvmTx{a1, a0, a0, a1}, want: []EvmTx{a0, a1}},
		{name: "higher tip replacement", txs: []EvmTx{a0, orderTestTx(a, 0, 3, 200, 1), a1}, want: []EvmTx{a0, a1}},
		{name: "equal tip replacement hash", txs: []EvmTx{replacements[1], replacements[0], a1}, want: []EvmTx{replacements[0], a1}},
		{name: "ready hash tie", txs: []EvmTx{highHash, lowHash}, want: []EvmTx{lowHash, highHash}},
		{name: "unlocked priority after tied heads", txs: []EvmTx{lowHash, highHash, successor}, want: []EvmTx{lowHash, successor, highHash}},
		{name: "parent nonce and gap", parent: 5, txs: []EvmTx{a0, afterGap, atParent}, want: []EvmTx{atParent}},
		{name: "missing parent nonce", parent: 2, txs: []EvmTx{a0, a1}},
		{name: "negative tip breaks chain", txs: []EvmTx{negative, a1}},
		{name: "zero tip accepted", txs: []EvmTx{zero}, want: []EvmTx{zero}},
		{name: "effective tip uses fee cap", txs: []EvmTx{capped, uncapped}, want: []EvmTx{uncapped, capped}},
		{name: "maximum nonce", parent: math.MaxUint64, txs: []EvmTx{maxNonce, maxNonce, a0}, want: []EvmTx{maxNonce}},
	}
	for _, algorithm := range []struct {
		name string
		run  func(CanonicalOrder) []EvmTx
	}{
		{"sort", CanonicalOrder.Run},
		{"kahn", referenceCanonicalOrder},
	} {
		t.Run(algorithm.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					input := slices.Clone(tc.txs)
					baseFee := big.NewInt(10)
					order := CanonicalOrder{BaseFee: baseFee, Txs: input, GetNonce: func(common.Address) uint64 { return tc.parent }}
					got := algorithm.run(order)
					require.Equal(t, orderHashes(tc.want), orderHashes(got))
					require.Equal(t, orderHashes(tc.txs), orderHashes(input), "input order changed")
					require.Equal(t, int64(10), baseFee.Int64(), "base fee changed")
				})
			}
		})
	}
}

func TestCanonicalOrderPermutationInvariant(t *testing.T) {
	for _, algorithm := range []struct {
		name string
		run  func(CanonicalOrder) []EvmTx
	}{
		{"sort", CanonicalOrder.Run},
		{"kahn", referenceCanonicalOrder},
	} {
		t.Run(algorithm.name, func(t *testing.T) {
			rng := utils.TestRng()
			input := make([]EvmTx, 0, 8)
			for sender := range 4 {
				address := common.BigToAddress(big.NewInt(int64(sender + 1)))
				input = append(input, orderTestTx(address, 0, 5, 100, uint64(sender)), orderTestTx(address, 1, 50, 100, uint64(sender))) //nolint:gosec // sender is in [0,4).
			}
			order := CanonicalOrder{BaseFee: big.NewInt(10), GetNonce: func(common.Address) uint64 { return 0 }, Txs: input}
			want := orderHashes(referenceCanonicalOrder(order))
			for permutation := range 32 {
				shuffled := slices.Clone(input)
				utils.Shuffle(rng, shuffled)
				order.Txs = shuffled
				require.Equal(t, want, orderHashes(algorithm.run(order)), "permutation %d", permutation)
			}
		})
	}
}

func TestCanonicalOrderMatchesReference(t *testing.T) {
	for _, algorithm := range []struct {
		name string
		run  func(CanonicalOrder) []EvmTx
	}{
		{"sort", CanonicalOrder.Run},
		{"kahn", referenceCanonicalOrder},
	} {
		t.Run(algorithm.name, func(t *testing.T) {
			rng := utils.TestRng()
			for batch := range 100 {
				parents := map[common.Address]uint64{}
				var txs []EvmTx
				for sender := range 8 {
					address := common.BigToAddress(big.NewInt(int64(sender + 1)))
					parent := uint64(rng.Intn(3)) //nolint:gosec // Intn(3) is non-negative.
					parents[address] = parent
					for nonce := range uint64(8) {
						if rng.Intn(5) == 0 {
							continue
						}
						tx := orderTestTx(address, nonce, rng.Int63n(8), rng.Int63n(20), rng.Uint64())
						txs = append(txs, tx)
						if rng.Intn(3) == 0 {
							txs = append(txs, tx)
						}
						if rng.Intn(3) == 0 {
							txs = append(txs, orderTestTx(address, nonce, rng.Int63n(8), 20, rng.Uint64()))
						}
					}
				}
				utils.Shuffle(rng, txs)
				order := CanonicalOrder{BaseFee: big.NewInt(10), GetNonce: func(a common.Address) uint64 { return parents[a] }, Txs: txs}
				got := algorithm.run(order)
				assertCanonicalOrderProperties(t, order, got)
				require.Equal(t, orderHashes(referenceCanonicalOrder(order)), orderHashes(got), "batch %d", batch)
			}
		})
	}
}

func assertCanonicalOrderProperties(t *testing.T, o CanonicalOrder, ordered []EvmTx) {
	t.Helper()
	seen := map[common.Hash]bool{}
	members := map[common.Hash]bool{}
	next := map[common.Address]uint64{}
	exhausted := map[common.Address]bool{}
	for _, tx := range o.Txs {
		members[tx.Hash()] = true
		next[tx.Sender()] = o.GetNonce(tx.Sender())
	}
	for _, tx := range ordered {
		require.True(t, members[tx.Hash()], "transaction was not in the input")
		require.False(t, seen[tx.Hash()], "duplicate transaction")
		seen[tx.Hash()] = true
		sender := tx.Sender()
		require.False(t, exhausted[sender], "nonce overflow")
		require.Equal(t, next[sender], tx.Nonce(), "sender nonces must start at the parent and be contiguous")
		chosenTip := tx.EffectiveGasTipValue(o.BaseFee)
		require.GreaterOrEqual(t, chosenTip.Sign(), 0)
		// Scan the input independently of the heap to check the best currently executable slot.
		for _, candidate := range o.Txs {
			if exhausted[candidate.Sender()] || candidate.Nonce() != next[candidate.Sender()] {
				continue
			}
			otherTip := candidate.EffectiveGasTipValue(o.BaseFee)
			if otherTip.Sign() >= 0 {
				comparison := otherTip.Cmp(chosenTip)
				better := comparison > 0 || (comparison == 0 && candidate.Hash().Cmp(tx.Hash()) < 0)
				require.False(t, better, "a higher-priority transaction was ready")
			}
		}
		if tx.Nonce() == math.MaxUint64 {
			exhausted[sender] = true
		} else {
			next[sender] = tx.Nonce() + 1
		}
	}
	for _, tx := range o.Txs {
		if !exhausted[tx.Sender()] && tx.Nonce() == next[tx.Sender()] {
			require.Less(t, tx.EffectiveGasTipValue(o.BaseFee).Sign(), 0, "an executable transaction was omitted")
		}
	}
}

// BenchmarkCanonicalOrder compares ordering 10,000 shuffled, unique transactions
// with contiguous sender nonces starting at zero and valid fees. It excludes
// duplicates, replacement conflicts, stale nonces, and nonce gaps. Transaction
// hashes are cached before timing; tip calculation and key construction are timed.
func BenchmarkCanonicalOrder(b *testing.B) {
	for _, senders := range []int{1, 100, 1_000, 10_000} {
		b.Run(fmt.Sprintf("10000tx/%dsenders", senders), func(b *testing.B) {
			rng := utils.TestRng()
			txs := make([]EvmTx, 10_000)
			for i := range txs {
				address := common.BigToAddress(big.NewInt(int64(i%senders + 1)))
				txs[i] = orderTestTx(address, uint64(i/senders), rng.Int63n(1_000), 2_000, uint64(i)) //nolint:gosec // i and senders are bounded and non-negative.
				_ = txs[i].Hash()                                                                     // Sender recovery and hash computation precede ordering in the node.
			}
			utils.Shuffle(rng, txs)
			order := CanonicalOrder{BaseFee: big.NewInt(10), GetNonce: func(common.Address) uint64 { return 0 }, Txs: txs}
			for _, algorithm := range []struct {
				name string
				run  func(CanonicalOrder) []EvmTx
			}{
				{"sort", CanonicalOrder.Run},
				{"kahn", referenceCanonicalOrder},
			} {
				b.Run(algorithm.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						ordered := algorithm.run(order)
						if len(ordered) != len(txs) {
							b.Fatalf("got %d transactions, want %d", len(ordered), len(txs))
						}
					}
				})
			}
		})
	}
}
