package evmonlyapp

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// requireHistoryOfTwoBlocks finalizes blocks 1 and 2 of app and requires the history storage
// contract to hold block 1's hash.
func requireHistoryOfTwoBlocks(t *testing.T, app abevm.Application) {
	t.Helper()
	first := evmOnlyTestBlock(app, 1)
	finalizeEVMOnlyTestBlock(t, app, first)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2))

	evmApp := app.(*evmOnlyApplication)
	require.Equal(t, params.HistoryStorageCode, evmApp.EvmCode(params.HistoryStorageAddress))
	got, err := evmApp.EvmStorageAt(params.HistoryStorageAddress, common.BigToHash(common.Big1), 2)
	require.NoError(t, err)
	require.Equal(t, first.Hash, got)
}

func TestEVMOnlyChainWithoutAnAllocKeepsBlockHistory(t *testing.T) {
	requireHistoryOfTwoBlocks(t, newInitializedEVMOnlyTestApp(t))
}

func TestEVMOnlyChainWithAnAllocKeepsBlockHistory(t *testing.T) {
	app := newEVMOnlyTestAppWithAlloc(t, GenesisAlloc{InitialHeight: 1, Accounts: map[common.Address]evmonly.GenesisAccount{{0xf1}: {Balance: uint256.NewInt(1_000)}}})
	require.NoError(t, app.InitChain(evmOnlyTestInitChain()))
	requireHistoryOfTwoBlocks(t, app)
}

func TestEVMOnlyApplicationRefusesAnAllocSettingAProtocolAccount(t *testing.T) {
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	alloc := GenesisAlloc{InitialHeight: 1, Accounts: map[common.Address]evmonly.GenesisAccount{
		{0xf1}:                       {Balance: uint256.NewInt(1_000)},
		params.HistoryStorageAddress: {Code: []byte{0x00}},
	}}

	_, err = NewEVMOnlyApplication(
		evmOnlyTestChainID, nil, storage,
		openEVMOnlyTestEpochs(t, storage, nil, 1),
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.Some(alloc),
		utils.None[string](),
	)
	require.Error(t, err)
}
