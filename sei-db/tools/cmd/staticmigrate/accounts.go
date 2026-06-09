package main

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// This file implements incremental account import for the static migration.
//
// Accounts dominate the EVM keyspace, but the flatKV import translator buffers
// every account fragment (nonce, codehash) and emits the merged rows only at
// Finalize() -- i.e. after the whole source has been read. That dumps all
// account writes + hashing into a silent tail at the very end of the import.
//
// memIAVL snapshot leaves are stored in ascending key order, and each EVM key
// kind has a distinct single-byte prefix, so the nonce leaves and codehash
// leaves each form a contiguous, address-sorted run. Because the nonce and
// codehash stripped keys are both the 20-byte account address, the two runs are
// sorted by the same order, so a merge-join by address reconstructs each
// account exactly once, streaming. We run several such merge-joins in parallel
// over disjoint address sub-regions (nonce is the primary keyspace; codehash
// regions are "attached") and feed the rows straight to the importer, so
// account writes overlap with the rest of the import instead of trailing it.

// accountPartition describes one reader pair's work as half-open LOCAL index
// ranges within the nonce range and the codehash range (0-based within each
// range). The address interval a pair owns is implicit in these ranges.
type accountPartition struct {
	nonceLo, nonceHi int
	codeLo, codeHi   int
}

// partitionAccounts splits the nonce keyspace (the primary, dense keyspace:
// most accounts have a nonce but no codehash) into up to n balanced, contiguous
// sub-ranges and attaches to each one the codehash sub-range covering the same
// half-open ADDRESS interval [addrLow, addrHigh):
//   - pair 0's addrLow is -inf, so codehash addresses below the first nonce
//     address are still owned by pair 0;
//   - pair k>0's addrLow is the first nonce address in nonce sub-range k;
//   - pair k's addrHigh is pair k+1's addrLow; the last pair's addrHigh is +inf.
//
// Because consecutive pairs share a boundary address, the per-pair codehash
// sub-ranges tile [0, nCode) exactly, and the nonce sub-ranges tile [0, nNonce)
// exactly. Hence every nonce address AND every codehash address -- including
// codehash-only addresses outside the nonce address span -- is owned by exactly
// one pair (complete, disjoint).
//
// nonceAddr / codeAddr return the 20-byte address at a LOCAL index into the
// nonce / codehash range; both index sequences must be strictly ascending by
// address. The function is pure (no snapshot dependency) so it can be property-
// tested directly.
//
// Degenerate cases: nNonce == 0 collapses to a single pair streaming the whole
// codehash range (codehash-only accounts); nCode == 0 yields nonce-only pairs;
// n is clamped to [1, max(1, nNonce)].
func partitionAccounts(nNonce, nCode, n int, nonceAddr, codeAddr func(i int) []byte) []accountPartition {
	if n < 1 {
		n = 1
	}
	if nNonce == 0 {
		if nCode == 0 {
			return nil
		}
		// No primary keyspace: a single zipper over the codehash range alone.
		return []accountPartition{{nonceLo: 0, nonceHi: 0, codeLo: 0, codeHi: nCode}}
	}
	if n > nNonce {
		n = nNonce
	}

	// lowerBound: first codehash index whose address is >= target. The codehash
	// range is address-sorted, so sort.Search gives the half-open boundary.
	lowerBound := func(target []byte) int {
		return sort.Search(nCode, func(i int) bool {
			return bytes.Compare(codeAddr(i), target) >= 0
		})
	}

	parts := make([]accountPartition, 0, n)
	for k := 0; k < n; k++ {
		nLo := k * nNonce / n
		nHi := (k + 1) * nNonce / n
		// addrLow = -inf for the first pair (codeLo 0), else the pair's first
		// nonce address. addrHigh = +inf for the last pair (codeHi nCode), else
		// the next pair's first nonce address (== this pair's addrHigh).
		codeLo := 0
		if k > 0 {
			codeLo = lowerBound(nonceAddr(nLo))
		}
		codeHi := nCode
		if k < n-1 {
			codeHi = lowerBound(nonceAddr(nHi))
		}
		parts = append(parts, accountPartition{nonceLo: nLo, nonceHi: nHi, codeLo: codeLo, codeHi: codeHi})
	}
	return parts
}

