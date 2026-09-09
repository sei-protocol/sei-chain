package ktype

import "bytes"

// MetaKeyPrefix is the key namespace a database reserves for its own metadata, which hashing must
// exclude: the metadata records the hash, so combining it in would make the hash depend on itself.
const MetaKeyPrefix = "_meta/"

const (
	metaVersion = MetaKeyPrefix + "version"
	metaLtHash  = MetaKeyPrefix + "hash"

	// moduleLtHashPrefix brackets the per-module metadata keys stored in each
	// data DB, e.g. "_meta/x:evm/hash", "_meta/x:gov/stats". The "x:" segment
	// namespaces module names so they never collide with the fixed per-DB keys
	// (version / hash). Each module has a "/hash" key (its per-module
	// LtHash) and a "/stats" key (its per-module key-count / byte totals).
	moduleLtHashPrefix = MetaKeyPrefix + "x:"
	moduleLtHashSuffix = "/hash"
	moduleStatsSuffix  = "/stats"
)

var (
	MetaKeyPrefixBytes = []byte(MetaKeyPrefix)
	MetaVersionKey     = []byte(metaVersion)
	MetaLtHashKey      = []byte(metaLtHash)
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
//
// The meta namespace is a cache. Every record in it is derived — the per-DB root and
// the per-module hashes and stats from a full scan of the DB's keys, the version
// from whatever wrote them — and it is kept on disk only because that scan is
// expensive. Consistently with being a cache it is neither hashed nor exported:
// the LtHash scan and RawGlobalIterator both skip it, so it is outside consensus
// and a state-synced node rebuilds all of it from the data it imported.
//
// A key that would be a source of truth rather than a cached derivation belongs
// in a module keyspace, the way migration progress does.
func IsMetaKey(key []byte) bool {
	return bytes.HasPrefix(key, MetaKeyPrefixBytes)
}
