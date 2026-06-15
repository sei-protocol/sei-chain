package receipt

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// littTagIndex is the "littidx" filtering strategy: litt holds the receipt
// bodies (point lookup by tx hash), and this pebble index holds one
// empty-valued key per log tag so FilterLogs can locate matching receipts
// without scanning whole blocks. It is Cody's lookup-index design with the
// body store split out to litt:
//
//	't' + block (u64 BE) + kind (1B) + tag (20B addr / 32B topic)
//	    + txIndex (u32 BE) + firstLogIndex (u32 BE) + txHash (32B)  -> nil
//
// A query seeks the (block, tag) entries, intersects candidates across
// criteria dimensions, then point-reads only the matching receipts from litt
// by the tx hash carried in the key — it never decodes non-matching
// receipts, where littdb's bloom strategy decodes every receipt of every
// candidate block. GC is a bounded range delete over the block range.
//
// The tx hash lives in the key (rather than a txIndex resolved through a
// separate map) so a candidate is one litt point lookup with no extra
// indirection; litt's keymap load is unchanged from littdb. The kind byte
// keeps address and per-topic-position keyspaces disjoint (criteria are
// positional). firstLogIndex is the receipt's block-wide first log index,
// stored so reads number logs without decoding the preceding receipts; for a
// block written across several SetReceipts calls (legacy migration subsets)
// numbering restarts per subset, matching littdb's bloom path.
type littTagIndex struct{}

const (
	littTagKeyPrefix = 't'

	littAddrTagLen  = common.AddressLength // 20
	littTopicTagLen = common.HashLength    // 32
	// txIndex(4) + firstLogIndex(4) + txHash(32)
	littTagSuffixLen = ledgerTxIndexLen + 4 + common.HashLength
	littTagKeyMaxLen = 1 + blockNumLen + 1 + littTopicTagLen + littTagSuffixLen
)

// littTagBlockKey is the lower bound for every tag key of a block (also the
// exclusive upper bound for block-1's keys).
func littTagBlockKey(blockNumber uint64) []byte {
	key := make([]byte, 1+blockNumLen)
	key[0] = littTagKeyPrefix
	binary.BigEndian.PutUint64(key[1:], blockNumber)
	return key
}

// littTagKindKey bounds a single (block, kind) keyspace.
func littTagKindKey(blockNumber uint64, kind byte) []byte {
	return append(littTagBlockKey(blockNumber), kind)
}

// appendLittTagKey writes a full tag key into dst[:0], reusing its capacity so
// the write path allocates no per-key slice (pebble's batch.Set copies the
// key, so the buffer is free to reuse immediately after).
func appendLittTagKey(dst []byte, blockNumber uint64, kind byte, tag []byte, txIndex, firstLogIndex uint32, txHash common.Hash) []byte {
	dst = append(dst, littTagKeyPrefix)
	dst = binary.BigEndian.AppendUint64(dst, blockNumber)
	dst = append(dst, kind)
	dst = append(dst, tag...)
	dst = binary.BigEndian.AppendUint32(dst, txIndex)
	dst = binary.BigEndian.AppendUint32(dst, firstLogIndex)
	return append(dst, txHash[:]...)
}

// stageBlock writes the tag keys for every log in the block (records already
// sorted by transaction index). Values are nil — all information is in the
// key — and re-staging the same data (crash replay) is idempotent.
func (littTagIndex) stageBlock(_ *littReceiptStore, batch dbtypes.Batch, blockNumber uint64, records []ReceiptRecord) error {
	scratch := make([]byte, 0, littTagKeyMaxLen)
	firstLogIndex := uint32(0)
	for _, record := range records {
		txIndex := record.Receipt.TransactionIndex
		txHash := record.TxHash
		for _, lg := range record.Receipt.Logs {
			addr := common.HexToAddress(lg.Address)
			scratch = appendLittTagKey(scratch[:0], blockNumber, tagKindAddress, addr[:], txIndex, firstLogIndex, txHash)
			if err := batch.Set(scratch, nil); err != nil {
				return err
			}
			for p, topic := range lg.Topics {
				if p >= maxIndexedTopics {
					break
				}
				th := common.HexToHash(topic)
				scratch = appendLittTagKey(scratch[:0], blockNumber, tagKindTopic0+byte(p), th[:], txIndex, firstLogIndex, txHash) //nolint:gosec // p < maxIndexedTopics
				if err := batch.Set(scratch, nil); err != nil {
					return err
				}
			}
		}
		firstLogIndex += uint32(len(record.Receipt.Logs)) //nolint:gosec // log counts fit within uint32
	}
	return nil
}

