package evmonly

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

type gethReferenceResult struct {
	state    *gethstate.StateDB
	txs      []TxResult
	receipts ethtypes.Receipts
	gasUsed  uint64
}

func TestExecutorNativeTransferFeeAccountingMatchesGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xd1)
	initialBalance := big.NewInt(1_000_000_000)
	value := big.NewInt(1234)
	baseFee := big.NewInt(7)
	tipCap := big.NewInt(3)
	feeCap := big.NewInt(20)

	state := NewMemoryState()
	state.SetBalance(sender, initialBalance)

	rawTx := signDynamicFeeTxWithFees(t, key, chainID, 0, &recipient, value, nil, tipCap, feeCap, 100_000)
	ctx := blockContext(chainID)
	ctx.BaseFee = baseFee
	ctx.Coinbase = testAddress(0xd2)
	cfg := Config{MinGasPrice: big.NewInt(0)}

	gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
	require.NoError(t, err)
	execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})
	require.NoError(t, err)
	state.ApplyChangeSet(execResult.ChangeSet)

	requireExecutionParity(t, execResult, gethResult)
	requireAddressParity(t, state, gethResult.state, sender)
	requireAddressParity(t, state, gethResult.state, recipient)
	requireAddressParity(t, state, gethResult.state, ctx.Coinbase)

	gasUsed := execResult.Txs[0].GasUsed
	effectivePrice := new(big.Int).Add(baseFee, tipCap)
	require.Equal(t, effectivePrice, execResult.Txs[0].EffectiveGasPrice)
	totalFee := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), effectivePrice)
	expectedSender := new(big.Int).Sub(new(big.Int).Sub(new(big.Int).Set(initialBalance), value), totalFee)
	require.Equal(t, expectedSender, state.GetBalance(sender))
	require.Equal(t, value, state.GetBalance(recipient))
	require.Equal(t, totalFee, state.GetBalance(ctx.Coinbase))
}

func TestExecutorNativeTransferEdgeCasesMatchGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	initialBalance := big.NewInt(1_000_000_000_000)

	tests := []struct {
		name          string
		value         *big.Int
		configureCtx  func(*BlockContext, common.Address)
		sign          func(t *testing.T, key *ecdsa.PrivateKey, ctx BlockContext, recipient common.Address) []byte
		wantEffective *big.Int
		assert        func(t *testing.T, state *MemoryState, sender, recipient, coinbase common.Address, gasUsed uint64)
	}{
		{
			name:  "zero value transfer touches nonce and fees only",
			value: big.NewInt(0),
			sign: func(t *testing.T, key *ecdsa.PrivateKey, _ BlockContext, recipient common.Address) []byte {
				t.Helper()
				return signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(0), nil, 100_000, big.NewInt(1))
			},
			wantEffective: big.NewInt(1),
			assert: func(t *testing.T, state *MemoryState, sender, recipient, coinbase common.Address, gasUsed uint64) {
				t.Helper()
				fee := new(big.Int).SetUint64(gasUsed)
				require.Equal(t, new(big.Int).Sub(initialBalance, fee), state.GetBalance(sender))
				require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
				require.Equal(t, fee, state.GetBalance(coinbase))
			},
		},
		{
			name:  "dynamic fee effective price is capped by fee cap",
			value: big.NewInt(17),
			configureCtx: func(ctx *BlockContext, _ common.Address) {
				ctx.BaseFee = big.NewInt(7)
			},
			sign: func(t *testing.T, key *ecdsa.PrivateKey, _ BlockContext, recipient common.Address) []byte {
				t.Helper()
				return signDynamicFeeTxWithFees(t, key, chainID, 0, &recipient, big.NewInt(17), nil, big.NewInt(10), big.NewInt(12), 100_000)
			},
			wantEffective: big.NewInt(12),
			assert: func(t *testing.T, state *MemoryState, sender, recipient, coinbase common.Address, gasUsed uint64) {
				t.Helper()
				fee := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), big.NewInt(12))
				require.Equal(t, new(big.Int).Sub(new(big.Int).Sub(initialBalance, big.NewInt(17)), fee), state.GetBalance(sender))
				require.Equal(t, big.NewInt(17), state.GetBalance(recipient))
				require.Equal(t, fee, state.GetBalance(coinbase))
			},
		},
		{
			name:  "coinbase sender receives its own fee reward",
			value: big.NewInt(23),
			configureCtx: func(ctx *BlockContext, sender common.Address) {
				ctx.Coinbase = sender
			},
			sign: func(t *testing.T, key *ecdsa.PrivateKey, _ BlockContext, recipient common.Address) []byte {
				t.Helper()
				return signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(23), nil, 100_000, big.NewInt(5))
			},
			wantEffective: big.NewInt(5),
			assert: func(t *testing.T, state *MemoryState, sender, recipient, _ common.Address, _ uint64) {
				t.Helper()
				require.Equal(t, new(big.Int).Sub(initialBalance, big.NewInt(23)), state.GetBalance(sender))
				require.Equal(t, big.NewInt(23), state.GetBalance(recipient))
			},
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			recipient := common.BigToAddress(big.NewInt(int64(0xe0 + i)))
			state := NewMemoryState()
			state.SetBalance(sender, initialBalance)
			ctx := blockContext(chainID)
			if tc.configureCtx != nil {
				tc.configureCtx(&ctx, sender)
			}
			rawTx := tc.sign(t, key, ctx, recipient)
			cfg := Config{MinGasPrice: big.NewInt(0)}

			gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
			require.NoError(t, err)
			execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
				Context: ctx,
				Txs:     [][]byte{rawTx},
			})
			require.NoError(t, err)
			state.ApplyChangeSet(execResult.ChangeSet)

			requireExecutionParity(t, execResult, gethResult)
			requireAddressParity(t, state, gethResult.state, sender)
			requireAddressParity(t, state, gethResult.state, recipient)
			requireAddressParity(t, state, gethResult.state, ctx.Coinbase)
			require.Equal(t, tc.wantEffective, execResult.Txs[0].EffectiveGasPrice)
			require.Equal(t, uint64(1), state.GetNonce(sender))
			tc.assert(t, state, sender, recipient, ctx.Coinbase, execResult.Txs[0].GasUsed)
		})
	}
}

