package types_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var (
	gasTestSender  = common.HexToAddress("0x1000")
	gasTestFactory = common.HexToAddress("0x2000")
	gasTestPayer   = common.HexToAddress("0x3000")
	gasTestStorer  = common.HexToAddress("0x4000")
	gasTestFresh   = common.HexToAddress("0xdead")
)

// Create, new-account and new-slot pricing (EIP-8037, 8038, 2780) is Amsterdam only; Sei's subset
// must charge what Osaka charges.
func TestSeiAmsterdamKeepsOsakaCreateAndAccountGas(t *testing.T) {
	seiCfg := types.DefaultChainConfig().EthereumConfig(big.NewInt(1329))
	require.True(t, seiCfg.Rules(common.Big0, true, 0).IsSeiAmsterdam)
	osakaCfg := *seiCfg
	osakaCfg.SeiAmsterdamTime = nil
	amsterdamCfg := osakaCfg
	amsterdamCfg.AmsterdamTime = new(uint64)

	for _, tc := range []struct {
		name string
		to   *common.Address
		val  int64
		data []byte
	}{
		{name: "value transfer to a new account", to: &gasTestFresh, val: 1},
		// PUSH1 32 PUSH1 0 RETURN: deploys 32 bytes.
		{name: "contract creation tx", data: common.FromHex("0x60206000f3")},
		{name: "CREATE opcode", to: &gasTestFactory},
		{name: "CALL with value to a new account", to: &gasTestPayer},
		{name: "SSTORE to a new slot", to: &gasTestStorer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seiGas := gasUsed(t, seiCfg, tc.to, tc.val, tc.data)
			require.Equal(t, gasUsed(t, &osakaCfg, tc.to, tc.val, tc.data), seiGas)
			require.NotEqual(t, gasUsed(t, &amsterdamCfg, tc.to, tc.val, tc.data), seiGas, "case no longer exercises Amsterdam repricing")
		})
	}
	require.Equal(t, params.TxGas, gasUsed(t, seiCfg, &gasTestFresh, 1, nil))
}

func gasUsed(t *testing.T, cfg *params.ChainConfig, to *common.Address, val int64, data []byte) uint64 {
	t.Helper()
	statedb, err := state.New(ethtypes.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	statedb.SetBalance(gasTestSender, uint256.NewInt(1e18), 0)
	// PUSH1 0 PUSH1 0 PUSH1 0 CREATE STOP
	statedb.SetCode(gasTestFactory, common.FromHex("0x600060006000f000"), 0)
	// CALL(gas, 0xdead, 1, 0, 0, 0, 0) STOP
	statedb.SetCode(gasTestPayer, common.FromHex("0x6000600060006000600161dead5af100"), 0)
	statedb.SetBalance(gasTestPayer, uint256.NewInt(1), 0)
	// PUSH1 1 PUSH1 0 SSTORE STOP
	statedb.SetCode(gasTestStorer, common.FromHex("0x600160005500"), 0)

	blockCtx := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		BlockNumber: big.NewInt(1),
		GasLimit:    30_000_000,
		BaseFee:     common.Big0,
		Random:      &common.Hash{},
		// Sei leaves this at zero; set so Amsterdam's state gas shows up.
		CostPerStateByte: params.CostPerStateByte,
	}
	evm := vm.NewEVM(blockCtx, statedb, cfg, vm.Config{})
	zero := new(uint256.Int)
	msg := &core.Message{
		From:      gasTestSender,
		To:        to,
		Value:     uint256.NewInt(uint64(val)), //nolint:gosec
		GasLimit:  1_000_000,
		GasPrice:  zero,
		GasFeeCap: zero,
		GasTipCap: zero,
		Data:      data,
	}
	res, err := core.ApplyMessage(evm, msg, core.NewGasPool(blockCtx.GasLimit))
	require.NoError(t, err)
	require.NoError(t, res.Err)
	return res.UsedGas
}