// littTagRef is what a tag scan recovers per candidate transaction: the
// receipt's block-wide first log index and the tx hash to look it up in litt.
type littTagRef struct {
	firstLogIndex uint32
	txHash        common.Hash
}

// filterLogs answers the query from the tag index: per block, intersect the
// candidate transactions across criteria dimensions, then point-read only the
// surviving receipts from litt. A query with no criteria enumerates the
// block's log-bearing transactions via the address-tag keyspace.
func (idx littTagIndex) filterLogs(s *littReceiptStore, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	if latest := s.latestVersion.Load(); latest >= 0 && toBlock > uint64(latest) { //nolint:gosec // latest is non-negative here
		toBlock = uint64(latest) //nolint:gosec // latest is non-negative here
	}
	if earliest := s.earliestVersion.Load(); earliest > 0 && fromBlock < uint64(earliest) { //nolint:gosec // earliest is non-negative here
		fromBlock = uint64(earliest) //nolint:gosec // earliest is non-negative here
	}
	if fromBlock > toBlock {
		return nil, nil
	}

	groups := criteriaTagGroups(crit)
	var logs []*ethtypes.Log
	for block := fromBlock; block <= toBlock; block++ {
		candidates, err := s.blockTagCandidates(block, groups)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			continue
		}
		blockLogs, err := s.candidateBlockLogs(block, candidates, crit)
		if err != nil {
			return nil, err
		}
		logs = append(logs, blockLogs...)
	}
	return logs, nil
}

// blockTagCandidates returns the block's candidate transactions. With no
// criteria groups, it scans the address-tag keyspace (every log-bearing tx
// has an address tag). Otherwise it buckets one bounded scan of the block's
// tag keys by criteria group, then intersects: a tx survives only if some tag
// of every group named it (OR within a group, AND across groups — matchLog
// semantics).
func (s *littReceiptStore) blockTagCandidates(blockNumber uint64, groups [][][]byte) (map[uint32]littTagRef, error) {
	if len(groups) == 0 {
		// No criteria: every log-bearing tx has an address tag, so scanning
		// the block's whole address keyspace enumerates them.
		set := make(map[uint32]littTagRef)
		if err := s.scanTagRange(littTagKindKey(blockNumber, tagKindAddress), littTagKindKey(blockNumber, tagKindAddress+1), set); err != nil {
			return nil, err
		}
		return set, nil
	}

	// One tight bounded scan per (group, tag): the index wrapper has no
	// SeekGE, so instead of walking every tag key in the block we bound the
	// iterator to exactly one tag's keys — the seek-equivalent. A tx survives
	// only if it appears in every group (OR within a group, AND across).
	groupSets := make([]map[uint32]littTagRef, len(groups))
	for gi, group := range groups {
		set := make(map[uint32]littTagRef)
		for _, tag := range group {
			prefix := littTagTagKey(blockNumber, tag)
			if err := s.scanTagRange(prefix, prefixSuccessor(prefix), set); err != nil {
				return nil, err
			}
		}
		if len(set) == 0 {
			return nil, nil // this dimension matched nothing; intersection empty
		}
		groupSets[gi] = set
	}

	// Intersect: keep transactions present in every group, seeding from the
	// smallest set so the membership checks are minimal.
	result := groupSets[0]
	for _, gs := range groupSets[1:] {
		if len(gs) < len(result) {
			result, gs = gs, result
		}
		for txIndex := range result {
			if _, ok := gs[txIndex]; !ok {
				delete(result, txIndex)
			}
		}
		if len(result) == 0 {
			return nil, nil
		}
	}
	return result, nil
}