// zipRange merge-joins one pair's nonce sub-range [nStart,nEnd) with its
// codehash sub-range [cStart,cEnd) by address and calls emit once per account.
// leafN / leafC return the (key, value) of the leaf at an ABSOLUTE index in the
// nonce / codehash range. emit receives the 20-byte address, the raw nonce
// value (nil if the account has no nonce), the raw codehash value (nil if
// none), and the number of memIAVL leaves consumed for that account (1 or 2).
//
// It asserts each leaf's kind and address length (failing loudly on an on-disk
// layout surprise) and that each run is strictly ascending by address (catching
// an unsorted snapshot or a partition bug). It is pure aside from the injected
// accessors and emit, so it is unit-testable against a reference merge.
func zipRange(
	nStart, nEnd int, leafN func(i int) (key, value []byte),
	cStart, cEnd int, leafC func(i int) (key, value []byte),
	emit func(addr, nonceVal, codeVal []byte, leaves int) error,
) error {
	ni, ci := nStart, cStart
	var (
		nAddr, nVal  []byte
		cAddr, cVal  []byte
		haveN, haveC bool
		prevN, prevC []byte
	)

	advanceN := func() error {
		if ni >= nEnd {
			haveN = false
			return nil
		}
		k, v := leafN(ni)
		kind, addr := keys.ParseEVMKey(k)
		if kind != keys.EVMKeyNonce || len(addr) != keys.AddressLen {
			return fmt.Errorf("nonce-range leaf %d is not a nonce key (kind=%d, addrlen=%d)", ni, kind, len(addr))
		}
		if prevN != nil && bytes.Compare(addr, prevN) <= 0 {
			return fmt.Errorf("nonce range not strictly ascending at leaf %d", ni)
		}
		nAddr, nVal, haveN, prevN = addr, v, true, addr
		ni++
		return nil
	}
	advanceC := func() error {
		if ci >= cEnd {
			haveC = false
			return nil
		}
		k, v := leafC(ci)
		kind, addr := keys.ParseEVMKey(k)
		if kind != keys.EVMKeyCodeHash || len(addr) != keys.AddressLen {
			return fmt.Errorf("codehash-range leaf %d is not a codehash key (kind=%d, addrlen=%d)", ci, kind, len(addr))
		}
		if prevC != nil && bytes.Compare(addr, prevC) <= 0 {
			return fmt.Errorf("codehash range not strictly ascending at leaf %d", ci)
		}
		cAddr, cVal, haveC, prevC = addr, v, true, addr
		ci++
		return nil
	}

	if err := advanceN(); err != nil {
		return err
	}
	if err := advanceC(); err != nil {
		return err
	}

	for haveN || haveC {
		switch {
		case haveN && (!haveC || bytes.Compare(nAddr, cAddr) < 0):
			if err := emit(nAddr, nVal, nil, 1); err != nil {
				return err
			}
			if err := advanceN(); err != nil {
				return err
			}
		case haveC && (!haveN || bytes.Compare(nAddr, cAddr) > 0):
			if err := emit(cAddr, nil, cVal, 1); err != nil {
				return err
			}
			if err := advanceC(); err != nil {
				return err
			}
		default: // addresses equal: one account with both fields
			if err := emit(nAddr, nVal, cVal, 2); err != nil {
				return err
			}
			if err := advanceN(); err != nil {
				return err
			}
			if err := advanceC(); err != nil {
				return err
			}
		}
	}
	return nil
}

// kindLeafRange returns the half-open leaf-index range [lo,hi) of every leaf
// whose key starts with the given EVM kind's single-byte prefix. Leaves are
// stored in ascending key order, so each prefix byte occupies one contiguous
// run located in O(log n) by binary search. Returns lo==hi when the kind has no
// leaves.
func kindLeafRange(snap *memiavl.Snapshot, kind keys.EVMKeyKind) (lo, hi int, err error) {
	prefix, ok := keys.EVMKeyPrefixByte(kind)
	if !ok {
		return 0, 0, fmt.Errorf("evm kind %d has no fixed prefix byte", kind)
	}
	n := snap.LeavesLen()
	lo = sort.Search(n, func(i int) bool {
		k := snap.LeafKey(uint32(i)) //nolint:gosec // leaf count fits in uint32
		return len(k) > 0 && k[0] >= prefix
	})
	hi = sort.Search(n, func(i int) bool {
		k := snap.LeafKey(uint32(i)) //nolint:gosec // leaf count fits in uint32
		return len(k) > 0 && k[0] > prefix
	})
	return lo, hi, nil
}

// complementIntervals returns the sorted half-open intervals of [0,total) that
// are not covered by any of the given (disjoint) excluded intervals. Empty
// excluded intervals are ignored. Used to scan everything except the two
// account (nonce, codehash) ranges.
func complementIntervals(total int, excluded [][2]int) [][2]int {
	ex := make([][2]int, 0, len(excluded))
	for _, e := range excluded {
		if e[1] > e[0] {
			ex = append(ex, e)
		}
	}
	sort.Slice(ex, func(i, j int) bool { return ex[i][0] < ex[j][0] })

	var out [][2]int
	cur := 0
	for _, e := range ex {
		if e[0] > cur {
			out = append(out, [2]int{cur, e[0]})
		}
		if e[1] > cur {
			cur = e[1]
		}
	}
	if cur < total {
		out = append(out, [2]int{cur, total})
	}
	return out
}

