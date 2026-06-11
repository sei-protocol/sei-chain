package receipt

import (
	"hash/fnv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
)

// Per-block bloom filter used by the block-ledger backends to skip blocks
// during FilterLogs. The bloom is built from every log address and topic in
// the block — the same fields eth_getLogs filters on — so a non-matching
// bloom proves the block holds no matching logs (no false negatives). False
// positives are caught by the exact matchLog predicate after decode.
//
// The protocol's 2048-bit logsBloom saturates at Giga log volume (all bits
// set => skips nothing), so we use a much larger filter. 16KB per block is
// negligible storage (~40MB per 100k-block retention window) and keeps the
// false-positive rate around 1% at ~10k distinct elements per block.
const (
	blockBloomSizeBytes = 16 * 1024
	blockBloomBits      = blockBloomSizeBytes * 8
	blockBloomHashes    = 4
)

// bloomPositions derives the k bit positions for data using double hashing
// (Kirsch-Mitzenmacher) over a single 64-bit FNV-1a hash. Topics can carry
// low-entropy raw values (indexed uints pad to mostly-zero words), so we hash
// rather than slicing bytes out of the element directly.
func bloomPositions(data []byte) [blockBloomHashes]uint32 {
	h := fnv.New64a()
	_, _ = h.Write(data)
	sum := h.Sum64()
	h1 := uint32(sum)
	h2 := uint32(sum>>32) | 1 // odd so positions cycle through the table
	var positions [blockBloomHashes]uint32
	for i := range positions {
		positions[i] = (h1 + uint32(i)*h2) % blockBloomBits
	}
	return positions
}

func bloomAdd(bloom []byte, data []byte) {
	for _, pos := range bloomPositions(data) {
		bloom[pos/8] |= 1 << (pos % 8)
	}
}

func bloomMayContain(bloom []byte, data []byte) bool {
	for _, pos := range bloomPositions(data) {
		if bloom[pos/8]&(1<<(pos%8)) == 0 {
			return false
		}
	}
	return true
}

// bloomOr merges src's bits into dst. Used when a block is written across
// multiple SetReceipts calls (legacy receipt migration flushes historical
// blocks in tx-hash-ordered subsets): the stored bloom must cover the union
// of every subset or FilterLogs would produce false negatives.
func bloomOr(dst, src []byte) {
	for i := range dst {
		dst[i] |= src[i]
	}
}

// buildBlockBloom returns a block bloom covering every log address and topic
// in the records.
func buildBlockBloom(records []ReceiptRecord) []byte {
	bloom := make([]byte, blockBloomSizeBytes)
	for _, record := range records {
		for _, lg := range record.Receipt.Logs {
			addr := common.HexToAddress(lg.Address)
			bloomAdd(bloom, addr[:])
			for _, topic := range lg.Topics {
				topicHash := common.HexToHash(topic)
				bloomAdd(bloom, topicHash[:])
			}
		}
	}
	return bloom
}

// bloomMatchesCriteria reports whether a block whose logs produced this bloom
// could contain a log matching the filter criteria. Mirrors the matchLog
// predicate semantics: OR within an address list / topic position, AND across
// topic positions, empty list = wildcard. Positions are not encoded in the
// bloom (same as Ethereum's logsBloom); the exact predicate verifies them.
func bloomMatchesCriteria(bloom []byte, crit filters.FilterCriteria) bool {
	if len(crit.Addresses) > 0 {
		found := false
		for _, addr := range crit.Addresses {
			if bloomMayContain(bloom, addr[:]) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, topicList := range crit.Topics {
		if len(topicList) == 0 {
			continue
		}
		found := false
		for _, topic := range topicList {
			if bloomMayContain(bloom, topic[:]) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
