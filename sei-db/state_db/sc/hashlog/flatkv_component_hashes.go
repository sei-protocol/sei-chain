package hashlog

import "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"

// FlatKVComponentHashes returns the checksum of each of a block's flatKV data databases and modules, keyed by hash
// log column name. The store-wide root is not included.
func FlatKVComponentHashes(hash *lthash.BlockHash) map[string][]byte {
	hashes := make(map[string][]byte, len(hash.PerDB))
	for dataDB, dbHash := range hash.PerDB {
		checksum := dbHash.Checksum()
		hashes[FlatKVDBHashPrefix+dataDB] = checksum[:]
	}
	for dataDB, modules := range hash.PerModule {
		for module, moduleHash := range modules {
			checksum := moduleHash.Checksum()
			hashes[FlatKVModuleHashPrefix+dataDB+"/"+module] = checksum[:]
		}
	}
	return hashes
}