func TestExecutorERC20StyleTransferMatchesGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	token := testAddress(0xd3)
	from := testAddress(0xd4)
	to := testAddress(0xd5)
	fromSlot := common.BytesToHash(from.Bytes())
	toSlot := common.BytesToHash(to.Bytes())
	amount := byte(7)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(1_000_000_000_000))
	state.SetCode(token, erc20TransferRuntime(fromSlot, toSlot, from, to, amount))
	state.SetState(token, fromSlot, common.BigToHash(big.NewInt(1000)))

	rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &token, big.NewInt(0), nil, 200_000, big.NewInt(1))
	cfg := Config{MinGasPrice: big.NewInt(0)}
	ctx := blockContext(chainID)

	gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
	require.NoError(t, err)
	execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})
	require.NoError(t, err)
	state.ApplyChangeSet(execResult.ChangeSet)

	requireExecutionParity(t, execResult, gethResult)
	requireAddressParity(t, state, gethResult.state, token, fromSlot, toSlot)
	require.Equal(t, common.BigToHash(big.NewInt(993)), state.GetState(token, fromSlot))
	require.Equal(t, common.BigToHash(big.NewInt(7)), state.GetState(token, toSlot))

	require.Len(t, execResult.Receipts[0].Logs, 1)
	log := execResult.Receipts[0].Logs[0]
	require.Equal(t, token, log.Address)
	require.Equal(t, []common.Hash{
		erc20TransferTopic(),
		common.BytesToHash(from.Bytes()),
		common.BytesToHash(to.Bytes()),
	}, log.Topics)
	require.Equal(t, common.LeftPadBytes([]byte{amount}, common.HashLength), log.Data)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, execResult.Receipts[0].Status)
}

func TestExecutorSelfDestructCreatedInSameTxMatchesGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	factory := testAddress(0xd6)
	beneficiary := testAddress(0xd7)
	child := crypto.CreateAddress(factory, 0)
	endowment := byte(17)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(1_000_000_000_000))
	state.SetCode(factory, createAndDestroyChildRuntime(beneficiary, endowment))

	rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &factory, big.NewInt(int64(endowment)), nil, 500_000, big.NewInt(1))
	cfg := Config{MinGasPrice: big.NewInt(0)}
	ctx := blockContext(chainID)

	gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
	require.NoError(t, err)
	execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})
	require.NoError(t, err)
	state.ApplyChangeSet(execResult.ChangeSet)

	requireExecutionParity(t, execResult, gethResult)
	requireAddressParity(t, state, gethResult.state, factory)
	requireAddressParity(t, state, gethResult.state, child)
	requireAddressParity(t, state, gethResult.state, beneficiary)
	require.Empty(t, state.GetCode(child))
	require.Equal(t, uint64(0), state.GetNonce(child))
	require.Equal(t, big.NewInt(0), state.GetBalance(child))
	require.Equal(t, big.NewInt(int64(endowment)), state.GetBalance(beneficiary))
}

func TestExecutorPragueSelfDestructEdgeCasesMatchGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	slot := testHash(0x47)
	value := testHash(0x48)
	contractBalance := big.NewInt(99)

	tests := []struct {
		name                  string
		beneficiary           func(common.Address) common.Address
		wantContractBalance   *big.Int
		wantBeneficiaryCredit *big.Int
	}{
		{
			name: "preexisting contract sends balance but keeps code and storage",
			beneficiary: func(common.Address) common.Address {
				return testAddress(0xdb)
			},
			wantContractBalance:   big.NewInt(0),
			wantBeneficiaryCredit: contractBalance,
		},
		{
			name: "preexisting contract self beneficiary keeps balance code and storage",
			beneficiary: func(contract common.Address) common.Address {
				return contract
			},
			wantContractBalance:   contractBalance,
			wantBeneficiaryCredit: contractBalance,
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			contract := common.BigToAddress(big.NewInt(int64(0xf0 + i)))
			beneficiary := tc.beneficiary(contract)
			runtime := selfDestructCode(beneficiary)

			state := NewMemoryState()
			state.SetBalance(sender, big.NewInt(1_000_000_000_000))
			state.SetBalance(contract, contractBalance)
			state.SetNonce(contract, 1)
			state.SetCode(contract, runtime)
			state.SetState(contract, slot, value)

			rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &contract, big.NewInt(0), nil, 100_000, big.NewInt(1))
			cfg := Config{MinGasPrice: big.NewInt(0)}
			ctx := blockContext(chainID)

			gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
			require.NoError(t, err)
			execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
				Context: ctx,
				Txs:     [][]byte{rawTx},
			})
			require.NoError(t, err)
			state.ApplyChangeSet(execResult.ChangeSet)

			requireExecutionParity(t, execResult, gethResult)
			requireAddressParity(t, state, gethResult.state, sender)
			requireAddressParity(t, state, gethResult.state, contract, slot)
			requireAddressParity(t, state, gethResult.state, beneficiary)
			require.Equal(t, runtime, state.GetCode(contract))
			require.Equal(t, uint64(1), state.GetNonce(contract))
			require.Equal(t, value, state.GetState(contract, slot))
			require.Equal(t, tc.wantContractBalance, state.GetBalance(contract))
			if beneficiary != contract {
				require.Equal(t, tc.wantBeneficiaryCredit, state.GetBalance(beneficiary))
			}
		})
	}
}

