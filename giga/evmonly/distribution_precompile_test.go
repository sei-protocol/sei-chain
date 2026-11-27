package evmonly

// Pins staking rewards running through the executor on real transactions:
// fees collected, epochs settled and rewards withdrawn.

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/distribution"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var rewardsDAO = common.HexToAddress("0x00000000000000000000000000000000000da0da")

// rewardsRegistry runs distribution, then staking with distribution's hooks,
// at the end of each block.
func rewardsRegistry() precompiles.Registry {
	return precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{
		staking.Address:      staking.Precompile{Hooks: distribution.Hooks{}},
		distribution.Address: distribution.Precompile{},
	}, distribution.Address, staking.Address)
}

// rewardsGenesis returns stakingGenesis's state with distribution's hooks
// tracking the genesis validators, distribution's params, and a schedule
// releasing 1,000 SEI over the first 1,000 seconds.
func rewardsGenesis(t *testing.T) *MemoryState {
	state := NewMemoryState()
	params := staking.DefaultParams()
	params.UnbondingTime = uint64(100 * time.Second)
	params.MaxVotingPowerRatio = big.NewInt(1_000_000_000_000_000_000)
	require.NoError(t, staking.SetParams(genesisState{state}, params))
	require.NoError(t, chainparams.WriteDistribution(genesisState{state}, chainparams.Distribution{WithdrawAddrEnabled: true, DAOAddress: rewardsDAO}))
	require.NoError(t, distribution.WriteReleaseSchedule(genesisState{state}, []distribution.ReleasePeriod{
		{StartTime: 0, EndTime: uint64(1000 * time.Second), Amount: seiWei(1000)},
	}, 0))
	for i, op := range []common.Address{stakingVal1, stakingVal2} {
		self := seiWei(int64(30 - 10*i))
		state.SetBalance(op, self)
		require.NoError(t, staking.Precompile{Hooks: distribution.Hooks{}}.AddGenesisValidator(precompiles.BlockContext{}, genesisState{state}, staking.GenesisValidator{
			Operator:          op,
			ConsensusPubkey:   common.BytesToHash(op[:]),
			SelfDelegation:    self,
			MinSelfDelegation: seiWei(1),
		}))
	}
	genesis := blockContext(big.NewInt(testChainID))
	genesis.Number, genesis.Time = 0, 0
	result, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: rewardsRegistry()}, withTestState(state)).
		ExecuteBlock(t.Context(), BlockRequest{Context: genesis})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	return state
}

// executeRewardsBlock runs txs in block on state, requiring each to succeed,
// applies the result, and moves block on to the next, ten seconds later.
func executeRewardsBlock(t *testing.T, state *MemoryState, block *BlockContext, closing utils.Option[uint64], txs ...[]byte) *BlockResult {
	t.Helper()
	block.ClosingEpoch = closing
	cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: rewardsRegistry()}
	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: *block, Txs: txs})
	require.NoError(t, err)
	for i, tx := range result.Txs {
		require.NoError(t, tx.Err, "tx %d", i)
		require.Equal(t, uint64(1), tx.Status, "tx %d", i)
	}
	state.ApplyChangeSet(result.ChangeSet)
	block.Number++
	block.Time += 10
	return result
}

func TestStakingRewardsThroughExecutor(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	state := rewardsGenesis(t)
	state.SetBalance(sender, seiWei(100))

	block := blockContext(chainID)
	block.Coinbase = distribution.FeeCollector
	block.BaseFee = big.NewInt(1_000_000_000)
	gasPrice := big.NewInt(3_000_000_000)
	stakingTo, distributionTo, other := staking.Address, distribution.Address, common.HexToAddress("0x0123")
	delegate, err := staking.ABI.Pack(staking.DelegateMethod, stakingVal2)
	require.NoError(t, err)
	withdraw, err := distribution.ABI.Pack(distribution.WithdrawDelegationRewardsMethod, stakingVal2)
	require.NoError(t, err)

	// Epoch 0 stages the delegation, and epoch 1's end applies it.
	executeRewardsBlock(t, state, &block, utils.Some[uint64](0),
		signLegacyTxWithGasPrice(t, key, chainID, 0, &stakingTo, seiWei(10), delegate, 2_000_000, gasPrice))
	executeRewardsBlock(t, state, &block, utils.Some[uint64](1))
	require.Zero(t, state.GetBalance(distribution.FeeCollector).Sign(), "each settlement empties the fee collector")

	// Epoch 2 collects a transfer's fees and settles them.
	daoBefore := state.GetBalance(rewardsDAO)
	escrowBefore := state.GetBalance(distribution.DistributionEscrow)
	result := executeRewardsBlock(t, state, &block, utils.Some[uint64](2),
		signLegacyTxWithGasPrice(t, key, chainID, 1, &other, big.NewInt(1), nil, 21_000, gasPrice))
	baseFees := new(big.Int).Mul(new(big.Int).SetUint64(result.GasUsed), block.BaseFee)
	tips := new(big.Int).Mul(new(big.Int).SetUint64(result.GasUsed), big.NewInt(2_000_000_000))
	require.Zero(t, state.GetBalance(distribution.FeeCollector).Sign())
	daoGot := new(big.Int).Sub(state.GetBalance(rewardsDAO), daoBefore)
	escrowGot := new(big.Int).Sub(state.GetBalance(distribution.DistributionEscrow), escrowBefore)
	minted := seiWei(10)
	require.Equal(t, new(big.Int).Add(new(big.Int).Add(baseFees, tips), minted), new(big.Int).Add(daoGot, escrowGot),
		"the base fees, the tips and ten seconds of the mint, all paid out")
	require.GreaterOrEqual(t, daoGot.Cmp(new(big.Int).Add(baseFees, new(big.Int).Rsh(tips, 1))), 0, "the base fees and half the tips go to the DAO")

	// The delegator withdraws what the settlements owe it.
	before := state.GetBalance(sender)
	result = executeRewardsBlock(t, state, &block, utils.None[uint64](),
		signLegacyTxWithGasPrice(t, key, chainID, 2, &distributionTo, big.NewInt(0), withdraw, 2_000_000, gasPrice))
	require.Len(t, result.Receipts[0].Logs, 1, "the withdrawal logs its event")
	paid := new(big.Int).SetBytes(result.Receipts[0].Logs[0].Data)
	require.Positive(t, paid.Sign())
	fee := new(big.Int).Mul(new(big.Int).SetUint64(result.GasUsed), gasPrice)
	require.Equal(t, new(big.Int).Sub(new(big.Int).Add(before, paid), fee), state.GetBalance(sender))
}