// splitIntervals divides the concatenated length of the given sorted, disjoint
// intervals into up to parts balanced chunks, returning for each chunk the list
// of sub-intervals it should scan. Used to fan the complement of the account
// ranges across the reader goroutines.
func splitIntervals(intervals [][2]int, parts int) [][][2]int {
	total := 0
	for _, iv := range intervals {
		total += iv[1] - iv[0]
	}
	if total == 0 || parts <= 0 {
		return nil
	}
	if parts > total {
		parts = total
	}
	out := make([][][2]int, 0, parts)
	for p := 0; p < parts; p++ {
		out = append(out, sliceVirtual(intervals, p*total/parts, (p+1)*total/parts))
	}
	return out
}

// sliceVirtual maps the half-open virtual offset range [vStart,vEnd) -- an
// offset into the concatenation of intervals -- back to actual leaf-index
// sub-intervals.
func sliceVirtual(intervals [][2]int, vStart, vEnd int) [][2]int {
	var out [][2]int
	base := 0
	for _, iv := range intervals {
		n := iv[1] - iv[0]
		lo := max(vStart, base)
		hi := min(vEnd, base+n)
		if lo < hi {
			out = append(out, [2]int{iv[0] + (lo - base), iv[0] + (hi - base)})
		}
		base += n
	}
	return out
}

// accountLeafRanges locates the nonce and codehash leaf-index ranges in the evm
// snapshot. It is cheap (two binary searches) and is called up front so the
// caller can start scanning the complement of these ranges immediately while
// the account producers run concurrently.
func accountLeafRanges(snap *memiavl.Snapshot) (nonceRange, codeRange [2]int, err error) {
	nLo, nHi, nerr := kindLeafRange(snap, keys.EVMKeyNonce)
	if nerr != nil {
		return [2]int{}, [2]int{}, nerr
	}
	cLo, cHi, cerr := kindLeafRange(snap, keys.EVMKeyCodeHash)
	if cerr != nil {
		return [2]int{}, [2]int{}, cerr
	}
	return [2]int{nLo, nHi}, [2]int{cLo, cHi}, nil
}

// runAccountProducers merge-joins the given nonce and codehash leaf ranges into
// complete account rows and feeds them to the importer, running up to np
// parallel reader pairs over disjoint address sub-regions. visited is
// incremented by the number of memIAVL account leaves consumed (so the progress
// meter advances through accounts) and rows by the number of account rows
// emitted. It blocks until all pairs finish (run it in its own goroutine to
// overlap with the complement scan) and returns the first error from any pair.
//
// done lets the caller abort all pairs (e.g. on an importer error elsewhere).
func runAccountProducers(
	snap *memiavl.Snapshot,
	importer sctypes.Importer,
	height int64,
	np int,
	nonceRange, codeRange [2]int,
	visited *atomic.Uint64,
	rows *atomic.Int64,
	done <-chan struct{},
) error {
	nLo, nHi := nonceRange[0], nonceRange[1]
	cLo, cHi := codeRange[0], codeRange[1]
	nNonce, nCode := nHi-nLo, cHi-cLo
	if nNonce == 0 && nCode == 0 {
		return nil
	}
	if np < 1 {
		np = 1
	}

	nonceAddr := func(i int) []byte {
		_, addr := keys.ParseEVMKey(snap.LeafKey(uint32(nLo + i))) //nolint:gosec
		return addr
	}
	codeAddr := func(i int) []byte {
		_, addr := keys.ParseEVMKey(snap.LeafKey(uint32(cLo + i))) //nolint:gosec
		return addr
	}
	parts := partitionAccounts(nNonce, nCode, np, nonceAddr, codeAddr)

	leafN := func(i int) ([]byte, []byte) { return snap.LeafKeyValue(uint32(i)) } //nolint:gosec
	leafC := leafN

	emit := func(addr, nonceVal, codeVal []byte, leaves int) error {
		pair, ok, eerr := flatkv.EncodeImportAccount(addr, nonceVal, codeVal, height)
		if eerr != nil {
			return eerr
		}
		// Always count the consumed leaves so the meter tracks progress, even
		// for all-zero accounts the buffered path would drop (ok == false).
		visited.Add(uint64(leaves)) //nolint:gosec
		if !ok {
			return nil
		}
		select {
		case <-done:
			return errScanStopped
		default:
		}
		importer.AddNode(&sctypes.SnapshotNode{Key: pair.Key, Value: pair.Value, Version: height, Height: 0})
		rows.Add(1)
		return nil
	}

	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	setErr := func(e error) {
		if e == nil {
			return
		}
		errOnce.Do(func() { firstErr = e })
	}

	for _, p := range parts {
		nStart, nEnd := nLo+p.nonceLo, nLo+p.nonceHi
		cStart, cEnd := cLo+p.codeLo, cLo+p.codeHi
		wg.Add(1)
		go func(nStart, nEnd, cStart, cEnd int) {
			defer wg.Done()
			if e := zipRange(nStart, nEnd, leafN, cStart, cEnd, leafC, emit); e != nil && e != errScanStopped {
				setErr(e)
			}
		}(nStart, nEnd, cStart, cEnd)
	}
	wg.Wait()
	return firstErr
}
