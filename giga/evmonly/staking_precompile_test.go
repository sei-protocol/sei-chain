package evmonly

// Pins the staking precompile running through the executor on real transactions.

import (
	"crypto/ecdsa"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

var (
	stakingVal1 = common.HexToAddress("0x0000000000000000000000000000000000000a01")
	stakingVal2 = common.HexToAddress("0x0000000000000000000000000000000000000a02")
)

// seiWei returns n SEI in wei.
func seiWei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000_000_000))
}

// stakingGenesis returns a state with staking's params, unbonding over 100
// seconds, and two genesis validators self-delegating 30 and 20 SEI.
func stakingGenesis(t *testing.T) *MemoryState {
	state := NewMemoryState()
	params := staking.DefaultParams()
	params.UnbondingTime = uint64(100 * time.Second)
	params.MaxVotingPowerRatio = big.NewInt(1_000_000_000_000_000_000)
	require.NoError(t, staking.SetParams(genesisState{state}, params))
	for i, op := range []common.Address{stakingVal1, stakingVal2} {
		self := seiWei(int64(30 - 10*i))
		state.SetBalance(op, self)
		require.NoError(t, staking.Precompile{}.AddGenesisValidator(precompiles.BlockContext{}, genesisState{state}, staking.GenesisValidator{
			Operator:          op,
			ConsensusPubkey:   common.BytesToHash(op[:]),
			SelfDelegation:    self,
			MinSelfDelegation: seiWei(1),
		}))
	}
	return state
}

func stakingRegistry() precompiles.Registry {
	return precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{staking.Address: staking.Precompile{}}, staking.Address)
}

func TestStakingPrecompileThroughExecutor(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	state := stakingGenesis(t)
	state.SetBalance(sender, seiWei(100))

	delegate, err := staking.ABI.Pack(staking.DelegateMethod, stakingVal2)
	require.NoError(t, err)
	undelegate, err := staking.ABI.Pack(staking.UndelegateMethod, stakingVal2, seiWei(4))
	require.NoError(t, err)
	redelegate, err := staking.ABI.Pack(staking.RedelegateMethod, stakingVal2, stakingVal1, seiWei(1))
	require.NoError(t, err)
	to := staking.Address
	txs := [][]byte{
		signLegacyTxWithGasPrice(t, key, chainID, 0, &to, seiWei(10), delegate, 2_000_000, big.NewInt(0)),
		signLegacyTxWithGasPrice(t, key, chainID, 1, &to, big.NewInt(0), undelegate, 2_000_000, big.NewInt(0)),
		signLegacyTxWithGasPrice(t, key, chainID, 2, &to, big.NewInt(0), redelegate, 2_000_000, big.NewInt(0)),
	}
	cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: stakingRegistry()}
	block := blockContext(chainID)
	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: block, Txs: txs})
	require.NoError(t, err)
	for i, tx := range result.Txs {
		require.NoError(t, tx.Err, "tx %d", i)
		require.Equal(t, uint64(1), tx.Status, "tx %d", i)
		require.Len(t, result.Receipts[i].Logs, 1, "tx %d logs its event", i)
	}
	state.ApplyChangeSet(result.ChangeSet)

	require.Equal(t, seiWei(90), state.GetBalance(sender))
	require.Zero(t, state.GetBalance(staking.Address).Sign())
	require.Equal(t, seiWei(56), state.GetBalance(staking.BondedEscrow), "the first EndBlock bonds both validators")
	require.Equal(t, seiWei(4), state.GetBalance(staking.NotBondedEscrow), "the unbonding entry waits")

	block.Number++
	block.Time += 100
	result, err = NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: block})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, seiWei(94), state.GetBalance(sender), "the unbonding entry is paid")
	require.Zero(t, state.GetBalance(staking.NotBondedEscrow).Sign())
}

func TestStakingPrecompileOCCMatchesSequential(t *testing.T) {
	chainID := big.NewInt(testChainID)
	keys := make([]*ecdsa.PrivateKey, len(anvilKeys))
	nonces := make([]uint64, len(anvilKeys))
	for i, hex := range anvilKeys {
		key, err := crypto.HexToECDSA(hex)
		require.NoError(t, err)
		keys[i] = key
	}
	to := staking.Address
	var txs [][]byte
	send := func(i int, value *big.Int, method string, args ...interface{}) {
		data, err := staking.ABI.Pack(method, args...)
		require.NoError(t, err)
		txs = append(txs, signLegacyTxWithGasPrice(t, keys[i], chainID, nonces[i], &to, value, data, 2_000_000, big.NewInt(0)))
		nonces[i]++
	}
	for i := range keys {
		send(i, seiWei(int64(5+i)), staking.DelegateMethod, []common.Address{stakingVal1, stakingVal2}[i%2])
	}
	send(0, nil, staking.UndelegateMethod, stakingVal1, seiWei(2))
	send(1, nil, staking.RedelegateMethod, stakingVal2, stakingVal1, seiWei(3))
	send(2, nil, staking.RedelegateMethod, stakingVal1, stakingVal2, seiWei(7))
	send(3, nil, staking.UndelegateMethod, stakingVal2, seiWei(8))
	send(4, seiWei(1), staking.DelegateMethod, stakingVal2)

	run := func(workers int) *BlockResult {
		state := stakingGenesis(t)
		for _, key := range keys {
			state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), seiWei(100))
		}
		result, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: workers, CustomPrecompiles: stakingRegistry()}, withTestState(state)).
			ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: txs})
		require.NoError(t, err)
		return result
	}
	sequential, occ := run(1), run(4)
	require.True(t, occ.OCCStats.Attempted)
	require.False(t, occ.OCCStats.Fallback, occ.OCCStats.FallbackReason)
	require.Positive(t, occ.OCCStats.RerunCount, "the block's staking txs conflict")
	for i, tx := range sequential.Txs {
		require.Equal(t, uint64(1), tx.Status, "tx %d", i)
	}
	require.Equal(t, sequential.GasUsed, occ.GasUsed)
	require.Equal(t, sequential.Txs, occ.Txs)
	require.Equal(t, sequential.Receipts, occ.Receipts)
	require.Equal(t, sequential.ChangeSet, occ.ChangeSet)
}
