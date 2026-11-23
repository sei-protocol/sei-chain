package evmonlyapp

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// evmOnlyBaseFeeTestGasLimit is a gas limit three transfers, 63,000 gas, use more than half of.
const evmOnlyBaseFeeTestGasLimit = 100_000

// evmOnlyTestEVMParams are EIP-1559's adjustments, 12.5% each way, with the genesis minimum fee
// and lag.
var evmOnlyTestEVMParams = chainparams.EVM{
	MaxDynamicBaseFeeUpwardAdjustment:   big.NewInt(125_000_000_000_000_000),
	MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(125_000_000_000_000_000),
	MinFeePerGas:                        evmOnlyGenesisEVMParams.MinFeePerGas,
	BaseFeeLag:                          evmOnlyGenesisEVMParams.BaseFeeLag,
}

// setEVMOnlyGenesisEVMParams sets the genesis base fee params for the rest of the test.
func setEVMOnlyGenesisEVMParams(t *testing.T, p chainparams.EVM) {
	t.Helper()
	saved := evmOnlyGenesisEVMParams
	evmOnlyGenesisEVMParams = p
	t.Cleanup(func() { evmOnlyGenesisEVMParams = saved })
}

// genesisBaseFeeRules returns the base fee rules of evmOnlyGenesisEVMParams.
func genesisBaseFeeRules() baseFeeRules {
	return baseFeeRulesOf(evmOnlyGenesisEVMParams)
}

// decWei returns wei as a Dec.
func decWei(wei uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(wei), evmonly.BaseFeeAdjustmentOne.ToBig())
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
	require.NoError(t, app.InitChain(evmOnlyBaseFeeTestInitChain()))
	return app
}

// evmOnlyBaseFeeTestInitChain is evmOnlyTestInitChain with a gas limit of
// evmOnlyBaseFeeTestGasLimit.
func evmOnlyBaseFeeTestInitChain() abevm.InitConfig {
	req := evmOnlyTestInitChain()
	req.GasLimit = evmOnlyBaseFeeTestGasLimit
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

// threeTransfersBlock returns app's block height holding three transfers, 63,000 gas.
func threeTransfersBlock(t *testing.T, app abevm.Application, height int64) abevm.FinalizeBlockReq {
	t.Helper()
	return evmOnlyTestBlock(app, height, oneEVMOnlyTestTx(t), oneEVMOnlyTestTx(t), oneEVMOnlyTestTx(t))
}

// raisedBaseFee returns the base fee three transfers set under the genesis rules, from scheduled,
// in a block of evmOnlyBaseFeeTestGasLimit.
func raisedBaseFee(scheduled uint256.Int) uint256.Int {
	return genesisBaseFeeRules().step.NextBaseFee(scheduled, 63_000, evmOnlyBaseFeeTestGasLimit)
}

// The gas a block uses sets the base fee two blocks later.
func TestEVMOnlyApplicationSetsTheBaseFeeTwoBlocksAhead(t *testing.T) {
	app := newEVMOnlyBaseFeeTestApp(t)
	genesis := *uint256.NewInt(evmOnlyGenesisMinFeePerGas)
	raised := raisedBaseFee(genesis)
	require.Greater(t, raised.Uint64(), genesis.Uint64())

	finalizeEVMOnlyTestBlock(t, app, threeTransfersBlock(t, app, 1))
	require.Equal(t, genesis.ToBig(), app.EvmBaseFee())
	require.Equal(t, genesis.ToBig(), app.EvmNextBaseFee())
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2))
	require.Equal(t, genesis.ToBig(), app.EvmBaseFee())
	require.Equal(t, raised.ToBig(), app.EvmNextBaseFee())
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
	require.Equal(t, raised.ToBig(), app.EvmBaseFee())
}

// A chain starts at its minimum fee, and empty blocks do not lower the base fee below it.
func TestEVMOnlyApplicationKeepsTheBaseFeeAtTheMinimumFee(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	minGasPrice := big.NewInt(evmOnlyGenesisMinFeePerGas)

	require.Equal(t, minGasPrice, app.EvmNextBaseFee())
	for height := int64(1); height <= 4; height++ {
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height))
		require.Equal(t, minGasPrice, app.EvmBaseFee())
	}
	require.Equal(t, minGasPrice, app.EvmMinGasPrice())
}

