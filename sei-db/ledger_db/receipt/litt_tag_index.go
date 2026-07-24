package receipt

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"golang.org/x/sync/errgroup"
)

// The tag index is the "littidx" filtering layer. litt holds the receipt
// bodies (point lookup by tx hash); this pebble index holds one empty-valued
// key per log tag so FilterLogs can locate matching receipts without scanning
// whole blocks:
//
//	't' + block (u64 BE) + kind (1B) + tag (20B addr / 32B topic)
//	    + txIndex (u32 BE) + firstLogIndex (u32 BE) + txHash (32B)  -> nil
//
// A query seeks the (block, tag) entries, intersects candidates across the
// criteria dimensions, then point-reads only the matching receipts from litt
// by the tx hash carried in the key. The kind byte keeps the address and each
// topic position in disjoint keyspaces (criteria are positional).
// firstLogIndex is the receipt's block-wide first log index, stored so reads
// can number logs without decoding the receipts before it. Pruning a block
// range is a single range tombstone (see deleteIndexRange).
const (
	littTagKeyPrefix = 't'

	tagKindAddress   byte = 0x00
	tagKindTopic0    byte = 0x01 // +p for topic position p
	maxIndexedTopics      = 4    // EVM LOG4 limit

	littTxIndexLen   = 4
	littLogIndexLen  = 4
	littAddrTagLen   = common.AddressLength // 20
	littTopicTagLen  = common.HashLength    // 32
	littTagSuffixLen = littTxIndexLen + littLogIndexLen + common.HashLength
	littTagKeyMaxLen = 1 + blockNumLen + 1 + littTopicTagLen + littTagSuffixLen
)

// littTagRef is what a tag scan recovers per candidate transaction: the
// receipt's block-wide first log index and the tx hash to look it up in litt.
type littTagRef struct {
	firstLogIndex uint32
	txHash        common.Hash
}

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

// littTagTagKey is the scan prefix for one (block, kind+tag).
func littTagTagKey(blockNumber uint64, kindTag []byte) []byte {
	return append(littTagBlockKey(blockNumber), kindTag...)
}

// appendLittTagKey writes a full tag key into dst[:0], reusing its capacity so
// the write path allocates no per-key slice (pebble's batch.Set copies the key,
// so the buffer is free to reuse immediately after).
func appendLittTagKey(dst []byte, blockNumber uint64, kind byte, tag []byte, txIndex, firstLogIndex uint32, txHash common.Hash) []byte {
	dst = append(dst, littTagKeyPrefix)
	dst = binary.BigEndian.AppendUint64(dst, blockNumber)
	dst = append(dst, kind)
	dst = append(dst, tag...)
	dst = binary.BigEndian.AppendUint32(dst, txIndex)
	dst = binary.BigEndian.AppendUint32(dst, firstLogIndex)
	return append(dst, txHash[:]...)
}

// addressTag and topicTag build the kind+tag bytes that key membership is
// matched on; criteriaTagGroups turns filter criteria into tag groups (OR
// within a group, AND across groups — matchLog semantics), one group per
// constrained dimension (the address list, each non-empty topic position).
func addressTag(addressHex string) []byte {
	addr := common.HexToAddress(addressHex)
	return append([]byte{tagKindAddress}, addr[:]...)
}

func topicTag(position int, topic common.Hash) []byte {
	return append([]byte{tagKindTopic0 + byte(position)}, topic[:]...) //nolint:gosec // position < maxIndexedTopics
}

func criteriaTagGroups(crit filters.FilterCriteria) [][][]byte {
	var groups [][][]byte
	if len(crit.Addresses) > 0 {
		group := make([][]byte, 0, len(crit.Addresses))
		for _, addr := range crit.Addresses {
			group = append(group, append([]byte{tagKindAddress}, addr[:]...))
		}
		groups = append(groups, group)
	}
	for p, topicList := range crit.Topics {
		if p >= maxIndexedTopics || len(topicList) == 0 {
			continue
		}
		group := make([][]byte, 0, len(topicList))
		for _, topic := range topicList {
			group = append(group, topicTag(p, topic))
		}
		groups = append(groups, group)
	}
	return groups
}

// parseLittTagKey extracts the transaction index and ref (first log index + tx
// hash) from a tag key's trailing suffix. The kind byte fixes the tag width.
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
	ref.firstLogIndex = binary.BigEndian.Uint32(suffix[littTxIndexLen:])
	copy(ref.txHash[:], suffix[littTxIndexLen+littLogIndexLen:])
	return binary.BigEndian.Uint32(suffix), ref, nil
}

// prefixSuccessor returns the smallest key strictly greater than every key
// beginning with prefix. Tag prefixes start with the 't' marker, so a non-0xff
// byte always exists and the nil (all-0xff) case can't arise here.
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