func TestExecutorAccessListGasAccountingMatchesGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	contract := testAddress(0xd8)
	slot := testHash(0x44)
	cfg := Config{MinGasPrice: big.NewInt(0)}
	ctx := blockContext(chainID)

	run := func(t *testing.T, accessList ethtypes.AccessList) (uint64, *BlockResult) {
		t.Helper()
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(1_000_000_000_000))
		state.SetCode(contract, sloadRuntime(slot))
		state.SetState(contract, slot, testHash(0x45))
		rawTx := signAccessListTx(t, key, chainID, 0, &contract, big.NewInt(0), nil, 120_000, big.NewInt(1), accessList)

		gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
		require.NoError(t, err)
		execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
			Context: ctx,
			Txs:     [][]byte{rawTx},
		})
		require.NoError(t, err)
		requireExecutionParity(t, execResult, gethResult)
		return execResult.Txs[0].GasUsed, execResult
	}

	coldGas, _ := run(t, nil)
	tests := []struct {
		name          string
		accessList    ethtypes.AccessList
		expectedDelta uint64
	}{
		{
			name: "contract slot warmed",
			accessList: ethtypes.AccessList{{
				Address:     contract,
				StorageKeys: []common.Hash{slot},
			}},
			expectedDelta: params.TxAccessListAddressGas +
				params.TxAccessListStorageKeyGas -
				(params.ColdSloadCostEIP2929 - params.WarmStorageReadCostEIP2929),
		},
		{
			name: "address only still pays intrinsic cost because destination is already warm",
			accessList: ethtypes.AccessList{{
				Address: contract,
			}},
			expectedDelta: params.TxAccessListAddressGas,
		},
		{
			name: "duplicate storage keys are charged twice but warm once",
			accessList: ethtypes.AccessList{{
				Address:     contract,
				StorageKeys: []common.Hash{slot, slot},
			}},
			expectedDelta: params.TxAccessListAddressGas +
				2*params.TxAccessListStorageKeyGas -
				(params.ColdSloadCostEIP2929 - params.WarmStorageReadCostEIP2929),
		},
		{
			name: "unrelated entry only adds intrinsic access list gas",
			accessList: ethtypes.AccessList{{
				Address:     testAddress(0xdc),
				StorageKeys: []common.Hash{testHash(0x49)},
			}},
			expectedDelta: params.TxAccessListAddressGas + params.TxAccessListStorageKeyGas,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gasUsed, execResult := run(t, tc.accessList)

			require.Equal(t, coldGas+tc.expectedDelta, gasUsed)
			require.Equal(t, uint8(ethtypes.AccessListTxType), execResult.Receipts[0].Type)
		})
	}
}

func TestExecutorLogOpcodeCorpusMatchesGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	data := testHash(0x60)
	cfg := Config{MinGasPrice: big.NewInt(0)}
	ctx := blockContext(chainID)

	for topicCount := 0; topicCount <= 4; topicCount++ {
		t.Run(fmt.Sprintf("LOG%d", topicCount), func(t *testing.T) {
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			contract := common.BigToAddress(big.NewInt(int64(0x100 + topicCount)))
			topics := make([]common.Hash, topicCount)
			for i := range topics {
				topics[i] = common.BigToHash(big.NewInt(int64(0x700 + i)))
			}
			state := NewMemoryState()
			state.SetBalance(sender, big.NewInt(1_000_000_000_000))
			state.SetCode(contract, logRuntime(data, topics))
			rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &contract, big.NewInt(0), nil, 200_000, big.NewInt(1))

			gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
			require.NoError(t, err)
			execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
				Context: ctx,
				Txs:     [][]byte{rawTx},
			})
			require.NoError(t, err)
			state.ApplyChangeSet(execResult.ChangeSet)

			requireExecutionParity(t, execResult, gethResult)
			requireAddressParity(t, state, gethResult.state, sender)
			requireAddressParity(t, state, gethResult.state, contract)
			require.Len(t, execResult.Receipts[0].Logs, 1)
			log := execResult.Receipts[0].Logs[0]
			require.Equal(t, contract, log.Address)
			require.Equal(t, topics, log.Topics)
			require.Equal(t, data.Bytes(), log.Data)
			require.Equal(t, ethtypes.CreateBloom(execResult.Receipts[0]), execResult.Receipts[0].Bloom)
		})
	}
}

