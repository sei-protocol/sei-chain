package operations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/kvrepair"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

const testEVMKeyOffset = len(keys.EVMStoreKey) + 1

type inspectRow struct {
	physKey []byte
	logical []byte
}

func listReport(backend string, version int64, bucket string, rows ...inspectRow) evmInspectJSON {
	r := evmInspectJSON{
		Backend:       backend,
		Version:       version,
		InspectBucket: bucket,
		KeyOffset:     testEVMKeyOffset,
		List:          true,
	}
	for _, row := range rows {
		r.Entries = append(r.Entries, evmInspectEntryJSON{
			Key:     fmt.Sprintf("%X", row.physKey),
			Logical: fmt.Sprintf("%X", row.logical),
		})
	}
	r.Listed = len(rows)
	r.Matched = uint64(len(rows))
	return r
}

func storageRow(addr ktype.Address, slot ktype.Slot, value []byte) inspectRow {
	return inspectRow{physKey: ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), logical: value}
}

func accountRow(addr ktype.Address, balance []byte, nonce uint64, codeHash []byte) inspectRow {
	logical := append(append(append([]byte{}, balance...), nonceBytes(nonce)...), codeHash...)
	return inspectRow{physKey: ktype.EVMPhysicalKey(keys.EVMKeyNonce, addr[:]), logical: logical}
}

func exportForTest(t *testing.T, reserve, prod evmInspectJSON) []kvrepair.Entry {
	t.Helper()
	r, err := exportKVRepair(reserve, prod, "c", "r", reserve.Version+1, "test")
	require.NoError(t, err)
	return r.Entries
}

func entry(key []byte, newValue []byte, oldValue []byte) kvrepair.Entry {
	e := kvrepair.Entry{Store: keys.EVMStoreKey, Key: kvrepair.HexBytes(key)}
	if newValue != nil {
		v := kvrepair.HexBytes(newValue)
		e.New = &v
	}
	if oldValue == nil {
		e.OldAbsent = true
	} else {
		v := kvrepair.HexBytes(oldValue)
		e.Old = &v
	}
	return e
}

func TestExportStorageSetsDeletesAndCreatesKeys(t *testing.T) {
	addr := addrN(0x01)
	changed, reserveOnly, prodOnly, same := slotN(1), slotN(2), slotN(3), slotN(4)
	reserve := listReport("memiavl", 10, flatkvBucketStorage,
		storageRow(addr, changed, padLeft32(5)),
		storageRow(addr, reserveOnly, padLeft32(7)),
		storageRow(addr, same, padLeft32(9)),
	)
	prod := listReport("composite", 10, flatkvBucketStorage,
		storageRow(addr, same, padLeft32(9)),
		storageRow(addr, prodOnly, padLeft32(8)),
		storageRow(addr, changed, padLeft32(6)),
	)

	require.Equal(t, []kvrepair.Entry{
		entry(keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, changed)), padLeft32(5), padLeft32(6)),
		entry(keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, reserveOnly)), padLeft32(7), nil),
		entry(keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, prodOnly)), nil, padLeft32(8)),
	}, exportForTest(t, reserve, prod))
}

func TestExportDeletesAZeroRowThatOnlyProductionHolds(t *testing.T) {
	addr, slot := addrN(0x01), slotN(1)
	reserve := listReport("memiavl", 10, flatkvBucketStorage)
	prod := listReport("flatkv", 10, flatkvBucketStorage, storageRow(addr, slot, make([]byte, 32)))

	require.Equal(t, []kvrepair.Entry{
		entry(keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), nil, nil),
	}, exportForTest(t, reserve, prod))
}

