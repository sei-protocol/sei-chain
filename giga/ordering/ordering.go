// Package ordering implements the SIP-6 canonical transaction ordering of one cut.
package ordering

import (
	"bytes"
	"cmp"
	"container/heap"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

var (
	// ErrInconsistentDuplicate reports two occurrences with one hash and different metadata.
	ErrInconsistentDuplicate = errors.New("duplicate hash with different metadata")
	// ErrMissingNonce reports a sender without a parent-state next nonce.
	ErrMissingNonce = errors.New("missing parent-state nonce")
)

// Tx is the ordering metadata of one transaction occurrence.
type Tx struct {
	Hash   common.Hash
	Sender common.Address
	Nonce  uint64
	// Tip is the effective tip against the block's base fee.
	Tip uint256.Int
}

// Order returns the indices of txs in canonical execution order. nextNonce
// returns the parent-state next nonce of a sender. The result does not depend
// on the order of txs.
func Order(txs []Tx, nextNonce func(common.Address) (uint64, bool)) ([]int, error) {
	unique, err := dedupe(txs)
	if err != nil {
		return nil, err
	}
	chains, err := senderChains(txs, resolveConflicts(txs, unique), nextNonce)
	if err != nil {
		return nil, err
	}
	return priorityOrder(txs, chains), nil
}

// dedupe returns one index per distinct hash.
func dedupe(txs []Tx) ([]int, error) {
	byHash := make(map[common.Hash]int, len(txs))
	for i, tx := range txs {
		if j, ok := byHash[tx.Hash]; !ok {
			byHash[tx.Hash] = i
		} else if txs[j] != tx {
			return nil, fmt.Errorf("%w: %v", ErrInconsistentDuplicate, tx.Hash)
		}
	}
	return slices.Collect(maps.Values(byHash)), nil
}

// resolveConflicts keeps the highest-ranked transaction of each (sender, nonce),
// grouped by sender.
func resolveConflicts(txs []Tx, unique []int) map[common.Address][]int {
	type slot struct {
		sender common.Address
		nonce  uint64
	}
	slots := make(map[slot]int, len(unique))
	for _, i := range unique {
		s := slot{txs[i].Sender, txs[i].Nonce}
		if j, ok := slots[s]; !ok || before(&txs[i], &txs[j]) {
			slots[s] = i
		}
	}
	bySender := map[common.Address][]int{}
	for s, i := range slots {
		bySender[s.sender] = append(bySender[s.sender], i)
	}
	return bySender
}

// senderChains returns, for each sender, its transactions from the parent-state
// next nonce up to the first nonce gap.
func senderChains(txs []Tx, bySender map[common.Address][]int, nextNonce func(common.Address) (uint64, bool)) ([][]int, error) {
	senders := slices.SortedFunc(maps.Keys(bySender), func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) })
	var chains [][]int
	for _, sender := range senders {
		next, ok := nextNonce(sender)
		if !ok {
			return nil, fmt.Errorf("%w: %v", ErrMissingNonce, sender)
		}
		chain := bySender[sender]
		slices.SortFunc(chain, func(a, b int) int { return cmp.Compare(txs[a].Nonce, txs[b].Nonce) })
		start := slices.IndexFunc(chain, func(i int) bool { return txs[i].Nonce >= next })
		if start < 0 {
			continue
		}
		chain = chain[start:]
		n := 0
		for n < len(chain) && txs[chain[n]].Nonce == next+uint64(n) {
			n++
		}
		if n > 0 {
			chains = append(chains, chain[:n])
		}
	}
	return chains, nil
}

// priorityOrder repeatedly emits the highest-ranked head among the sender chains.
func priorityOrder(txs []Tx, chains [][]int) []int {
	r := &ready{txs: txs, chains: chains}
	var out []int
	heap.Init(r)
	for r.Len() > 0 {
		chain := r.chains[0]
		out = append(out, chain[0])
		if len(chain) > 1 {
			r.chains[0] = chain[1:]
			heap.Fix(r, 0)
		} else {
			heap.Pop(r)
		}
	}
	return out
}

// before reports whether a ranks ahead of b: greater tip first, then lower hash.
func before(a, b *Tx) bool {
	if c := a.Tip.Cmp(&b.Tip); c != 0 {
		return c > 0
	}
	return bytes.Compare(a.Hash[:], b.Hash[:]) < 0
}

// ready is a heap of sender chains keyed by their first transaction.
type ready struct {
	txs    []Tx
	chains [][]int
}

func (r *ready) Len() int { return len(r.chains) }
func (r *ready) Less(i, j int) bool {
	return before(&r.txs[r.chains[i][0]], &r.txs[r.chains[j][0]])
}
func (r *ready) Swap(i, j int) { r.chains[i], r.chains[j] = r.chains[j], r.chains[i] }
func (r *ready) Push(x any)    { r.chains = append(r.chains, x.([]int)) }
func (r *ready) Pop() any {
	last := r.chains[len(r.chains)-1]
	r.chains = r.chains[:len(r.chains)-1]
	return last
}
