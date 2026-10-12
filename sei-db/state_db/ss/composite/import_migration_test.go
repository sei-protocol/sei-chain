package composite

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	sccomposite "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// TestImport_MidMigrationSnapshotRestoresNonce restores a snapshot taken while
// the EVM migration boundary sits between an address's code hash key and its
// nonce key, and checks that the state store serves the committed nonce.
func TestImport_MidMigrationSnapshotRestoresNonce(t *testing.T) {
	addr := make([]byte, 20)
	for i := range addr {
		addr[i] = 0xAB
	}
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, addr)
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, addr)
	codeHash := make([]byte, 32)
	for i := range codeHash {
		codeHash[i] = 0x11
	}
	nonce := make([]byte, 8)
	binary.BigEndian.PutUint64(nonce, 5)

	version, nodes := exportMidMigrationSnapshot(t, []*proto.KVPair{
		{Key: codeHashKey, Value: codeHash},
		{Key: nonceKey, Value: nonce},
	})

	// The stream must carry the nonce in memIAVL and the account row in FlatKV,
	// or the restore below passes without exercising the conflict.
	acctKey := ktype.EVMPhysicalKey(keys.EVMKeyNonce, addr)
	var sawMemIAVLNonce, sawFlatKVAccount bool
	for _, n := range nodes {
		sawMemIAVLNonce = sawMemIAVLNonce || (n.StoreKey == keys.EVMStoreKey && bytes.Equal(n.Key, nonceKey))
		sawFlatKVAccount = sawFlatKVAccount || (n.StoreKey == keys.FlatKVStoreKey && bytes.Equal(n.Key, acctKey))
	}
	require.True(t, sawMemIAVLNonce && sawFlatKVAccount, "snapshot is not mid-migration")

	for _, tc := range []struct {
		name              string
		evmSplit          bool
		separateEVMSubDBs bool
	}{
		{name: "cosmos", evmSplit: false},
		{name: "evm-split", evmSplit: true},
		{name: "evm-separate-sub-dbs", evmSplit: true, separateEVMSubDBs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ssCfg := config.DefaultStateStoreConfig()
			ssCfg.Backend = config.PebbleDBBackend
			ssCfg.EVMSplit = tc.evmSplit
			ssCfg.SeparateEVMSubDBs = tc.separateEVMSubDBs
			store, err := NewCompositeStateStore(ssCfg, t.TempDir())
			require.NoError(t, err)
			defer func() { _ = store.Close() }()

			ch := make(chan types.SnapshotNode, len(nodes))
			for _, n := range nodes {
				ch <- n
			}
			close(ch)
			require.NoError(t, store.Import(version, ch))

			got, err := store.Get(keys.EVMStoreKey, version, nonceKey)
			require.NoError(t, err)
			require.Equal(t, nonce, got)

			got, err = store.Get(keys.EVMStoreKey, version, codeHashKey)
			require.NoError(t, err)
			require.Equal(t, codeHash, got)
		})
	}
}

// exportMidMigrationSnapshot commits pairs to the evm store on memIAVL, migrates
// exactly one key to FlatKV, and returns the export version with the leaf nodes
// the snapshot stream carries to the state store importer.
func exportMidMigrationSnapshot(t *testing.T, pairs []*proto.KVPair) (int64, []types.SnapshotNode) {
	t.Helper()
	dir := t.TempDir()
	stores := []string{keys.BankStoreKey, keys.EVMStoreKey}

	memCfg := config.DefaultStateCommitConfig()
	memCfg.WriteMode = sctypes.MemiavlOnly
	memCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cs, err := sccomposite.NewCompositeCommitStore(t.Context(), dir, memCfg)
	require.NoError(t, err)
	require.NoError(t, cs.Initialize(stores))
	require.NoError(t, cs.LoadLatest())
	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}))
	_, err = cs.Commit(cs.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, cs.Close())

	migCfg := config.DefaultStateCommitConfig()
	migCfg.WriteMode = sctypes.MigrateEVM
	migCfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cs, err = sccomposite.NewCompositeCommitStore(t.Context(), dir, migCfg)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()
	require.NoError(t, cs.SetMigrationBatchSize(1))
	require.NoError(t, cs.Initialize(stores))
	require.NoError(t, cs.LoadLatest())
	require.NoError(t, cs.ApplyChangeSets(nil))
	version, err := cs.Commit(cs.Version() + 1)
	require.NoError(t, err)

	exporter, err := cs.Exporter(version)
	require.NoError(t, err)
	defer func() { require.NoError(t, exporter.Close()) }()
	var nodes []types.SnapshotNode
	module := ""
	for {
		item, err := exporter.Next()
		if errors.Is(err, errorutils.ErrorExportDone) {
			break
		}
		require.NoError(t, err)
		switch v := item.(type) {
		case string:
			module = v
		case *sctypes.SnapshotNode:
			if v.Height == 0 {
				nodes = append(nodes, types.SnapshotNode{StoreKey: module, Key: v.Key, Value: v.Value})
			}
		}
	}
	return version, nodes
}
