package evmonlyapp

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/export"
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

	require.Equal(t, params.HistoryStorageCode, app.EvmCode(params.HistoryStorageAddress))
	got, err := app.EvmStorageAt(params.HistoryStorageAddress, common.BigToHash(common.Big1), 2)
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

func TestEVMOnlyApplicationTraceContextReadsBlockHistory(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	first := evmOnlyTestBlock(app, 1)
	finalizeEVMOnlyTestBlock(t, app, first)
	second := evmOnlyTestBlock(app, 2)
	finalizeEVMOnlyTestBlock(t, app, second)
	third := evmOnlyTestBlock(app, 3)
	finalizeEVMOnlyTestBlock(t, app, third)
	parent, err := app.EvmStateAt(t.Context(), 3)
	require.NoError(t, err)
	defer parent.Close()

	got, err := app.EvmBlockContext(traceTestHeader(4, third.Hash), parent)

	require.NoError(t, err)
	require.Equal(t, first.Hash, got.GetHash(1))
	require.Equal(t, second.Hash, got.GetHash(2))
	require.Equal(t, third.Hash, got.GetHash(3))
	require.Equal(t, common.Hash{}, got.GetHash(4))
}

// parentHashesCode returns BLOCKHASH(NUMBER-1) and BLOCKHASH(NUMBER-2).
var parentHashesCode = hexutil.Bytes{
	0x43, 0x60, 0x01, 0x90, 0x03, 0x40, 0x5f, 0x52, // mstore(0, blockhash(number - 1))
	0x43, 0x60, 0x02, 0x90, 0x03, 0x40, 0x60, 0x20, 0x52, // mstore(32, blockhash(number - 2))
	0x60, 0x40, 0x5f, 0xf3, // return(0, 64)
}

func TestEVMOnlyApplicationLatestCallReadsBlockHistory(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	first := evmOnlyTestBlock(app, 1)
	finalizeEVMOnlyTestBlock(t, app, first)
	second := evmOnlyTestBlock(app, 2)
	finalizeEVMOnlyTestBlock(t, app, second)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
	reader := common.Address{0xb3}

	result, err := app.EvmCall(t.Context(), callMessage(common.Address{0xf1}, &reader), abevm.CallOverrides{
		State: export.StateOverride{reader: {Code: &parentHashesCode}},
	})

	require.NoError(t, err)
	require.False(t, result.Failed())
	require.Equal(t, append(second.Hash.Bytes(), first.Hash.Bytes()...), result.ReturnData)
}
