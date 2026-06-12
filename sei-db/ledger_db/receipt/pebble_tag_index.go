package receipt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// tagBlockIndex is the "pebbleidx" log index: an exact lookup index instead
// of a per-block bloom. Every log writes one empty-valued key per tag — the
// emitting address and each topic position:
//
//	'T' + block (u64 BE) + kind (1B) + tag (20B addr / 32B topic)
//	    + txIndex (u32 BE) + firstLogIndex (u32 BE)  -> nil
//
// All information lives in the key; pebble's sorted layout groups a block's
// entries for one tag contiguously, so FilterLogs answers a query with one
// short sequential scan per (block, tag), an intersection across criteria
// dimensions, and a point read per matching receipt — it never decodes
// non-matching receipts, where the bloom design decodes every receipt of
// every candidate block. GC is one DeleteRange over the block range.
//
// The kind byte separates the address keyspace from each topic position
// (criteria are positional), keeping scans exact per dimension; matchLog
// still re-verifies, so correctness never depends on the index shape.
//
// firstLogIndex is the block-wide index of the receipt's first log, assigned
// at write time so reads can number logs without decoding the preceding
// receipts of the block. For a block written across several SetReceipts
// calls (legacy migration subsets) the numbering restarts per subset;
// matching and ordering remain exact.
type tagBlockIndex struct{}

const (
	ledgerTagKeyPrefix = 'T'

	tagKindAddress byte = 0x00
	tagKindTopic0  byte = 0x01 // +p for topic position p
	// maxIndexedTopics caps indexed topic positions at the EVM's LOG4 limit.
	maxIndexedTopics = 4

	tagKeySuffixLen = ledgerTxIndexLen + 4 // txIndex + firstLogIndex
)

// ledgerTagPrefix returns the scan prefix for one tag within one block.
func ledgerTagPrefix(blockNumber uint64, tag []byte) []byte {
	key := make([]byte, 0, 1+blockNumLen+len(tag)+tagKeySuffixLen)
	key = append(key, ledgerTagKeyPrefix)
	key = binary.BigEndian.AppendUint64(key, blockNumber)
	return append(key, tag...)
}

func ledgerTagKey(blockNumber uint64, tag []byte, txIndex, firstLogIndex uint32) []byte {
	key := ledgerTagPrefix(blockNumber, tag)
	key = binary.BigEndian.AppendUint32(key, txIndex)
	return binary.BigEndian.AppendUint32(key, firstLogIndex)
}

func addressTag(addressHex string) []byte {
	addr := common.HexToAddress(addressHex)
	return append([]byte{tagKindAddress}, addr[:]...)
}

func topicTag(position int, topic common.Hash) []byte {
	return append([]byte{tagKindTopic0 + byte(position)}, topic[:]...) //nolint:gosec // position < maxIndexedTopics
}

// stageBlock writes the tag keys for every log in the block's records
// (already sorted by transaction index). Values are nil: all information is
// in the key, and rewriting the same data (crash replay) is idempotent.
func (tagBlockIndex) stageBlock(_ *pebbleReceiptStore, batch *pebble.Batch, blockNumber uint64, records []ReceiptRecord) error {
	firstLogIndex := uint32(0)
	for _, record := range records {
		txIndex := record.Receipt.TransactionIndex
		for _, lg := range record.Receipt.Logs {
			if err := batch.Set(ledgerTagKey(blockNumber, addressTag(lg.Address), txIndex, firstLogIndex), nil, nil); err != nil {
				return err
			}
			for p, topic := range lg.Topics {
				if p >= maxIndexedTopics {
					break
				}
				if err := batch.Set(ledgerTagKey(blockNumber, topicTag(p, common.HexToHash(topic)), txIndex, firstLogIndex), nil, nil); err != nil {
					return err
				}
			}
		}
		firstLogIndex += uint32(len(record.Receipt.Logs)) //nolint:gosec // log counts fit within uint32
	}
	return nil
}

// criteriaTagGroups converts filter criteria into tag groups: one group per
// criteria dimension (the address list, each non-empty topic position). Tags
// within a group are OR'd, groups are AND'd — mirroring matchLog. Topic
// positions beyond the EVM limit are left to the exact predicate (no log can
// match them anyway).
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