// Before InitChain, an app answers for the genesis base fees.
func TestEVMOnlyApplicationReportsGenesisBaseFeesBeforeInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil).(*evmOnlyApplication)
	genesis := big.NewInt(evmOnlyGenesisMinFeePerGas)

	require.Equal(t, genesis, app.EvmBaseFee())
	require.Equal(t, genesis, app.EvmNextBaseFee())
	require.Equal(t, app.admissionMinGasPrice(genesis), app.EvmMinGasPrice())
}

// CheckTx prices against the highest base fee already set, so a transaction it admits can execute
// at every height whose base fee is known.
func TestEVMOnlyApplicationAdmitsAtTheHighestScheduledBaseFee(t *testing.T) {
	app := newEVMOnlyBaseFeeTestApp(t)
	raised := raisedBaseFee(*uint256.NewInt(evmOnlyGenesisMinFeePerGas))

	finalizeEVMOnlyTestBlock(t, app, threeTransfersBlock(t, app, 1))

	// Block 2 still runs at the genesis base fee, but block 3 runs at raised.
	require.Equal(t, raised.ToBig(), app.EvmMinGasPrice())
	atGenesis := signedEVMOnlyTestTxAtPrice(t, big.NewInt(evmOnlyGenesisMinFeePerGas))
	require.Error(t, admissionError(app, atGenesis))
	atRaised := signedEVMOnlyTestTxAtPrice(t, raised.ToBig())
	require.NoError(t, admissionError(app, atRaised))
}

// A transaction priced below its block's base fee is refused by CheckTx and, included anyway, uses
// no gas rather than failing the block.
func TestEVMOnlyApplicationSkipsATxBelowTheBaseFee(t *testing.T) {
	app := newEVMOnlyBaseFeeTestApp(t)
	seedEVMOnlyBaseFees(app, 2*evmOnlyMinGasPrice) // above this node's MinGasPrice
	below := signedEVMOnlyTestTxAtPrice(t, big.NewInt(2*evmOnlyMinGasPrice-1))
	at := signedEVMOnlyTestTxAtPrice(t, big.NewInt(2*evmOnlyMinGasPrice))
	require.Error(t, admissionError(app, below))

	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 1, below, at))

	require.NoError(t, err)
	require.Equal(t, uint64(21_000), response.GasUsed, "only the transaction paying the base fee ran")
}

// A block executed at a base fee other than the one scheduled for it fails to finalize.
func TestEVMOnlyApplicationRefusesABlockRunAtAnUnscheduledBaseFee(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	installEVMExecutorEncoder(app, func(block evmonly.BlockContext, state evmonly.StateReader, result *evmonly.BlockResult) ([]gigatypes.MiscChange, error) {
		block.BaseFee = new(big.Int).Add(block.BaseFee, big.NewInt(1))
		return app.encodeCursorChangeSet(block, state, result)
	})

	_, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(app, 1))

	require.Error(t, err)
	require.Contains(t, err.Error(), "not its scheduled")
}

// The base fees a block set survive a restart, so the resumed node runs the next blocks at those
// fees.
func TestEVMOnlyApplicationResumesItsBaseFeesAfterRestart(t *testing.T) {
	raised := raisedBaseFee(*uint256.NewInt(evmOnlyGenesisMinFeePerGas))
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := newEVMOnlyTestAppOnChain(t, storage, evmOnlyTestChainID)
	require.NoError(t, err)
	require.NoError(t, app.InitChain(evmOnlyBaseFeeTestInitChain()))
	finalizeEVMOnlyTestBlock(t, app, threeTransfersBlock(t, app, 1))

	app, storage = reopenEVMOnlyTestApp(t, app, storage, home)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })

	require.Equal(t, raised.ToBig(), app.EvmMinGasPrice())
	finalizeEVMOnlyTestBlock(t, app, threeTransfersBlock(t, app, 2))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
	require.Equal(t, raised.ToBig(), app.EvmBaseFee())
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 4))
	raisedAgain := raisedBaseFee(raised)
	require.Equal(t, raisedAgain.ToBig(), app.EvmBaseFee())
}

