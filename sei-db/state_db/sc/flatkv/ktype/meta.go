package ktype

import (
	"bytes"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

const metaKeyPrefix = "_meta/"

const (
	metaVersion  = metaKeyPrefix + "version"
	metaLtHash   = metaKeyPrefix + "hash"
	metaEarliest = metaKeyPrefix + "earliest"
)

var (
	MetaKeyPrefixBytes = []byte(metaKeyPrefix)
	MetaVersionKey     = []byte(metaVersion)
	MetaLtHashKey      = []byte(metaLtHash)
	// MetaEarliestVersionKey records the version a seeded store's history
	// begins at (written once by SetInitialVersion, global metadata DB
	// only). Absent on genesis stores and stores predating the record.
	MetaEarliestVersionKey = []byte(metaEarliest)
)

// IsMetaKey reports whether key is a per-DB internal metadata key (not user data).
//
// Safety: _meta/ keys are 10–13 bytes; the shortest user key is 20 bytes
// (an EVM address). Prefix collision would require an address starting with
// 0x5F6D657461 ("_meta") — probability ~2^-48 for random addresses and
// negligible even under CREATE2 brute-force. Legacy DB keys must not use
// the _meta/ prefix.
func IsMetaKey(key []byte) bool {
	return bytes.HasPrefix(key, MetaKeyPrefixBytes)
}

// LocalMeta stores per-DB version tracking metadata.
// Version is stored at _meta/version, LtHash at _meta/hash.
type LocalMeta struct {
	CommittedVersion int64          // Current committed version in this DB
	LtHash           *lthash.LtHash // nil for old format (version-only)
}
