package flatkv

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// LocalMetaDecoder assembles one data database's LocalMeta from the rows of its _meta/ namespace. The zero value
// is ready to use.
type LocalMetaDecoder struct {
	// firstKey is the first metadata key fed to the decoder, nil until one is.
	firstKey []byte

	// hasVersion is whether the version record has been fed.
	hasVersion bool

	// version is the raw version record.
	version []byte

	// hasLtHash is whether the per-DB root record has been fed.
	hasLtHash bool

	// ltHash is the raw per-DB root record.
	ltHash []byte

	// moduleHashes holds the per-module hash records, in the order they were fed.
	moduleHashes []moduleMetaRecord

	// moduleStats holds the per-module stats records, in the order they were fed.
	moduleStats []moduleMetaRecord
}

// moduleMetaRecord is one per-module metadata record, not yet decoded.
type moduleMetaRecord struct {
	// module is the module the record belongs to.
	module string

	// value is the record's raw value.
	value []byte
}

// Add feeds one row of the _meta/ namespace. Rows must arrive in ascending key order. Both slices are
// copied. A key outside the records LocalMeta holds is ignored.
func (d *LocalMetaDecoder) Add(
	// The row's key, which must lie in the _meta/ namespace.
	key []byte,
	// The row's value.
	value []byte,
) {
	if d.firstKey == nil {
		d.firstKey = bytes.Clone(key)
	}
	switch {
	case bytes.Equal(key, ktype.MetaVersionKey):
		d.hasVersion = true
		d.version = bytes.Clone(value)
	case bytes.Equal(key, ktype.MetaLtHashKey):
		d.hasLtHash = true
		d.ltHash = bytes.Clone(value)
	default:
		if module, ok := ktype.ParseModuleLtHashKey(key); ok {
			record := moduleMetaRecord{module: module, value: bytes.Clone(value)}
			d.moduleHashes = append(d.moduleHashes, record)
		} else if module, ok := ktype.ParseModuleStatsKey(key); ok {
			record := moduleMetaRecord{module: module, value: bytes.Clone(value)}
			d.moduleStats = append(d.moduleStats, record)
		}
	}
}

// Finish returns the LocalMeta the fed rows describe. A database with no version record is reported as one
// that has never been written, and it is an error for such a database to hold any other metadata.
func (d *LocalMetaDecoder) Finish() (*LocalMeta, error) {
	if !d.hasVersion {
		// Metadata is written as a set, so a DB missing its version must hold no metadata at
		// all. Reporting one that holds some as never-written would hide a lost root behind a
		// replay that cannot restore it: the hashes are maintained by unmixing the old value
		// and mixing the new, so re-applying a block whose rows are already on disk cancels
		// out and leaves those rows uncounted, silently changing the store root.
		//
		// Rebuilding the root here instead would mean a full scan of the DB, and unlike an
		// import or an empty DB this path is not already making one.
		if d.firstKey != nil {
			return nil, fmt.Errorf("flatkv: %s is absent but %s is present; "+
				"this DB lost its version record, and no replay can rebuild the root it should have",
				ktype.MetaVersionKey, d.firstKey)
		}
		return &LocalMeta{CommittedVersion: 0}, nil
	}

	meta := &LocalMeta{}
	var err error
	meta.CommittedVersion, err = decodeVersion(ktype.MetaVersionKey, d.version)
	if err != nil {
		return nil, err
	}

	if d.hasLtHash {
		meta.LtHash, err = lthash.Unmarshal(d.ltHash)
		if err != nil {
			return nil, fmt.Errorf("unmarshal meta hash: %w", err)
		}
	}

	meta.ModuleLtHashes = make(map[string]*lthash.LtHash, len(d.moduleHashes))
	for _, record := range d.moduleHashes {
		h, err := lthash.Unmarshal(record.value)
		if err != nil {
			return nil, fmt.Errorf("unmarshal module %q meta hash: %w", record.module, err)
		}
		meta.ModuleLtHashes[record.module] = h
	}

	meta.ModuleStats = make(map[string]lthash.ModuleStats, len(d.moduleStats))
	for _, record := range d.moduleStats {
		st, err := lthash.UnmarshalModuleStats(record.value)
		if err != nil {
			return nil, fmt.Errorf("unmarshal module %q stats: %w", record.module, err)
		}
		meta.ModuleStats[record.module] = st
	}

	return meta, nil
}