// filterLogs answers the query from the tag index: per block, union the
// candidates of each tag within a group, intersect across groups, and point
// read only the surviving receipts. A query with no criteria has no tags to
// seek, so it falls back to scanning the blocks in the range (the bloom
// design degenerates identically: an empty query matches every bloom).
func (idx tagBlockIndex) filterLogs(s *pebbleReceiptStore, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	// Clamp to the data actually present; the loop below visits every block
	// number in the range, so unbounded requests must not walk past the tip.
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
	if len(groups) == 0 {
		var logs []*ethtypes.Log
		for block := fromBlock; block <= toBlock; block++ {
			blockLogs, err := s.filterBlockLogs(block, crit)
			if err != nil {
				return nil, err
			}
			logs = append(logs, blockLogs...)
		}
		return logs, nil
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: ledgerBlockKey(ledgerTagKeyPrefix, fromBlock),
		UpperBound: ledgerBlockUpperBound(ledgerTagKeyPrefix, toBlock),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var logs []*ethtypes.Log
	for block := fromBlock; block <= toBlock; block++ {
		candidates, err := blockTagCandidates(iter, block, groups)
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

// blockTagCandidates returns txIndex -> firstLogIndex for the block's
// receipts that satisfy every tag group: the first group seeds the set from
// its tags' scans, each later group keeps only the transactions it also
// contains. Returns early once the intersection is empty.
func blockTagCandidates(iter *pebble.Iterator, blockNumber uint64, groups [][][]byte) (map[uint32]uint32, error) {
	var result map[uint32]uint32
	for _, group := range groups {
		set := make(map[uint32]uint32)
		for _, tag := range group {
			prefix := ledgerTagPrefix(blockNumber, tag)
			for valid := iter.SeekGE(prefix); valid; valid = iter.Next() {
				key := iter.Key()
				if !bytes.HasPrefix(key, prefix) {
					break
				}
				suffix := key[len(prefix):]
				if len(suffix) != tagKeySuffixLen {
					return nil, fmt.Errorf("corrupt receipt tag key %x", key)
				}
				txIndex := binary.BigEndian.Uint32(suffix)
				firstLogIndex := binary.BigEndian.Uint32(suffix[ledgerTxIndexLen:])
				if result == nil {
					set[txIndex] = firstLogIndex
				} else if _, ok := result[txIndex]; ok {
					set[txIndex] = firstLogIndex
				}
			}
			if err := iter.Error(); err != nil {
				return nil, err
			}
		}
		result = set
		if len(result) == 0 {
			return nil, nil
		}
	}
	return result, nil
}

// candidateBlockLogs point reads the candidate receipts in transaction-index
// order and applies the exact matchLog predicate. A missing receipt is
// skipped, not an error: pruning can land between the index scan and the
// read.
func (s *pebbleReceiptStore) candidateBlockLogs(blockNumber uint64, candidates map[uint32]uint32, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	txIndexes := make([]uint32, 0, len(candidates))
	for txIndex := range candidates {
		txIndexes = append(txIndexes, txIndex)
	}
	sort.Slice(txIndexes, func(i, j int) bool { return txIndexes[i] < txIndexes[j] })

	var logs []*ethtypes.Log
	for _, txIndex := range txIndexes {
		bz, closer, err := s.db.Get(ledgerReceiptKey(blockNumber, txIndex))
		if err != nil {
			if errors.Is(err, pebble.ErrNotFound) {
				continue
			}
			return nil, err
		}
		receipt := &types.Receipt{}
		unmarshalErr := receipt.Unmarshal(bz)
		_ = closer.Close()
		if unmarshalErr != nil {
			return nil, unmarshalErr
		}
		for _, lg := range getLogsForTx(receipt, uint(candidates[txIndex])) {
			if matchLog(lg, crit) {
				logs = append(logs, lg)
			}
		}
	}
	return logs, nil
}

// pruneBlocks removes the tag entries for blocks in [floor, cutoff).
func (tagBlockIndex) pruneBlocks(batch *pebble.Batch, floor, cutoff uint64) error {
	return batch.DeleteRange(ledgerBlockKey(ledgerTagKeyPrefix, floor), ledgerBlockKey(ledgerTagKeyPrefix, cutoff), nil)
}