// scanTagRange walks [lower, upper) of the tag keyspace and adds every entry's
// txIndex -> ref into dst.
func (s *littReceiptStore) scanTagRange(lower, upper []byte, dst map[uint32]littTagRef) error {
	iter, err := s.index.NewIter(&dbtypes.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return err
	}
	defer func() { _ = iter.Close() }()

	for ; iter.Valid(); iter.Next() {
		txIndex, ref, err := parseLittTagKey(iter.Key())
		if err != nil {
			return err
		}
		dst[txIndex] = ref
	}
	return iter.Error()
}

// littTagTagKey is the scan prefix for one (block, kind+tag) — every key for
// that exact tag in that block shares it.
func littTagTagKey(blockNumber uint64, kindtag []byte) []byte {
	return append(littTagBlockKey(blockNumber), kindtag...)
}

// prefixSuccessor returns the smallest key strictly greater than every key
// beginning with prefix: increment the last byte that isn't 0xff and truncate.
// Tag prefixes always start with the 't' marker byte, so a non-0xff byte
// always exists and the nil (all-0xff) case can't arise here.
func prefixSuccessor(prefix []byte) []byte {
	out := make([]byte, len(prefix))
	copy(out, prefix)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] != 0xff {
			out[i]++
			return out[:i+1]
		}
	}
	return nil
}

// parseLittTagKey extracts the transaction index and the ref (first log index
// + tx hash) from a tag key's trailing suffix. The kind byte determines the
// tag width (address vs topic), which fixes where the suffix begins.
func parseLittTagKey(key []byte) (txIndex uint32, ref littTagRef, err error) {
	if len(key) < 1+blockNumLen+1 {
		return 0, littTagRef{}, fmt.Errorf("corrupt receipt tag key %x", key)
	}
	tagLen := littTopicTagLen
	if key[1+blockNumLen] == tagKindAddress {
		tagLen = littAddrTagLen
	}
	suffixStart := 1 + blockNumLen + 1 + tagLen
	if len(key) != suffixStart+littTagSuffixLen {
		return 0, littTagRef{}, fmt.Errorf("corrupt receipt tag key %x", key)
	}
	suffix := key[suffixStart:]
	ref.firstLogIndex = binary.BigEndian.Uint32(suffix[ledgerTxIndexLen:])
	copy(ref.txHash[:], suffix[ledgerTxIndexLen+4:])
	return binary.BigEndian.Uint32(suffix), ref, nil
}

// candidateBlockLogs point-reads the candidate receipts from litt by tx hash,
// in transaction-index order, and applies the exact matchLog predicate. A
// missing receipt is skipped, not an error: litt TTL GC can reclaim a body
// between the index scan and the read.
func (s *littReceiptStore) candidateBlockLogs(_ uint64, candidates map[uint32]littTagRef, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	txIndexes := make([]uint32, 0, len(candidates))
	for txIndex := range candidates {
		txIndexes = append(txIndexes, txIndex)
	}
	sort.Slice(txIndexes, func(i, j int) bool { return txIndexes[i] < txIndexes[j] })

	var logs []*ethtypes.Log
	for _, txIndex := range txIndexes {
		ref := candidates[txIndex]
		bz, exists, err := s.receipts.Get(ref.txHash[:])
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(bz); err != nil {
			return nil, err
		}
		for _, lg := range getLogsForTx(receipt, uint(ref.firstLogIndex)) {
			if matchLog(lg, crit) {
				logs = append(logs, lg)
			}
		}
	}
	return logs, nil
}

// pruneBlocks deletes the tag entries for blocks in [floor, cutoff).
func (littTagIndex) pruneBlocks(s *littReceiptStore, floor, cutoff uint64) error {
	return s.deleteIndexRange(littTagBlockKey(floor), littTagBlockKey(cutoff))
}