func TestExportMapsCodeAndMiscKeys(t *testing.T) {
	addr := addrN(0x02)
	codeKey := keys.BuildEVMKey(keys.EVMKeyCode, addr[:])
	reserve := listReport("memiavl", 10, flatkvBucketCode,
		inspectRow{physKey: ktype.EVMPhysicalKey(keys.EVMKeyCode, addr[:]), logical: []byte{0x60, 0x01}})
	prod := listReport("composite", 10, flatkvBucketCode,
		inspectRow{physKey: ktype.EVMPhysicalKey(keys.EVMKeyCode, addr[:]), logical: []byte{0x60, 0x02}})
	require.Equal(t, []kvrepair.Entry{entry(codeKey, []byte{0x60, 0x01}, []byte{0x60, 0x02})},
		exportForTest(t, reserve, prod))

	miscKey := append([]byte{0x09}, addr[:]...)
	reserve = listReport("memiavl", 10, flatkvBucketMisc,
		inspectRow{physKey: ktype.ModulePhysicalKey(keys.EVMStoreKey, miscKey), logical: []byte{}})
	prod = listReport("composite", 10, flatkvBucketMisc,
		inspectRow{physKey: migrationBoundaryPhysKey, logical: []byte{0x01}},
		inspectRow{physKey: migrationVersionPhysKey, logical: []byte{0x02}})
	require.Equal(t, []kvrepair.Entry{entry(miscKey, []byte{}, nil)}, exportForTest(t, reserve, prod),
		"a misc key keeps an empty value distinct from absent, and the migration markers are dropped")
}

func TestExportAccountWritesOnlyTheChangedField(t *testing.T) {
	addr := addrN(0x03)
	codeHash := codeHashOf(0xCC)
	reserve := listReport("memiavl", 10, flatkvBucketAccount, accountRow(addr, padLeft32(5), 2, codeHash[:]))
	prod := listReport("composite", 10, flatkvBucketAccount, accountRow(addr, padLeft32(5), 1, codeHash[:]))

	require.Equal(t, []kvrepair.Entry{
		entry(keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), nonceBytes(2), nonceBytes(1)),
	}, exportForTest(t, reserve, prod))
}

func TestExportAccountOnlyOneSideHolds(t *testing.T) {
	addr := addrN(0x04)
	codeHash := codeHashOf(0xCC)
	balanceKey := keys.BuildEVMKey(keys.EVMKeyBalance, addr[:])
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr[:])
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:])

	reserve := listReport("memiavl", 10, flatkvBucketAccount, accountRow(addr, make([]byte, 32), 3, codeHash[:]))
	prod := listReport("composite", 10, flatkvBucketAccount)
	require.Equal(t, []kvrepair.Entry{
		entry(nonceKey, nonceBytes(3), nil),
		entry(codeHashKey, codeHash[:], nil),
	}, exportForTest(t, reserve, prod), "fields that are zero on both sides are left out")

	reserve = listReport("memiavl", 10, flatkvBucketAccount)
	prod = listReport("composite", 10, flatkvBucketAccount, accountRow(addr, make([]byte, 32), 0, make([]byte, 32)))
	require.Equal(t, []kvrepair.Entry{
		entry(balanceKey, nil, nil),
		entry(nonceKey, nil, nil),
		entry(codeHashKey, nil, nil),
	}, exportForTest(t, reserve, prod), "an all-zero row only production holds is deleted field by field")
}