func TestExecutorCallOpcodeCorpusMatchesGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	targetSlot := testHash(0x61)
	successSlot := testHash(0x62)
	targetValue := testHash(0x63)
	cfg := Config{MinGasPrice: big.NewInt(0)}
	ctx := blockContext(chainID)

	tests := []struct {
		name                string
		op                  byte
		wantSuccess         common.Hash
		wantCallerTarget    common.Hash
		wantTargetTarget    common.Hash
		wantTargetUnchanged bool
	}{
		{
			name:             "CALL writes callee storage",
			op:               0xf1,
			wantSuccess:      common.BigToHash(big.NewInt(1)),
			wantTargetTarget: targetValue,
		},
		{
			name:             "CALLCODE writes caller storage",
			op:               0xf2,
			wantSuccess:      common.BigToHash(big.NewInt(1)),
			wantCallerTarget: targetValue,
		},
		{
			name:             "DELEGATECALL writes caller storage",
			op:               0xf4,
			wantSuccess:      common.BigToHash(big.NewInt(1)),
			wantCallerTarget: targetValue,
		},
		{
			name:                "STATICCALL reports failure and blocks callee storage writes",
			op:                  0xfa,
			wantSuccess:         common.Hash{},
			wantTargetUnchanged: true,
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			caller := common.BigToAddress(big.NewInt(int64(0x120 + i)))
			target := common.BigToAddress(big.NewInt(int64(0x130 + i)))
			state := NewMemoryState()
			state.SetBalance(sender, big.NewInt(1_000_000_000_000))
			state.SetCode(caller, callOpcodeRuntime(tc.op, target, successSlot))
			state.SetCode(target, storeCode(targetSlot, targetValue))
			rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &caller, big.NewInt(0), nil, 500_000, big.NewInt(1))

			gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
			require.NoError(t, err)
			execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
				Context: ctx,
				Txs:     [][]byte{rawTx},
			})
			require.NoError(t, err)
			state.ApplyChangeSet(execResult.ChangeSet)

			requireExecutionParity(t, execResult, gethResult)
			requireAddressParity(t, state, gethResult.state, sender)
			requireAddressParity(t, state, gethResult.state, caller, targetSlot, successSlot)
			requireAddressParity(t, state, gethResult.state, target, targetSlot)
			require.Equal(t, tc.wantSuccess, state.GetState(caller, successSlot))
			require.Equal(t, tc.wantCallerTarget, state.GetState(caller, targetSlot))
			require.Equal(t, tc.wantTargetTarget, state.GetState(target, targetSlot))
			if tc.wantTargetUnchanged {
				require.Equal(t, common.Hash{}, state.GetState(target, targetSlot))
			}
		})
	}
}

func TestExecutorEnvironmentOpcodeCorpusMatchesGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	contract := testAddress(0xdf)
	contractBalance := big.NewInt(123)
	slots := environmentSlots()
	ctx := blockContext(chainID)
	ctx.Number = 99
	ctx.Time = 12345
	ctx.GasLimit = 9_000_000
	ctx.BaseFee = big.NewInt(11)
	ctx.BlobBaseFee = big.NewInt(13)
	ctx.Coinbase = testAddress(0xe1)
	ctx.ParentHash = testHash(0x64)
	ctx.PrevRandao = testHash(0x65)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(1_000_000_000_000))
	state.SetBalance(contract, contractBalance)
	state.SetCode(contract, environmentRuntime(ctx.Number-1, slots))
	rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &contract, big.NewInt(0), nil, 500_000, big.NewInt(20))
	cfg := Config{MinGasPrice: big.NewInt(0)}

	gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
	require.NoError(t, err)
	execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})
	require.NoError(t, err)
	state.ApplyChangeSet(execResult.ChangeSet)

	requireExecutionParity(t, execResult, gethResult)
	requireAddressParity(t, state, gethResult.state, sender)
	requireAddressParity(t, state, gethResult.state, contract, slots.all()...)
	require.Equal(t, ctx.ParentHash, state.GetState(contract, slots.blockHash))
	require.Equal(t, common.BytesToHash(ctx.Coinbase.Bytes()), state.GetState(contract, slots.coinbase))
	require.Equal(t, common.BigToHash(new(big.Int).SetUint64(ctx.Time)), state.GetState(contract, slots.timestamp))
	require.Equal(t, common.BigToHash(new(big.Int).SetUint64(ctx.Number)), state.GetState(contract, slots.number))
	require.Equal(t, ctx.PrevRandao, state.GetState(contract, slots.prevRandao))
	require.Equal(t, common.BigToHash(new(big.Int).SetUint64(ctx.GasLimit)), state.GetState(contract, slots.gasLimit))
	require.Equal(t, common.BigToHash(chainID), state.GetState(contract, slots.chainID))
	require.Equal(t, common.BigToHash(contractBalance), state.GetState(contract, slots.selfBalance))
	require.Equal(t, common.BigToHash(ctx.BaseFee), state.GetState(contract, slots.baseFee))
	require.Equal(t, common.BigToHash(ctx.BlobBaseFee), state.GetState(contract, slots.blobBaseFee))
}

func TestExecutorVMFailureReceiptsAndFeesMatchGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	contract := testAddress(0xdd)
	coinbase := testAddress(0xde)
	storageSlot := testHash(0x50)

	tests := []struct {
		name       string
		code       []byte
		gasLimit   uint64
		wantErr    error
		checkState func(t *testing.T, state *MemoryState)
	}{
		{
			name:     "revert refunds unused gas and keeps receipt failure",
			code:     revertRuntime(),
			gasLimit: 100_000,
			wantErr:  vm.ErrExecutionReverted,
		},
		{
			name:     "invalid opcode consumes the transaction gas limit",
			code:     []byte{0xfe},
			gasLimit: 100_000,
			checkState: func(t *testing.T, state *MemoryState) {
				t.Helper()
				require.Equal(t, big.NewInt(100_000), state.GetBalance(coinbase))
			},
		},
		{
			name:     "out of gas rolls back storage",
			code:     storeCode(storageSlot, testHash(0x51)),
			gasLimit: 22_000,
			wantErr:  vm.ErrOutOfGas,
			checkState: func(t *testing.T, state *MemoryState) {
				t.Helper()
				require.Equal(t, common.Hash{}, state.GetState(contract, storageSlot))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			initialBalance := big.NewInt(1_000_000_000)
			state := NewMemoryState()
			state.SetBalance(sender, initialBalance)
			state.SetCode(contract, tc.code)
			rawTx := signLegacyTxWithGasPrice(t, key, chainID, 0, &contract, big.NewInt(0), nil, tc.gasLimit, big.NewInt(1))
			cfg := Config{MinGasPrice: big.NewInt(0)}
			ctx := blockContext(chainID)
			ctx.Coinbase = coinbase

			gethResult, err := executeGethReferenceBlock(t, state, cfg, ctx, [][]byte{rawTx})
			require.NoError(t, err)
			execResult, err := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
				Context: ctx,
				Txs:     [][]byte{rawTx},
			})
			require.NoError(t, err)
			state.ApplyChangeSet(execResult.ChangeSet)

			requireExecutionParity(t, execResult, gethResult)
			requireAddressParity(t, state, gethResult.state, sender)
			requireAddressParity(t, state, gethResult.state, contract, storageSlot)
			requireAddressParity(t, state, gethResult.state, coinbase)
			require.Equal(t, ethtypes.ReceiptStatusFailed, execResult.Receipts[0].Status)
			require.Equal(t, ethtypes.ReceiptStatusFailed, execResult.Txs[0].Status)
			if tc.wantErr != nil {
				require.ErrorIs(t, execResult.Txs[0].Err, tc.wantErr)
			} else {
				require.Error(t, execResult.Txs[0].Err)
			}
			require.Equal(t, uint64(1), state.GetNonce(sender))
			fee := new(big.Int).SetUint64(execResult.Txs[0].GasUsed)
			require.Equal(t, new(big.Int).Sub(initialBalance, fee), state.GetBalance(sender))
			require.Equal(t, fee, state.GetBalance(coinbase))
			require.Empty(t, execResult.Receipts[0].Logs)
			if tc.checkState != nil {
				tc.checkState(t, state)
			}
		})
	}
}

