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

	// moduleLtHashPrefix brackets the per-module metadata keys stored in each
	// data DB, e.g. "_meta/x:evm/hash", "_meta/x:gov/stats". The "x:" segment
	// namespaces module names so they never collide with the fixed per-DB keys
	// (version / hash / earliest). Each module has a "/hash" key (its per-module
	// LtHash) and a "/stats" key (its per-module key-count / byte totals).
	moduleLtHashPrefix = metaKeyPrefix + "x:"
	moduleLtHashSuffix = "/hash"
	moduleStatsSuffix  = "/stats"
)

var (
	MetaKeyPrefixBytes = []byte(metaKeyPrefix)
	MetaVersionKey     = []byte(metaVersion)
	MetaLtHashKey      = []byte(metaLtHash)
	// MetaEarliestVersionKey records the version a seeded store's history
	// begins at (written once by SetInitialVersion, global metadata DB
	// only). Absent on genesis stores and stores predating the record.
	MetaEarliestVersionKey = []byte(metaEarliest)

	// ModuleLtHashPrefixBytes is the inclusive lower bound for iterating the
	// per-module LtHash keys ("_meta/x:") within a data DB.
	ModuleLtHashPrefixBytes = []byte(moduleLtHashPrefix)
)

// ModuleLtHashKey returns the per-DB metadata key that stores the LtHash for a
// single module within a data DB, e.g. ModuleLtHashKey("evm") ==
// "_meta/x:evm/hash". account/code/storage DBs only ever hold the "evm"
// module; miscDB may hold several (evm plus cosmos modules).
func ModuleLtHashKey(module string) []byte {
	return []byte(moduleLtHashPrefix + module + moduleLtHashSuffix)
}

// ParseModuleLtHashKey extracts the module name from a per-module LtHash meta
// key. Returns ("", false) if key is not of the form "_meta/x:<module>/hash".
// Module names never contain '/', so trimming the fixed prefix/suffix is
// unambiguous. Per-module stats keys ("_meta/x:<module>/stats") share the
// prefix but not the suffix, so they are correctly rejected here.
func ParseModuleLtHashKey(key []byte) (string, bool) {
	return parseModuleKey(key, moduleLtHashSuffix)
}

// ModuleStatsKey returns the per-DB metadata key that stores the auxiliary
// stats (key count / byte totals) for a single module within a data DB, e.g.
// ModuleStatsKey("evm") == "_meta/x:evm/stats".
func ModuleStatsKey(module string) []byte {
	return []byte(moduleLtHashPrefix + module + moduleStatsSuffix)
}

// ParseModuleStatsKey extracts the module name from a per-module stats meta
// key. Returns ("", false) if key is not of the form "_meta/x:<module>/stats".
func ParseModuleStatsKey(key []byte) (string, bool) {
	return parseModuleKey(key, moduleStatsSuffix)
}

// parseModuleKey trims the shared "_meta/x:" prefix and the given suffix from a
// per-module meta key, returning the module name in between. Module names never
// contain '/', so the trim is unambiguous.
func parseModuleKey(key []byte, suffix string) (string, bool) {
	if !bytes.HasPrefix(key, ModuleLtHashPrefixBytes) {
		return "", false
	}
	rest := key[len(ModuleLtHashPrefixBytes):]
	if !bytes.HasSuffix(rest, []byte(suffix)) {
		return "", false
	}
	module := rest[:len(rest)-len(suffix)]
	if len(module) == 0 {
		return "", false
	}
	return string(module), true
}

// IsMetaKey reports whether key is a per-DB internal metadata key (not user data).
//
// Safety: _meta/ keys are 10–13 bytes; the shortest user key is 20 bytes
// (an EVM address). Prefix collision would require an address starting with
// 0x5F6D657461 ("_meta") — probability ~2^-48 for random addresses and
// negligible even under CREATE2 brute-force. Misc DB keys must not use
// the _meta/ prefix.
func IsMetaKey(key []byte) bool {
	return bytes.HasPrefix(key, MetaKeyPrefixBytes)
}

// LocalMeta stores per-DB version tracking metadata.
// Version is stored at _meta/version, the per-DB root LtHash at _meta/hash,
// and per-module LtHashes at _meta/x:<module>/hash.
type LocalMeta struct {
	CommittedVersion int64          // Current committed version in this DB
	LtHash           *lthash.LtHash // per-DB root; nil for old format (version-only)

	// ModuleLtHashes holds the LtHash of each module's keys within this DB,
	// keyed by module name (e.g. "evm", "gov"). The per-DB root (LtHash)
	// equals the homomorphic sum of these module hashes. nil/empty when the
	// DB has never been written (fresh store).
	ModuleLtHashes map[string]*lthash.LtHash

	// ModuleStats holds the auxiliary key-count / byte totals of each module's
	// keys within this DB, keyed by module name and mirroring ModuleLtHashes.
	// Consensus-irrelevant; per-DB / global totals are derived on demand.
	// nil/empty when the DB has never been written (fresh store).
	ModuleStats map[string]lthash.ModuleStats
}
