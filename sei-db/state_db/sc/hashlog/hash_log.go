package hashlog

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// The CSV column that always precedes the per-type hash columns.
const blockNumberHeader = "block_number"

// A record for a single block, recording information about the block's hash.
//
// The set of hashes a record carries is data-driven: each configured hash type (e.g. "changeset", "flatKV",
// "memIAVL", "root") maps to a hash value. A nil value means no hash was recorded for that type (the
// subsystem was disabled, or the caller opted out of changeset hashing for the block by passing a nil change set).
type HashLog struct {
	// The software version currently running on this node. Populated from the file name when read back;
	// it is not stored on individual CSV rows.
	Version string

	// The block number for this log entry.
	BlockNumber uint64

	// The recorded hashes, keyed by hash type. A nil value means the hash was not recorded for that type
	// (the subsystem was disabled, or changeset hashing was opted out for this block).
	Hashes map[string][]byte
}

// Get the CSV header for a HashLog given the ordered set of hash types. The first column is always the block
// number, followed by one column per hash type in order.
func hashLogHeaders(hashTypes []string) string {
	fields := make([]string, 0, len(hashTypes)+1)
	fields = append(fields, blockNumberHeader)
	fields = append(fields, hashTypes...)
	return strings.Join(fields, ",")
}

// Convert a HashLog to a CSV string using "," as the separator. Hashes are emitted in hashTypes order and
// hex-encoded; a missing or nil hash becomes an empty field. The version is not emitted (it lives in the file
// name). No escaping is needed: block numbers are numeric and hashes are hex, so no field can contain a ",".
func (h *HashLog) toCSV(hashTypes []string) string {
	fields := make([]string, 0, len(hashTypes)+1)
	fields = append(fields, strconv.FormatUint(h.BlockNumber, 10))
	for _, hashType := range hashTypes {
		fields = append(fields, hex.EncodeToString(h.Hashes[hashType]))
	}
	return strings.Join(fields, ",")
}

// Parse a HashLog from a CSV string. The hashTypes give the column order, recovered from the file's header.
// The returned HashLog has an empty Version; callers set it from the file name.
func hashLogFromCSV(hashTypes []string, csv string) (*HashLog, error) {
	fields := strings.Split(csv, ",")
	if len(fields) != len(hashTypes)+1 {
		return nil, fmt.Errorf("expected %d fields, got %d", len(hashTypes)+1, len(fields))
	}

	blockNumber, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse block number %q: %w", fields[0], err)
	}

	hashes := make(map[string][]byte, len(hashTypes))
	for i, hashType := range hashTypes {
		field := fields[i+1]
		if field == "" {
			hashes[hashType] = nil
			continue
		}
		hash, err := hex.DecodeString(field)
		if err != nil {
			return nil, fmt.Errorf("failed to decode hash for type %q: %w", hashType, err)
		}
		hashes[hashType] = hash
	}

	return &HashLog{
		BlockNumber: blockNumber,
		Hashes:      hashes,
	}, nil
}