func TestExecutorPreVMFailuresMatchGeth(t *testing.T) {
	chainID := big.NewInt(713715)
	recipient := testAddress(0xd9)
	ctx := blockContext(chainID)

	tests := []struct {
		name  string
		cfg   Config
		setup func(t *testing.T) (*MemoryState, []byte)
		want  error
	}{
		{
			name: "nonce too high",
			setup: func(t *testing.T) (*MemoryState, []byte) {
				t.Helper()
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				sender := crypto.PubkeyToAddress(key.PublicKey)
				state := NewMemoryState()
				state.SetBalance(sender, big.NewInt(1_000_000_000_000))
				return state, signLegacyTxWithGasPrice(t, key, chainID, 1, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(1))
			},
			want: core.ErrNonceTooHigh,
		},
		{
			name: "nonce too low",
			setup: func(t *testing.T) (*MemoryState, []byte) {
				t.Helper()
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				sender := crypto.PubkeyToAddress(key.PublicKey)
				state := NewMemoryState()
				state.SetBalance(sender, big.NewInt(1_000_000_000_000))
				state.SetNonce(sender, 1)
				return state, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(1))
			},
			want: core.ErrNonceTooLow,
		},
		{
			name: "insufficient funds",
			setup: func(t *testing.T) (*MemoryState, []byte) {
				t.Helper()
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				sender := crypto.PubkeyToAddress(key.PublicKey)
				state := NewMemoryState()
				state.SetBalance(sender, big.NewInt(1))
				return state, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(1))
			},
			want: core.ErrInsufficientFunds,
		},
		{
			name: "intrinsic gas too low",
			setup: func(t *testing.T) (*MemoryState, []byte) {
				t.Helper()
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				sender := crypto.PubkeyToAddress(key.PublicKey)
				state := NewMemoryState()
				state.SetBalance(sender, big.NewInt(1_000_000_000_000))
				return state, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 20_000, big.NewInt(1))
			},
			want: core.ErrIntrinsicGas,
		},
		{
			name: "fee cap below base fee",
			cfg:  Config{DisableGasPriceCheck: true},
			setup: func(t *testing.T) (*MemoryState, []byte) {
				t.Helper()
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				sender := crypto.PubkeyToAddress(key.PublicKey)
				state := NewMemoryState()
				state.SetBalance(sender, big.NewInt(1_000_000_000_000))
				return state, signDynamicFeeTxWithFees(t, key, chainID, 0, &recipient, big.NewInt(1), nil, big.NewInt(1), big.NewInt(1), 100_000)
			},
			want: core.ErrFeeCapTooLow,
		},
		{
			name: "tip cap above fee cap",
			cfg:  Config{DisableGasPriceCheck: true},
			setup: func(t *testing.T) (*MemoryState, []byte) {
				t.Helper()
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				sender := crypto.PubkeyToAddress(key.PublicKey)
				state := NewMemoryState()
				state.SetBalance(sender, big.NewInt(1_000_000_000_000))
				return state, signDynamicFeeTxWithFees(t, key, chainID, 0, &recipient, big.NewInt(1), nil, big.NewInt(3), big.NewInt(2), 100_000)
			},
			want: core.ErrTipAboveFeeCap,
		},
		{
			name: "sender has contract code",
			setup: func(t *testing.T) (*MemoryState, []byte) {
				t.Helper()
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				sender := crypto.PubkeyToAddress(key.PublicKey)
				state := NewMemoryState()
				state.SetBalance(sender, big.NewInt(1_000_000_000_000))
				state.SetCode(sender, []byte{0x00})
				return state, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(1))
			},
			want: core.ErrSenderNoEOA,
		},
		{
			name: "sender nonce already max uint64",
			setup: func(t *testing.T) (*MemoryState, []byte) {
				t.Helper()
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				sender := crypto.PubkeyToAddress(key.PublicKey)
				state := NewMemoryState()
				state.SetBalance(sender, big.NewInt(1_000_000_000_000))
				state.SetNonce(sender, math.MaxUint64)
				return state, signLegacyTxWithGasPrice(t, key, chainID, math.MaxUint64, &recipient, big.NewInt(1), nil, 100_000, big.NewInt(1))
			},
			want: core.ErrNonceMax,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, rawTx := tc.setup(t)
			testCtx := ctx
			if errors.Is(tc.want, core.ErrFeeCapTooLow) {
				testCtx.BaseFee = big.NewInt(2)
			}
			cfg := tc.cfg
			if cfg.MinGasPrice == nil {
				cfg.MinGasPrice = big.NewInt(0)
			}

			gethResult, gethErr := executeGethReferenceBlock(t, state, cfg, testCtx, [][]byte{rawTx})
			execResult, execErr := NewExecutor(cfg, WithState(state)).ExecuteBlock(context.Background(), BlockRequest{
				Context: testCtx,
				Txs:     [][]byte{rawTx},
			})

			require.ErrorIs(t, gethErr, tc.want)
			require.ErrorIs(t, execErr, tc.want)
			require.Nil(t, gethResult)
			require.Nil(t, execResult)
			require.Equal(t, big.NewInt(0), state.GetBalance(recipient))
		})
	}
}

