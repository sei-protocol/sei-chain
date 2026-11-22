package mvcc

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
)

// ScanOptions configures Scan.
type ScanOptions struct {
	// StoreKey is the store whose entries Scan reads.
	StoreKey string
	// Visit, when set, is called with the store-relative key, version and value
	// of every live entry, in key order. An error it returns stops the scan.
	Visit func(key []byte, version int64, value []byte) error
	// Progress, when set, is called with the running report as entries are read.
	Progress func(ScanReport)
}

// ScanReport is what Scan found in a DB.
type ScanReport struct {
	DBInfo
	// ChangelogAhead is the version of a changelog entry above Latest, which
	// only opening the DB would apply, and 0 when there is none.
	ChangelogAhead int64
	// Entries is the number of live entries read, and Deletions the number of
	// entries recording that their key was deleted.
	Entries   int64
	Deletions int64
}

// Scan opens the DB at dir read-only and reads every entry under one store.
func Scan(dir string, opts ScanOptions) (ScanReport, error) {
	m, err := openOfflineDB(dir, true)
	if err != nil {
		return ScanReport{}, err
	}
	defer func() { _ = m.close() }()

	report := ScanReport{DBInfo: m.info}
	if report.ChangelogAhead, err = changelogAhead(dir, m.info.Latest); err != nil {
		return report, err
	}
	return report, m.scanStore(&report, opts)
}

// scanStore reads every entry under opts.StoreKey, counting into report and
// handing each live one to opts.Visit.
func (m *offlineDB) scanStore(report *ScanReport, opts ScanOptions) error {
	prefix := storePrefix(opts.StoreKey)
	itr, err := m.db.NewIter(&pebble.IterOptions{
		LowerBound: MVCCEncode(prefix, 0, m.info.Descending),
		UpperBound: MVCCEncode(prefixEnd(prefix), 0, m.info.Descending),
	})
	if err != nil {
		return fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() { _ = itr.Close() }()

	for valid := itr.First(); valid; valid = itr.Next() {
		userKey, version, value, live, err := m.decodeEntry(itr)
		if err != nil {
			return err
		}
		if !live {
			report.Deletions++
			continue
		}
		report.Entries++
		if opts.Progress != nil && report.Entries%progressEvery == 0 {
			opts.Progress(*report)
		}
		if opts.Visit == nil {
			continue
		}
		if err := opts.Visit(bytes.TrimPrefix(userKey, prefix), version, value); err != nil {
			return err
		}
	}
	return itr.Error()
}

// decodeEntry splits the entry under itr into its user key, version and value.
// live is false for an entry recording that its key was deleted at its version.
func (m *offlineDB) decodeEntry(itr *pebble.Iterator) (userKey []byte, version int64, value []byte, live bool, err error) {
	userKey, vBz, ok := SplitMVCCKey(itr.Key())
	if !ok {
		return nil, 0, nil, false, fmt.Errorf("invalid MVCC key %x", itr.Key())
	}
	if len(vBz) > 0 {
		if version, err = decodeMVCCVersion(vBz, m.info.Descending); err != nil {
			return nil, 0, nil, false, fmt.Errorf("decode version of %x: %w", userKey, err)
		}
	}
	raw, err := itr.ValueAndErr()
	if err != nil {
		return nil, 0, nil, false, fmt.Errorf("read value of %x: %w", userKey, err)
	}
	value, tombstoneBz, ok := SplitMVCCKey(raw)
	if !ok {
		return nil, 0, nil, false, fmt.Errorf("invalid MVCC value of %x", userKey)
	}
	if len(tombstoneBz) == 0 {
		return userKey, version, value, true, nil
	}
	tombstone, err := decodeMVCCVersion(tombstoneBz, m.info.Descending)
	if err != nil {
		return nil, 0, nil, false, fmt.Errorf("decode tombstone of %x: %w", userKey, err)
	}
	return userKey, version, value, tombstone > version, nil
}