func TestExportRefusesInputs(t *testing.T) {
	addr := addrN(0x05)
	row := storageRow(addr, slotN(1), padLeft32(1))
	other := storageRow(addr, slotN(1), padLeft32(2))
	valid := func() (evmInspectJSON, evmInspectJSON) {
		return listReport("memiavl", 10, flatkvBucketStorage, row), listReport("composite", 10, flatkvBucketStorage, other)
	}

	for name, tc := range map[string]struct {
		edit         func(reserve, prod *evmInspectJSON)
		repairHeight int64
		err          string
	}{
		"reserve is not memiavl": {
			edit: func(reserve, _ *evmInspectJSON) { reserve.Backend = "flatkv" },
			err:  "reserve report backend",
		},
		"reserve uses the translator": {
			edit: func(reserve, _ *evmInspectJSON) { reserve.Mode = memiavlNormTranslator },
			err:  "translator",
		},
		"production is memiavl": {
			edit: func(_, prod *evmInspectJSON) { prod.Backend = "memiavl" },
			err:  "production report backend",
		},
		"different heights": {
			edit: func(_, prod *evmInspectJSON) { prod.Version = 11 },
			err:  "at height 10 and production report at 11",
		},
		"different buckets": {
			edit: func(_, prod *evmInspectJSON) { prod.InspectBucket = flatkvBucketCode },
			err:  "bucket",
		},
		"different prefixes": {
			edit: func(_, prod *evmInspectJSON) { prod.KeyPrefix = "03" },
			err:  "prefix",
		},
		"different offsets": {
			edit: func(_, prod *evmInspectJSON) { prod.KeyOffset = 0 },
			err:  "offset",
		},
		"not a list": {
			edit: func(_, prod *evmInspectJSON) { prod.List = false },
			err:  "production report is not an inspect list",
		},
		"list cut off": {
			edit: func(reserve, _ *evmInspectJSON) { reserve.Matched = 2 },
			err:  "reserve report lists 1 of 2 matched keys",
		},
		"repair height at the report height": {
			repairHeight: 10,
			err:          "is not above the report height 10",
		},
		"no differences": {
			edit: func(reserve, prod *evmInspectJSON) { prod.Entries = reserve.Entries },
			err:  "no differences",
		},
		"non-evm key": {
			edit: func(_, prod *evmInspectJSON) { prod.Entries[0].Key = fmt.Sprintf("%X", []byte("bank/01")) },
			err:  "is not an EVM row",
		},
		"key in the wrong bucket": {
			edit: func(reserve, prod *evmInspectJSON) {
				reserve.InspectBucket, prod.InspectBucket = flatkvBucketCode, flatkvBucketCode
			},
			err: "does not belong to the code bucket",
		},
		"key listed twice": {
			edit: func(_, prod *evmInspectJSON) { prod.Entries = append(prod.Entries, prod.Entries[0]) },
			err:  "listed twice",
		},
		"short account value": {
			edit: func(reserve, prod *evmInspectJSON) {
				reserve.InspectBucket, prod.InspectBucket = flatkvBucketAccount, flatkvBucketAccount
				reserve.Entries[0].Key = fmt.Sprintf("%X", ktype.EVMPhysicalKey(keys.EVMKeyNonce, addr[:]))
				prod.Entries = nil
				prod.Listed, prod.Matched = 0, 0
			},
			err: "logical value has 32 bytes, want 72",
		},
	} {
		t.Run(name, func(t *testing.T) {
			reserve, prod := valid()
			if tc.edit != nil {
				tc.edit(&reserve, &prod)
			}
			repairHeight := tc.repairHeight
			if repairHeight == 0 {
				repairHeight = 11
			}
			_, err := exportKVRepair(reserve, prod, "c", "r", repairHeight, "test")
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestKVRepairExportCommandWritesALoadableFile(t *testing.T) {
	dir := t.TempDir()
	addr := addrN(0x06)
	writeReport := func(name string, r evmInspectJSON) string {
		data, err := json.Marshal(r)
		require.NoError(t, err)
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, data, 0o600))
		return path
	}
	reservePath := writeReport("reserve.json", listReport("memiavl", 10, flatkvBucketStorage, storageRow(addr, slotN(1), padLeft32(1))))
	prodPath := writeReport("prod.json", listReport("composite", 10, flatkvBucketStorage, storageRow(addr, slotN(1), padLeft32(2))))
	output := filepath.Join(dir, "repair.json")

	cmd := KVRepairExportCmd()
	cmd.SetArgs([]string{"--reserve", reservePath, "--prod", prodPath, "--chain-id", "c", "--name", "r",
		"--repair-height", "11", "-o", output})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	require.NoError(t, cmd.Execute())
	require.Contains(t, stderr.String(), "wrote 1 entries")

	data, err := os.ReadFile(output) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	r, err := kvrepair.Parse(data)
	require.NoError(t, err)
	require.NoError(t, r.Validate(func(store string) bool { return store == keys.EVMStoreKey }))
	require.Equal(t, int64(10), r.ReadHeight)
	require.Equal(t, int64(11), r.Height)
	require.Contains(t, r.Source, "reserve memiavl")
}

// TestExportFromRealInspectReportsRepairsEveryDifference runs the inspect code
// path on a memiavl reserve and a FlatKV production store, exports a repair,
// applies its writes to both stores the way the handler does on every node,
// and checks that the reports then agree.
func TestExportFromRealInspectReportsRepairsEveryDifference(t *testing.T) {
	reserveHome := t.TempDir()
	reserve := newTestMemiavlStore(t, reserveHome)
	defer func() { _ = reserve.Close() }()
	reserveDir := utils.GetCosmosSCStorePath(reserveHome)
	prod, prodDir := newDiskBackedFlatKVStore(t, 1)
	defer func() { _ = prod.Close() }()

	acct, contract := addrN(0x07), addrN(0x08)
	codeHash := codeHashOf(0xAB)
	miscKey := append([]byte{0x09}, contract[:]...)
	commitEVMBlock(t, reserve, prod, []*proto.KVPair{
		noncePair(acct, 1),
		codeHashPair(contract, codeHash),
		codePair(contract, []byte{0x60, 0x01}),
		storagePair(contract, slotN(1), 0x11),
		storagePair(contract, slotN(2), 0x22),
		{Key: miscKey, Value: []byte{0x01}},
	})
	commitDivergentBlock(t, reserve, prod,
		[]*proto.KVPair{noncePair(acct, 5)},
		[]*proto.KVPair{
			noncePair(acct, 4),
			codePair(contract, []byte{0x60, 0x02}),
			storagePair(contract, slotN(1), 0x99),
			{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(contract, slotN(2))), Delete: true},
			storagePair(contract, slotN(3), 0x33),
			{Key: miscKey, Value: []byte{0x02}},
		})

	var repairWrites []*proto.KVPair
	for _, bucket := range flatkvBucketOrder {
		reserveReport := inspectForTest(t, "memiavl", reserveDir, 2, bucket)
		prodReport := inspectForTest(t, "flatkv", prodDir, 2, bucket)
		r, err := exportKVRepair(reserveReport, prodReport, "c", "r-"+bucket, 3, "test")
		require.NoError(t, err, "bucket %s", bucket)
		for _, e := range r.Entries {
			pair := &proto.KVPair{Key: e.Key}
			if e.New == nil {
				pair.Delete = true
			} else {
				pair.Value = *e.New
			}
			repairWrites = append(repairWrites, pair)
		}
	}
	commitEVMBlock(t, reserve, prod, repairWrites)

	for _, bucket := range flatkvBucketOrder {
		reserveReport := inspectForTest(t, "memiavl", reserveDir, 3, bucket)
		prodReport := inspectForTest(t, "flatkv", prodDir, 3, bucket)
		_, err := exportKVRepair(reserveReport, prodReport, "c", "r-"+bucket, 4, "test")
		require.ErrorContains(t, err, "no differences", "bucket %s", bucket)
	}
}

func commitEVMBlock(t *testing.T, reserve *memiavl.CommitStore, prod *flatkv.CommitStore, pairs []*proto.KVPair) {
	t.Helper()
	commitDivergentBlock(t, reserve, prod, pairs, pairs)
}

func commitDivergentBlock(t *testing.T, reserve *memiavl.CommitStore, prod *flatkv.CommitStore, reservePairs, prodPairs []*proto.KVPair) {
	t.Helper()
	require.NoError(t, reserve.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: reservePairs},
	}}))
	_, err := reserve.Commit(reserve.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, prod.ApplyChangeSets(prod.Version()+1, []*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: prodPairs},
	}}))
	_, err = prod.Commit(prod.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, prod.FlushSnapshots())
}

func inspectForTest(t *testing.T, backend, dbDir string, height int64, bucket string) evmInspectJSON {
	t.Helper()
	cmd := EvmLogicalDigestCmd()
	require.NoError(t, cmd.Flags().Set("key-offset", fmt.Sprint(testEVMKeyOffset)))
	require.NoError(t, cmd.Flags().Set("list", "true"))
	require.NoError(t, cmd.Flags().Set("list-limit", "0"))
	_, jsonReport := captureDigestOutput(t, true)
	require.NoError(t, runEvmLogicalInspect(cmd, backend, dbDir, "", "", height, bucket,
		memiavlNormSemantic, memiavlOpenModeReplay))
	var report evmInspectJSON
	require.NoError(t, json.Unmarshal(jsonReport.Bytes(), &report))
	return report
}