func TestExecutorPrepareBlockRejectsBlobTransactions(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := testAddress(0xda)
	rawTx := signBlobTxWithFees(
		t,
		key,
		chainID,
		0,
		recipient,
		big.NewInt(1),
		nil,
		big.NewInt(1),
		big.NewInt(3),
		big.NewInt(3),
		100_000,
		[]common.Hash{testHash(0x46)},
	)
	ctx := blockContext(chainID)
	ctx.BlobBaseFee = big.NewInt(1)

	prepared, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}).PrepareBlock(context.Background(), BlockRequest{
		Context: ctx,
		Txs:     [][]byte{rawTx},
	})

	require.ErrorIs(t, err, errUnsupportedBlobTx)
	require.Empty(t, prepared.Txs)
}

func TestExecutorOCCDeterministicAcrossRuns(t *testing.T) {
	chainID := big.NewInt(713715)
	txCount := 24
	rawTxs := make([][]byte, 0, txCount)
	senders := make([]common.Address, 0, txCount)
	recipients := make([]common.Address, 0, txCount)

	for i := range txCount {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := common.BigToAddress(big.NewInt(int64(40_000 + i)))
		senders = append(senders, sender)
		recipients = append(recipients, recipient)
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &recipient, big.NewInt(int64(i+1)), nil, 100_000, big.NewInt(1)))
	}

	newState := func() *MemoryState {
		state := NewMemoryState()
		for _, sender := range senders {
			state.SetBalance(sender, big.NewInt(1_000_000_000))
		}
		return state
	}

	var baseline *BlockResult
	var baselineState *MemoryState
	cfg := Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4}
	req := BlockRequest{Context: blockContext(chainID), Txs: rawTxs}
	for iteration := 0; iteration < 8; iteration++ {
		state := newState()
		executor := NewExecutor(cfg, WithState(state))
		result, err := executor.ExecuteBlock(context.Background(), req)
		executor.Close()
		require.NoError(t, err)
		require.True(t, result.OCCStats.Attempted)
		require.False(t, result.OCCStats.Fallback)
		require.Zero(t, result.OCCStats.ConflictCount)
		state.ApplyChangeSet(result.ChangeSet)

		if iteration == 0 {
			baseline = result
			baselineState = state
			continue
		}
		require.Equal(t, baseline.GasUsed, result.GasUsed)
		require.Equal(t, baseline.Txs, result.Txs)
		require.Equal(t, baseline.Receipts, result.Receipts)
		require.Equal(t, baseline.ChangeSet, result.ChangeSet)
		require.Equal(t, baseline.OCCStats, result.OCCStats)
		for i := range txCount {
			require.Equal(t, baselineState.GetBalance(senders[i]), state.GetBalance(senders[i]))
			require.Equal(t, baselineState.GetBalance(recipients[i]), state.GetBalance(recipients[i]))
		}
		require.Equal(t, baselineState.GetBalance(req.Context.Coinbase), state.GetBalance(req.Context.Coinbase))
	}
}

func executeGethReferenceBlock(t *testing.T, initial *MemoryState, cfg Config, ctx BlockContext, rawTxs [][]byte) (*gethReferenceResult, error) {
	t.Helper()
	chainConfig := chainConfigForTest(cfg, ctx)
	if err := validateBlockContext(chainConfig, ctx); err != nil {
		return nil, err
	}
	stateDB := newGethStateFromMemory(t, initial)
	evm := vm.NewEVM(buildBlockContext(ctx), stateDB, chainConfig, vm.Config{}, nil)
	gasLimit := ctx.GasLimit
	if gasLimit == 0 {
		gasLimit = math.MaxUint64
	}
	gasPool := new(core.GasPool).AddGas(gasLimit)
	baseFee := cloneOptionalBig(ctx.BaseFee)
	signer := ethtypes.MakeSigner(chainConfig, new(big.Int).SetUint64(ctx.Number), ctx.Time)
	result := &gethReferenceResult{}

	for txIndex, rawTx := range rawTxs {
		var tx ethtypes.Transaction
		if err := tx.UnmarshalBinary(rawTx); err != nil {
			return nil, err
		}
		if err := validateSupportedTx(&tx); err != nil {
			return nil, err
		}
		sender, err := ethtypes.Sender(signer, &tx)
		if err != nil {
			return nil, err
		}
		msg, err := core.TransactionToMessage(&tx, signer, baseFee)
		if err != nil {
			return nil, err
		}
		stateDB.SetTxContext(tx.Hash(), txIndex)
		evm.SetTxContext(core.NewEVMTxContext(msg))
		execResult, err := core.ApplyMessage(evm, msg, gasPool)
		if err != nil {
			return nil, err
		}
		stateDB.Finalise(true)

		txIndexUint := uint(txIndex)
		txLogs := stateDB.GetLogs(tx.Hash(), ctx.Number, ctx.BlockHash)
		status := ethtypes.ReceiptStatusSuccessful
		if execResult.Failed() {
			status = ethtypes.ReceiptStatusFailed
		}
		receipt := &ethtypes.Receipt{
			Type:              tx.Type(),
			Status:            status,
			Logs:              txLogs,
			TxHash:            tx.Hash(),
			GasUsed:           execResult.UsedGas,
			EffectiveGasPrice: effectiveGasPrice(&tx, baseFee),
			BlockHash:         ctx.BlockHash,
			BlockNumber:       new(big.Int).SetUint64(ctx.Number),
			TransactionIndex:  txIndexUint,
		}
		if tx.To() == nil {
			receipt.ContractAddress = crypto.CreateAddress(sender, tx.Nonce())
		}
		receipt.Bloom = ethtypes.CreateBloom(receipt)

		result.gasUsed += execResult.UsedGas
		receipt.CumulativeGasUsed = result.gasUsed
		result.txs = append(result.txs, TxResult{
			Hash:              tx.Hash(),
			Sender:            sender,
			To:                tx.To(),
			ContractAddress:   receipt.ContractAddress,
			Status:            status,
			GasUsed:           execResult.UsedGas,
			CumulativeGasUsed: result.gasUsed,
			EffectiveGasPrice: new(big.Int).Set(receipt.EffectiveGasPrice),
			Logs:              txLogs,
			Err:               execResult.Err,
		})
		result.receipts = append(result.receipts, receipt)
	}
	result.state = stateDB
	return result, nil
}

