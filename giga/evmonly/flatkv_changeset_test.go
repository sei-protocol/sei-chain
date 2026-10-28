package evmonly

import (
	"context"
	"math/big"
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
	changes, err := NewFlatKVChangeSetEncoder()(StateChangeSet{
		Accounts: []AccountChange{{
			Address: address,
			Account: AccountState{Balance: big.NewInt(99), Nonce: 7, Code: []byte{0x60, 0x01}},
		}},
		Storage: []StorageChange{
			{Address: address, Key: slotA, Value: common.Hash{0xaa}},
			{Address: address, Key: slotB, Value: common.Hash{0xbb}},
		},
	})
	require.NoError(t, err)
	prior := store.OpenView()
	mutations, err := flatkv.MutationsFromChangeSets(changes, 1, prior)
	prior.Close()
	require.NoError(t, err)
	require.NoError(t, store.CommitStateChanges(1, mutations))

	view := store.OpenView()
	defer view.Close()
	require.Equal(t, common.BigToHash(big.NewInt(99)), view.GetBalance(address))
	require.Equal(t, uint64(7), view.GetNonce(address))
	require.Equal(t, []byte{0x60, 0x01}, view.GetCode(address))
	require.Equal(t, common.Hash{0xaa}, view.GetStorage(address, slotA))
	require.Equal(t, common.Hash{0xbb}, view.GetStorage(address, slotB))
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