// A chain running gov writes the genesis base fee params at its first block, and from then on
// sets base fees by the params in state rather than the compiled-in ones.
func TestEVMOnlyApplicationSetsBaseFeesByTheParamsInState(t *testing.T) {
	setEVMOnlyGenesisEVMParams(t, evmOnlyTestEVMParams)
	app := newGovEVMOnlyTestApp(t, gov.DefaultParams(), 0).(*evmOnlyApplication)
	seedEVMOnlyBaseFees(app, 100*evmOnlyGenesisMinFeePerGas) // so empty blocks lower it
	gasLimit := evmOnlyTestInitChain().GasLimit

	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))
	out, err := precompileCall(t, app, chainparams.Address, chainparams.ABI, chainparams.ParamMethod, "evm", "KeyMaxDynamicBaseFeeDownwardAdjustment")
	require.NoError(t, err)
	require.Equal(t, common.BigToHash(evmOnlyTestEVMParams.MaxDynamicBaseFeeDownwardAdjustment).Bytes(), out[0].([]byte))

	evmOnlyGenesisEVMParams.MaxDynamicBaseFeeDownwardAdjustment = big.NewInt(500_000_000_000_000_000)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
	var atBlock3 uint256.Int
	atBlock3.SetFromBig(app.EvmBaseFee())
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 4))

	// Empty block 2 set block 4's base fee from block 3's.
	inState := baseFeeRulesOf(evmOnlyTestEVMParams).step.NextBaseFee(atBlock3, 0, gasLimit)
	compiledIn := genesisBaseFeeRules().step.NextBaseFee(atBlock3, 0, gasLimit)
	require.NotEqual(t, compiledIn, inState)
	require.Equal(t, inState.ToBig(), app.EvmBaseFee())
}

// A raised minimum fee sets the base fee of each block after those already set, and CheckTx
// admits at it once it is set.
func TestEVMOnlyApplicationFollowsTheMinimumFeeParam(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	genesis := big.NewInt(evmOnlyGenesisMinFeePerGas)
	raised := big.NewInt(3 * evmOnlyGenesisMinFeePerGas)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))

	raisedParams := evmOnlyGenesisEVMParams
	raisedParams.MinFeePerGas = decWei(3 * evmOnlyGenesisMinFeePerGas)
	setEVMOnlyGenesisEVMParams(t, raisedParams)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2))

	require.Equal(t, genesis, app.EvmNextBaseFee(), "block 3's base fee was already set")
	require.Equal(t, raised, app.EvmMinGasPrice(), "block 4's base fee is the raised minimum")
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
	require.Equal(t, genesis, app.EvmBaseFee())
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 4))
	require.Equal(t, raised, app.EvmBaseFee())
}

// The gas a block uses sets the base fee the lag param's number of blocks later.
func TestEVMOnlyApplicationFollowsTheLagParam(t *testing.T) {
	threeBlocks := evmOnlyGenesisEVMParams
	threeBlocks.BaseFeeLag = 3
	setEVMOnlyGenesisEVMParams(t, threeBlocks)
	app := newEVMOnlyBaseFeeTestApp(t)
	genesis := *uint256.NewInt(evmOnlyGenesisMinFeePerGas)
	raised := raisedBaseFee(genesis)

	finalizeEVMOnlyTestBlock(t, app, threeTransfersBlock(t, app, 1))
	for height := int64(2); height <= 3; height++ {
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, height))
		require.Equal(t, genesis.ToBig(), app.EvmBaseFee(), "block %d", height)
	}
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 4))
	require.Equal(t, raised.ToBig(), app.EvmBaseFee())
}

// The genesis base fee params: a 0.6% adjustment each way, two blocks later, never below 1 gwei.
func TestEVMOnlyGenesisEVMParams(t *testing.T) {
	require.Equal(t, uint64(1_000_000_000), uint64(evmOnlyGenesisMinFeePerGas))
	require.Equal(t, chainparams.EVM{
		MaxDynamicBaseFeeUpwardAdjustment:   big.NewInt(6_000_000_000_000_000),
		MaxDynamicBaseFeeDownwardAdjustment: big.NewInt(6_000_000_000_000_000),
		MinFeePerGas:                        new(big.Int).Mul(big.NewInt(1_000_000_000), big.NewInt(1_000_000_000_000_000_000)),
		BaseFeeLag:                          2,
	}, evmOnlyGenesisEVMParams)
	require.NoError(t, evmOnlyGenesisEVMParams.Validate())
	require.Equal(t, *uint256.NewInt(1_000_000_000), genesisBaseFeeRules().step.MinBaseFee)
}