func newGethStateFromMemory(t *testing.T, initial *MemoryState) *gethstate.StateDB {
	t.Helper()
	db := gethstate.NewDatabase(triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil), nil)
	seed, err := gethstate.New(ethtypes.EmptyRootHash, db)
	require.NoError(t, err)
	if initial != nil {
		initial.mu.RLock()
		for addr, acct := range initial.accounts {
			if acct.Balance != nil {
				seed.SetBalance(addr, uint256.MustFromBig(acct.Balance), tracing.BalanceChangeUnspecified)
			}
			if acct.Nonce != 0 {
				seed.SetNonce(addr, acct.Nonce, tracing.NonceChangeUnspecified)
			}
			if len(acct.Code) != 0 {
				seed.SetCode(addr, acct.Code)
			}
			for key, value := range acct.Storage {
				seed.SetState(addr, key, value)
			}
		}
		initial.mu.RUnlock()
	}
	root, err := seed.Commit(0, true, false)
	require.NoError(t, err)
	stateDB, err := gethstate.New(root, db)
	require.NoError(t, err)
	return stateDB
}

func chainConfigForTest(cfg Config, ctx BlockContext) *params.ChainConfig {
	executor := &Executor{cfg: cfg.WithDefaults()}
	return executor.chainConfig(ctx)
}

func requireExecutionParity(t *testing.T, execResult *BlockResult, gethResult *gethReferenceResult) {
	t.Helper()
	require.Equal(t, gethResult.gasUsed, execResult.GasUsed)
	require.Len(t, execResult.Txs, len(gethResult.txs))
	require.Len(t, execResult.Receipts, len(gethResult.receipts))
	for i := range gethResult.txs {
		require.Equal(t, gethResult.txs[i], execResult.Txs[i])
		require.Equal(t, gethResult.receipts[i].Type, execResult.Receipts[i].Type)
		require.Equal(t, gethResult.receipts[i].Status, execResult.Receipts[i].Status)
		require.Equal(t, gethResult.receipts[i].CumulativeGasUsed, execResult.Receipts[i].CumulativeGasUsed)
		require.Equal(t, gethResult.receipts[i].Bloom, execResult.Receipts[i].Bloom)
		require.Equal(t, gethResult.receipts[i].Logs, execResult.Receipts[i].Logs)
		require.Equal(t, gethResult.receipts[i].TxHash, execResult.Receipts[i].TxHash)
		require.Equal(t, gethResult.receipts[i].ContractAddress, execResult.Receipts[i].ContractAddress)
		require.Equal(t, gethResult.receipts[i].GasUsed, execResult.Receipts[i].GasUsed)
		require.Equal(t, gethResult.receipts[i].EffectiveGasPrice, execResult.Receipts[i].EffectiveGasPrice)
		require.Equal(t, gethResult.receipts[i].BlockHash, execResult.Receipts[i].BlockHash)
		require.Equal(t, gethResult.receipts[i].BlockNumber, execResult.Receipts[i].BlockNumber)
		require.Equal(t, gethResult.receipts[i].TransactionIndex, execResult.Receipts[i].TransactionIndex)
	}
}

func requireAddressParity(t *testing.T, state *MemoryState, gethState *gethstate.StateDB, addr common.Address, slots ...common.Hash) {
	t.Helper()
	require.Zero(t, gethState.GetBalance(addr).ToBig().Cmp(state.GetBalance(addr)))
	require.Equal(t, gethState.GetNonce(addr), state.GetNonce(addr))
	require.Equal(t, gethState.GetCode(addr), state.GetCode(addr))
	for _, slot := range slots {
		require.Equal(t, gethState.GetState(addr, slot), state.GetState(addr, slot))
	}
}

