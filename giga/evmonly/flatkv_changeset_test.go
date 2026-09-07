package evmonly

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

func TestFlatKVChangeSetEncoderPersistsExecutorState(t *testing.T) {
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = t.TempDir()
	store, err := openFlatKVTestStore(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	address := common.Address{0x11}
	slotA, slotB := common.Hash{0x21}, common.Hash{0x22}
	encode := NewFlatKVChangeSetEncoder(store)
	changes, err := encode(StateChangeSet{
		Nonces: []NonceChange{{Address: address, Nonce: 7}},
		Code:   []CodeChange{{Address: address, Code: []byte{0x60, 0x01}}},
		Storage: []StorageChange{
			{Address: address, Key: slotA, Value: common.Hash{0xaa}},
			{Address: address, Key: slotB, Value: common.Hash{0xbb}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.CommitStateChanges(1, changes))

	view := store.OpenView()
	require.Equal(t, uint64(7), view.GetNonce(address))
	require.Equal(t, []byte{0x60, 0x01}, view.GetCode(address))
	require.Equal(t, common.Hash{0xaa}, view.GetStorage(address, slotA))
	view.Close()

	changes, err = encode(StateChangeSet{
		StorageClears: []common.Address{address},
		Storage:       []StorageChange{{Address: address, Key: slotB, Value: common.Hash{0xcc}}},
	})
	require.NoError(t, err)
	require.NoError(t, store.CommitStateChanges(2, changes))

	view = store.OpenView()
	defer view.Close()
	require.Equal(t, common.Hash{}, view.GetStorage(address, slotA))
	require.Equal(t, common.Hash{0xcc}, view.GetStorage(address, slotB))
}

func openFlatKVTestStore(ctx context.Context, cfg *flatkvconfig.Config) (*flatkv.CommitStore, error) {
	store, err := flatkv.NewCommitStore(ctx, cfg, nil)
	if err != nil {
		return nil, err
	}
	if err := store.LoadLatest(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}
