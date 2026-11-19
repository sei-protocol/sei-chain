package evmonlyapp

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// evmOnlyBaseFeeTestGasLimit is a gas limit three transfers, 63,000 gas, use more than half of.
const evmOnlyBaseFeeTestGasLimit = 100_000

// evmOnlyTestEVMParams are EIP-1559's adjustments, 12.5% each way.
var evmOnlyTestEVMParams = chainparams.EVM{
	MaxDynamicBaseFeeUpwardAdjustment:   big.NewInt(125_000_000_000_000_000),
	MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(125_000_000_000_000_000),
}

// setEVMOnlyGenesisEVMParams sets the genesis base fee params for the rest of the test.
func setEVMOnlyGenesisEVMParams(t *testing.T, p chainparams.EVM) {
	t.Helper()
	saved := evmOnlyGenesisEVMParams
	evmOnlyGenesisEVMParams = p
	t.Cleanup(func() { evmOnlyGenesisEVMParams = saved })
}

// genesisBaseFeeParams returns the base fee rules of evmOnlyGenesisEVMParams.
func genesisBaseFeeParams() evmonly.BaseFeeParams {
	return baseFeeParams(evmOnlyGenesisEVMParams)
}

// seedEVMOnlyBaseFees sets the base fee app's committed block ran at, and each one set after it,
// to fee.
func seedEVMOnlyBaseFees(app *evmOnlyApplication, fee uint64) {
	for state := range app.cursor.Lock() {
		for i := range state.committed.baseFees {
			state.committed.baseFees[i].SetUint64(fee)
		}
	}
}

// newEVMOnlyBaseFeeTestApp returns an app initialized with evmOnlyBaseFeeTestGasLimit.
func newEVMOnlyBaseFeeTestApp(t *testing.T) *evmOnlyApplication {
	t.Helper()
	app := newEVMOnlyTestApp(t, nil).(*evmOnlyApplication)
	_, err := app.InitChain(evmOnlyBaseFeeTestInitChain())
	require.NoError(t, err)
	return app
}

// evmOnlyBaseFeeTestInitChain is evmOnlyTestInitChain with a gas limit of
// evmOnlyBaseFeeTestGasLimit.
func evmOnlyBaseFeeTestInitChain() *proxy.RequestInitChain {
	req := evmOnlyTestInitChain()
	req.MaxGas = evmOnlyBaseFeeTestGasLimit
	return req
}

// signedEVMOnlyTestTxAtPrice signs a transfer from a fresh sender at gasPrice.
func signedEVMOnlyTestTxAtPrice(t *testing.T, gasPrice *big.Int) []byte {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{GasPrice: gasPrice, Gas: 21_000, To: &recipient, Value: big.NewInt(1)})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(evmOnlyTestChainID)), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

// threeTransfersBlock returns block height holding three transfers, 63,000 gas.
func threeTransfersBlock(t *testing.T, height int64) *proxy.RequestFinalizeBlock {
	t.Helper()
	return evmOnlyTestBlock(height, oneEVMOnlyTestTx(t), oneEVMOnlyTestTx(t), oneEVMOnlyTestTx(t))
}

// raisedBaseFee returns the base fee three transfers set under the genesis rules, from scheduled,
// in a block of evmOnlyBaseFeeTestGasLimit.
func raisedBaseFee(scheduled uint256.Int) uint256.Int {
	return genesisBaseFeeParams().NextBaseFee(scheduled, 63_000, evmOnlyBaseFeeTestGasLimit)
}

// The gas a block uses sets the base fee two blocks later.
func TestEVMOnlyApplicationSetsTheBaseFeeTwoBlocksAhead(t *testing.T) {
	app := newEVMOnlyBaseFeeTestApp(t)
	genesis := *uint256.NewInt(evmOnlyBlockMinGasPrice)
	raised := raisedBaseFee(genesis)
	require.Greater(t, raised.Uint64(), genesis.Uint64())

	finalizeAndCommitEVMOnlyTestBlock(t, app, threeTransfersBlock(t, 1))
	require.Equal(t, genesis.ToBig(), app.EvmBaseFee())
	require.Equal(t, genesis.ToBig(), app.EvmNextBaseFee())
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))
	require.Equal(t, genesis.ToBig(), app.EvmBaseFee())
	require.Equal(t, raised.ToBig(), app.EvmNextBaseFee())
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
	require.Equal(t, raised.ToBig(), app.EvmBaseFee())
}

// A chain starts at its minimum gas price, and empty blocks do not lower the base fee below it.
func TestEVMOnlyApplicationKeepsTheBaseFeeAtTheMinGasPrice(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	minGasPrice := big.NewInt(evmOnlyBlockMinGasPrice)

	require.Equal(t, minGasPrice, app.EvmNextBaseFee())
	for height := int64(1); height <= 4; height++ {
		finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(height))
		require.Equal(t, minGasPrice, app.EvmBaseFee())
	}
	require.Equal(t, minGasPrice, app.EvmMinGasPrice())
}