func signAccessListTx(
	t *testing.T,
	key *ecdsa.PrivateKey,
	chainID *big.Int,
	nonce uint64,
	to *common.Address,
	value *big.Int,
	data []byte,
	gas uint64,
	gasPrice *big.Int,
	accessList ethtypes.AccessList,
) []byte {
	t.Helper()
	tx := ethtypes.NewTx(&ethtypes.AccessListTx{
		ChainID:    chainID,
		Nonce:      nonce,
		GasPrice:   new(big.Int).Set(gasPrice),
		Gas:        gas,
		To:         to,
		Value:      value,
		Data:       data,
		AccessList: accessList,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func sloadRuntime(slot common.Hash) []byte {
	code := appendPush32(nil, slot)
	return append(code, 0x54, 0x00)
}

func revertRuntime() []byte {
	return []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
}

func logRuntime(data common.Hash, topics []common.Hash) []byte {
	code := appendPush32(nil, data)
	code = appendPush1(code, 0)
	code = append(code, 0x52)
	for i := len(topics) - 1; i >= 0; i-- {
		code = appendPush32(code, topics[i])
	}
	code = appendPush1(code, common.HashLength)
	code = appendPush1(code, 0)
	return append(code, 0xa0+byte(len(topics)), 0x00)
}

func callOpcodeRuntime(op byte, target common.Address, successSlot common.Hash) []byte {
	code := appendPush1(nil, 0)
	code = appendPush1(code, 0)
	code = appendPush1(code, 0)
	code = appendPush1(code, 0)
	if op == 0xf1 || op == 0xf2 {
		code = appendPush1(code, 0)
	}
	code = appendPush20(code, target)
	code = appendPush2(code, 0xffff)
	code = append(code, op)
	code = appendPush32(code, successSlot)
	return append(code, 0x55, 0x00)
}

type envOpcodeSlots struct {
	blockHash   common.Hash
	coinbase    common.Hash
	timestamp   common.Hash
	number      common.Hash
	prevRandao  common.Hash
	gasLimit    common.Hash
	chainID     common.Hash
	selfBalance common.Hash
	baseFee     common.Hash
	blobBaseFee common.Hash
}

func environmentSlots() envOpcodeSlots {
	return envOpcodeSlots{
		blockHash:   testHash(0x70),
		coinbase:    testHash(0x71),
		timestamp:   testHash(0x72),
		number:      testHash(0x73),
		prevRandao:  testHash(0x74),
		gasLimit:    testHash(0x75),
		chainID:     testHash(0x76),
		selfBalance: testHash(0x77),
		baseFee:     testHash(0x78),
		blobBaseFee: testHash(0x79),
	}
}

func (s envOpcodeSlots) all() []common.Hash {
	return []common.Hash{
		s.blockHash,
		s.coinbase,
		s.timestamp,
		s.number,
		s.prevRandao,
		s.gasLimit,
		s.chainID,
		s.selfBalance,
		s.baseFee,
		s.blobBaseFee,
	}
}

func environmentRuntime(blockNumberForHash uint64, slots envOpcodeSlots) []byte {
	code := appendPushUint64(nil, blockNumberForHash)
	code = append(code, 0x40)
	code = appendStoreTop(code, slots.blockHash)
	for _, op := range []struct {
		op   byte
		slot common.Hash
	}{
		{op: 0x41, slot: slots.coinbase},
		{op: 0x42, slot: slots.timestamp},
		{op: 0x43, slot: slots.number},
		{op: 0x44, slot: slots.prevRandao},
		{op: 0x45, slot: slots.gasLimit},
		{op: 0x46, slot: slots.chainID},
		{op: 0x47, slot: slots.selfBalance},
		{op: 0x48, slot: slots.baseFee},
		{op: 0x4a, slot: slots.blobBaseFee},
	} {
		code = append(code, op.op)
		code = appendStoreTop(code, op.slot)
	}
	return append(code, 0x00)
}

func erc20TransferRuntime(fromSlot, toSlot common.Hash, from, to common.Address, amount byte) []byte {
	code := appendPush32(nil, fromSlot)
	code = append(code, 0x54)
	code = appendPush1(code, amount)
	code = append(code, 0x90, 0x03)
	code = appendPush32(code, fromSlot)
	code = append(code, 0x55)

	code = appendPush32(code, toSlot)
	code = append(code, 0x54)
	code = appendPush1(code, amount)
	code = append(code, 0x01)
	code = appendPush32(code, toSlot)
	code = append(code, 0x55)

	code = appendPush1(code, amount)
	code = appendPush1(code, 0)
	code = append(code, 0x52)
	code = appendPush20(code, to)
	code = appendPush20(code, from)
	code = appendPush32(code, erc20TransferTopic())
	code = appendPush1(code, common.HashLength)
	code = appendPush1(code, 0)
	return append(code, 0xa3, 0x00)
}

func createAndDestroyChildRuntime(beneficiary common.Address, endowment byte) []byte {
	childInit := initCode(selfDestructCode(beneficiary))
	if len(childInit) > math.MaxUint8 {
		panic("child init code too large for test runtime")
	}
	code := []byte{
		0x60, byte(len(childInit)),
		0x60, 0x00,
		0x60, 0x00,
		0x39,
		0x60, byte(len(childInit)),
		0x60, 0x00,
		0x60, endowment,
		0xf0,
		0x60, 0x00,
		0x60, 0x00,
		0x60, 0x00,
		0x60, 0x00,
		0x60, 0x00,
		0x85,
		0x61, 0xff, 0xff,
		0xf1,
		0x00,
	}
	code[3] = byte(len(code))
	return append(code, childInit...)
}

func erc20TransferTopic() common.Hash {
	return crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
}

func appendPush1(code []byte, value byte) []byte {
	return append(code, 0x60, value)
}

func appendPush2(code []byte, value uint16) []byte {
	return append(code, 0x61, byte(value>>8), byte(value))
}

func appendPush20(code []byte, value common.Address) []byte {
	code = append(code, 0x73)
	return append(code, value.Bytes()...)
}

func appendPush32(code []byte, value common.Hash) []byte {
	code = append(code, 0x7f)
	return append(code, value.Bytes()...)
}

func appendPushUint64(code []byte, value uint64) []byte {
	return appendPush32(code, common.BigToHash(new(big.Int).SetUint64(value)))
}

func appendStoreTop(code []byte, slot common.Hash) []byte {
	code = appendPush32(code, slot)
	return append(code, 0x55)
}