// stageTagKeys writes the tag keys for every log in the block (records already
// sorted by transaction index) onto the index batch. Values are nil — all
// information is in the key — so re-staging the same data (crash replay) is
// idempotent. firstLogIndex is block-wide within a single call; a block split
// across calls (legacy migration) restarts it per part — harmless, as the RPC
// layer recomputes log indices from canonical block data.
func (s *littReceiptStore) stageTagKeys(batch dbtypes.Batch, blockNumber uint64, records []ReceiptRecord) error {
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

// filterLogsByTags answers a getLogs query from the tag index. Each block is
// independent — its candidate intersection and the point-reads of the surviving
// receipts touch only that block's keys — so a range is fanned across a bounded
// worker pool (logFilterParallelism) instead of walked one block at a time.
// This parallelizes both the per-block index scans and the litt body reads,
// which dominate wide-range latency. Results are exact (matchLog re-verifies
// after decode) and stay in (block, txIndex) order via the indexed buffer.
func (s *littReceiptStore) filterLogsByTags(ctx context.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria, budget *LogBudget) ([]*ethtypes.Log, error) {
	if latest := s.latestVersion.Load(); latest >= 0 && toBlock > uint64(latest) { //nolint:gosec // latest is non-negative
		toBlock = uint64(latest) //nolint:gosec // latest is non-negative
	}
	if earliest := s.earliestVersion.Load(); earliest > 0 && fromBlock < uint64(earliest) { //nolint:gosec // earliest is non-negative
		fromBlock = uint64(earliest) //nolint:gosec // earliest is non-negative
	}
	if fromBlock > toBlock {
		return nil, nil
	}

	groups := criteriaTagGroups(crit)
	nBlocks := int(toBlock - fromBlock + 1) //nolint:gosec // bounded by the caller's range cap

	// One result slot per block, written by exactly one worker, so the merged
	// output keeps block order regardless of completion order. errgroup caps
	// concurrency at the configured limit and hands the next block to whichever
	// worker frees up, load-balancing across the skewed per-block cost.
	//
	// budget.Reserve aborts the fan-out once matched logs exceed its cap: the
	// tripping worker returns the budget's error, which cancels the group so
	// already-scheduled blocks bail before reading, and the loop stops queueing
	// new blocks.
	// egCtx derives from the caller's request context, so a client disconnect
	// cancels the group the same way a budget trip does.
	results := make([][]*ethtypes.Log, nBlocks)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(s.logFilterParallelism)
	for i := 0; i < nBlocks; i++ {
		if egCtx.Err() != nil {
			break
		}
		eg.Go(func() error {
			if egCtx.Err() != nil {
				return nil // group already cancelled; skip without overwriting the cause
			}
			blockLogs, err := s.blockLogs(egCtx, fromBlock+uint64(i), groups, crit, budget) //nolint:gosec // i < nBlocks
			if err != nil {
				return err
			}
			results[i] = blockLogs
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	logs := make([]*ethtypes.Log, 0, budget.UsedCount())
	for _, blockLogs := range results {
		logs = append(logs, blockLogs...)
	}
	return logs, nil
}

// blockLogs answers the query for one block: intersect the tag candidates, then
// point-read and match the surviving receipts. Returns nil when nothing matches.
func (s *littReceiptStore) blockLogs(ctx context.Context, block uint64, groups [][][]byte, crit filters.FilterCriteria, budget *LogBudget) ([]*ethtypes.Log, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates, err := s.blockTagCandidates(block, groups)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return s.candidateBlockLogs(ctx, candidates, crit, budget)
}

// blockTagCandidates returns the block's candidate transactions. With no
// criteria groups it scans the address keyspace (every log-bearing tx has an
// address tag). Otherwise it scans exactly one tag's keys per (group, tag) —
// the index iterator has no SeekGE, so a tight bounded scan is the
// seek-equivalent — and intersects: a tx survives only if some tag of every
// group named it.
func (s *littReceiptStore) blockTagCandidates(blockNumber uint64, groups [][][]byte) (map[uint32]littTagRef, error) {
	if len(groups) == 0 {
		set := make(map[uint32]littTagRef)
		err := s.scanTagRange(littTagKindKey(blockNumber, tagKindAddress), littTagKindKey(blockNumber, tagKindAddress+1), set)
		return set, err
	}

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

	// Intersect, seeding from the smallest set to minimize membership checks.
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

// scanTagRange walks [lower, upper) of the tag keyspace, adding every entry's
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

// candidateBlockLogs point-reads the candidate receipts from litt by tx hash,
// in transaction-index order, and applies the exact matchLog predicate. A
// missing receipt is skipped, not an error: litt TTL GC can reclaim a body
// between the index scan and the read.
func (s *littReceiptStore) candidateBlockLogs(ctx context.Context, candidates map[uint32]littTagRef, crit filters.FilterCriteria, budget *LogBudget) ([]*ethtypes.Log, error) {
	txIndexes := make([]uint32, 0, len(candidates))
	for txIndex := range candidates {
		txIndexes = append(txIndexes, txIndex)
	}
	sort.Slice(txIndexes, func(i, j int) bool { return txIndexes[i] < txIndexes[j] })

	var logs []*ethtypes.Log
	for _, txIndex := range txIndexes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if budget.Tripped() {
			return nil, budget.Err()
		}

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
		for _, rawLog := range receipt.Logs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if budget.Tripped() {
				return nil, budget.Err()
			}
			lg := convertLog(rawLog, receipt, uint(ref.firstLogIndex))
			if !matchLog(lg, crit) {
				continue
			}
			if err := budget.Reserve(lg); err != nil {
				return nil, err
			}
			logs = append(logs, lg)
		}
	}
	return logs, nil
}