// CheckTx prices against the highest base fee already set, so a transaction it admits can execute
// at every height whose base fee is known.
func TestEVMOnlyApplicationAdmitsAtTheHighestScheduledBaseFee(t *testing.T) {
	app := newEVMOnlyBaseFeeTestApp(t)
	raised := raisedBaseFee(*uint256.NewInt(evmOnlyBlockMinGasPrice))

	finalizeAndCommitEVMOnlyTestBlock(t, app, threeTransfersBlock(t, 1))

	// Block 2 still runs at the genesis base fee, but block 3 runs at raised.
	require.Equal(t, raised.ToBig(), app.EvmMinGasPrice())
	atGenesis := signedEVMOnlyTestTxAtPrice(t, big.NewInt(evmOnlyBlockMinGasPrice))
	require.False(t, app.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: atGenesis}).IsOK())
	atRaised := signedEVMOnlyTestTxAtPrice(t, raised.ToBig())
	require.True(t, app.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: atRaised}).IsOK())
}

// A transaction priced below its block's base fee is refused by CheckTx and, included anyway, is
// recorded as a failure that used no gas rather than failing the block.
func TestEVMOnlyApplicationRecordsATxBelowTheBaseFeeAsFailed(t *testing.T) {
	app := newEVMOnlyBaseFeeTestApp(t)
	seedEVMOnlyBaseFees(app, 2*evmOnlyMinGasPrice) // above this node's MinGasPrice
	raw := signedEVMOnlyTestTxAtPrice(t, big.NewInt(2*evmOnlyMinGasPrice-1))
	require.False(t, app.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: raw}).IsOK())

	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(1, raw))

	require.NoError(t, err)
	require.Len(t, response.TxResults, 1)
	require.Equal(t, int64(0), response.TxResults[0].GasUsed)
	require.Contains(t, response.TxResults[0].Log, ethcore.ErrFeeCapTooLow.Error())
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
}

// A block executed at a base fee other than the one scheduled for it fails to finalize.
func TestEVMOnlyApplicationRefusesABlockRunAtAnUnscheduledBaseFee(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	installEVMExecutorEncoder(app, func(block evmonly.BlockContext, state evmonly.StateReader, result *evmonly.BlockResult) ([]gigatypes.MiscChange, error) {
		block.BaseFee = new(big.Int).Add(block.BaseFee, big.NewInt(1))
		return app.encodeCursorChangeSet(block, state, result)
	})

	_, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(1))

	require.Error(t, err)
	require.Contains(t, err.Error(), "not its scheduled")
}

// The base fees a block set survive a restart, so the resumed node runs the next blocks at those
// fees.
func TestEVMOnlyApplicationResumesItsBaseFeesAfterRestart(t *testing.T) {
	raised := raisedBaseFee(*uint256.NewInt(evmOnlyBlockMinGasPrice))
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := newEVMOnlyTestAppOnChain(storage, evmOnlyTestChainID)
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyBaseFeeTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, app, threeTransfersBlock(t, 1))

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	require.Equal(t, raised.ToBig(), app.EvmMinGasPrice())
	finalizeAndCommitEVMOnlyTestBlock(t, app, threeTransfersBlock(t, 2))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
	require.Equal(t, raised.ToBig(), app.EvmBaseFee())
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(4))
	raisedAgain := raisedBaseFee(raised)
	require.Equal(t, raisedAgain.ToBig(), app.EvmBaseFee())
}

// A chain running gov writes the genesis base fee params at its first block, and from then on
// sets base fees by the params in state rather than the compiled-in ones.
func TestEVMOnlyApplicationSetsBaseFeesByTheParamsInState(t *testing.T) {
	setEVMOnlyGenesisEVMParams(t, evmOnlyTestEVMParams)
	app := newGovEVMOnlyTestApp(t, gov.DefaultParams(), 0).(*evmOnlyApplication)
	seedEVMOnlyBaseFees(app, 100*evmOnlyBlockMinGasPrice) // so empty blocks lower it
	gasLimit := uint64(evmOnlyTestInitChain().MaxGas)

	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))
	out, err := precompileCall(t, app, chainparams.Address, chainparams.ABI, chainparams.ParamMethod, "evm", "KeyMaxDynamicBaseFeeDownwardAdjustment")
	require.NoError(t, err)
	require.Equal(t, common.BigToHash(evmOnlyTestEVMParams.MaxDynamicBaseFeeDownwardAdjustment).Bytes(), out[0].([]byte))

	evmOnlyGenesisEVMParams.MaxDynamicBaseFeeDownwardAdjustment = big.NewInt(500_000_000_000_000_000)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
	var atBlock3 uint256.Int
	atBlock3.SetFromBig(app.EvmBaseFee())
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(4))

	// Empty block 2 set block 4's base fee from block 3's.
	inState := baseFeeParams(evmOnlyTestEVMParams).NextBaseFee(atBlock3, 0, gasLimit)
	compiledIn := genesisBaseFeeParams().NextBaseFee(atBlock3, 0, gasLimit)
	require.NotEqual(t, compiledIn, inState)
	require.Equal(t, inState.ToBig(), app.EvmBaseFee())
}

// The genesis base fee params, a 0.6% adjustment each way, and the chain's 1 gwei minimum gas price.
func TestEVMOnlyGenesisEVMParams(t *testing.T) {
	require.Equal(t, uint64(1_000_000_000), uint64(evmOnlyBlockMinGasPrice))
	require.Equal(t, chainparams.EVM{
		MaxDynamicBaseFeeUpwardAdjustment:   big.NewInt(6_000_000_000_000_000),
		MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(6_000_000_000_000_000),
	}, evmOnlyGenesisEVMParams)
	require.NoError(t, evmOnlyGenesisEVMParams.Validate())
	require.Equal(t, 2, evmOnlyBaseFeeLag)
}
