package evmonlyapp

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// evmOnlyTestBlockInterval is the time between consecutive test blocks, and between genesis and
// the first.
const evmOnlyTestBlockInterval = uint64(time.Second)

// evmOnlyTestEVMParams are base fee params with a floor at the default admission price, under which
// a test block of three transfers, 63,000 gas a second after its parent, is above its target.
var evmOnlyTestEVMParams = chainparams.EVM{
	MinFeePerGas:             big.NewInt(evmOnlyMinGasPrice),
	TargetGasUsedPerSecond:   30_000,
	BaseFeeChangeDenominator: 144,
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

// raisedBaseFee returns the base fee three transfers set over a test block interval, from
// scheduled, under the genesis rules.
func raisedBaseFee(scheduled uint256.Int) uint256.Int {
	return genesisBaseFeeParams().NextBaseFee(scheduled, 63_000, evmOnlyTestBlockInterval)
}

// The gas a block uses sets the base fee two blocks later, so the first two blocks run at the
// floor.
func TestEVMOnlyApplicationSetsTheBaseFeeTwoBlocksAhead(t *testing.T) {
	setEVMOnlyGenesisEVMParams(t, evmOnlyTestEVMParams)
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	floor := genesisBaseFeeParams().Floor
	raised := raisedBaseFee(floor)
	require.Greater(t, raised.Uint64(), floor.Uint64())

	finalizeAndCommitEVMOnlyTestBlock(t, app, threeTransfersBlock(t, 1))
	require.Equal(t, floor.ToBig(), app.EvmBaseFee())
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))
	require.Equal(t, floor.ToBig(), app.EvmBaseFee())
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
	require.Equal(t, raised.ToBig(), app.EvmBaseFee())
}

// The same gas over a block twice as long is a smaller share of its target, so it raises the base
// fee less.
func TestEVMOnlyApplicationTargetsGasPerSecond(t *testing.T) {
	setEVMOnlyGenesisEVMParams(t, evmOnlyTestEVMParams)
	floor := genesisBaseFeeParams().Floor
	for name, elapsed := range map[string]uint64{"one second": evmOnlyTestBlockInterval, "two seconds": 2 * evmOnlyTestBlockInterval} {
		t.Run(name, func(t *testing.T) {
			app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
			first := threeTransfersBlock(t, 1)
			first.Header.Time = time.Unix(0, int64(1_700_000_000*evmOnlyTestBlockInterval+elapsed)) //nolint:gosec // G115: test times fit int64.
			finalizeAndCommitEVMOnlyTestBlock(t, app, first)
			finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))
			finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))

			want := genesisBaseFeeParams().NextBaseFee(floor, 63_000, elapsed)
			require.Equal(t, want.ToBig(), app.EvmBaseFee())
		})
	}
	oneSecond := genesisBaseFeeParams().NextBaseFee(floor, 63_000, evmOnlyTestBlockInterval)
	twoSeconds := genesisBaseFeeParams().NextBaseFee(floor, 63_000, 2*evmOnlyTestBlockInterval)
	require.Greater(t, oneSecond.Uint64(), twoSeconds.Uint64())
	require.Greater(t, twoSeconds.Uint64(), floor.Uint64())
}

// CheckTx prices against the highest base fee already set, so a transaction it admits can execute
// at every height whose base fee is known.
func TestEVMOnlyApplicationAdmitsAtTheHighestScheduledBaseFee(t *testing.T) {
	setEVMOnlyGenesisEVMParams(t, evmOnlyTestEVMParams)
	app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
	raised := raisedBaseFee(genesisBaseFeeParams().Floor)

	finalizeAndCommitEVMOnlyTestBlock(t, app, threeTransfersBlock(t, 1))

	// Block 2 still runs at the floor, but block 3 runs at raised.
	require.Equal(t, raised.ToBig(), app.EvmMinGasPrice())
	atFloor := signedEVMOnlyTestTxAtPrice(t, big.NewInt(evmOnlyMinGasPrice))
	require.False(t, app.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: atFloor}).IsOK())
	atRaised := signedEVMOnlyTestTxAtPrice(t, raised.ToBig())
	require.True(t, app.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: atRaised}).IsOK())
}

// A transaction priced below its block's base fee is refused by CheckTx and, included anyway, is
// recorded as a failure that used no gas rather than failing the block.
func TestEVMOnlyApplicationRecordsATxBelowTheBaseFeeAsFailed(t *testing.T) {
	setEVMOnlyGenesisEVMParams(t, evmOnlyTestEVMParams)
	app := newInitializedEVMOnlyTestApp(t)
	raw := signedEVMOnlyTestTxAtPrice(t, big.NewInt(evmOnlyMinGasPrice-1))
	require.False(t, app.CheckTx(t.Context(), &proxy.RequestCheckTxV2{Tx: raw}).IsOK())

	response, err := app.FinalizeBlock(t.Context(), evmOnlyTestBlock(1, raw))

	require.NoError(t, err)
	require.Len(t, response.TxResults, 1)
	require.Equal(t, int64(0), response.TxResults[0].GasUsed)
	require.Contains(t, response.TxResults[0].Log, ethcore.ErrFeeCapTooLow.Error())
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
}

// The base fees a block set, and its time, survive a restart, so the resumed node runs the next
// blocks at those fees and measures the next block's duration from that time.
func TestEVMOnlyApplicationResumesItsBaseFeesAfterRestart(t *testing.T) {
	setEVMOnlyGenesisEVMParams(t, evmOnlyTestEVMParams)
	raised := raisedBaseFee(genesisBaseFeeParams().Floor)
	home := t.TempDir()
	storage := openEVMOnlyTestStorageAt(t, home)
	app, err := newEVMOnlyTestAppOnChain(storage, evmOnlyTestChainID)
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
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
	floor := genesisBaseFeeParams().Floor
	inState := raisedBaseFee(floor)

	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))
	out, err := precompileCall(t, app, chainparams.Address, chainparams.ABI, chainparams.ParamMethod, "evm", "KeyTargetGasUsedPerSecond")
	require.NoError(t, err)
	require.Equal(t, common.BigToHash(new(big.Int).SetUint64(evmOnlyTestEVMParams.TargetGasUsedPerSecond)).Bytes(), out[0].([]byte))

	// Under a compiled-in target of 2 giga-gas per second, three transfers would leave the base fee
	// at the floor.
	evmOnlyGenesisEVMParams.TargetGasUsedPerSecond = 2_000_000_000
	require.Equal(t, floor, genesisBaseFeeParams().NextBaseFee(floor, 63_000, evmOnlyTestBlockInterval))
	finalizeAndCommitEVMOnlyTestBlock(t, app, threeTransfersBlock(t, 2))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(3))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(4))

	require.Equal(t, inState.ToBig(), app.EvmBaseFee())
}

// The genesis base fee params: no floor, a 2 giga-gas per second target and D = 144.
func TestEVMOnlyGenesisEVMParams(t *testing.T) {
	require.Equal(t, chainparams.EVM{
		MinFeePerGas:             new(big.Int),
		TargetGasUsedPerSecond:   2_000_000_000,
		BaseFeeChangeDenominator: 144,
	}, evmOnlyGenesisEVMParams)
	require.NoError(t, evmOnlyGenesisEVMParams.Validate())
	require.Equal(t, 2, evmOnlyBaseFeeLag)
}
