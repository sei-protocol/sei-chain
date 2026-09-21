package evmonly

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// blockExecEnv is the block-constant input to transaction execution: the chain
// rules in force, the EVM block context, and the precompiles resolvable at this
// height.
type blockExecEnv struct {
	chainConfig *params.ChainConfig
	blockCtx    vm.BlockContext
	rules       params.Rules
	precompiles map[common.Address]vm.PrecompiledContract
	customOnly  map[common.Address]vm.PrecompiledContract
}

func (e *Executor) newBlockExecEnv(ctx BlockContext, custom map[common.Address]vm.PrecompiledContract) *blockExecEnv {
	chainConfig := e.chainConfig(ctx)
	blockCtx := buildBlockContext(ctx)
	rules := chainConfig.Rules(blockCtx.BlockNumber, blockCtx.Random != nil, blockCtx.Time)
	return &blockExecEnv{
		chainConfig: chainConfig,
		blockCtx:    blockCtx,
		rules:       rules,
		precompiles: vm.ActivePrecompiledContracts(rules, custom),
		customOnly:  custom,
	}
}

// txEVM hands out the EVM for a state database, building it on first use. A
// transaction that takes the plain-transfer path never asks for one.
type txEVM struct {
	env     *blockExecEnv
	stateDB *nativeStateDB
	evm     *vm.EVM
}

func newTxEVM(env *blockExecEnv, stateDB *nativeStateDB) *txEVM {
	return &txEVM{env: env, stateDB: stateDB}
}

func (t *txEVM) get() *vm.EVM {
	if t.evm == nil {
		t.evm = vm.NewEVM(t.env.blockCtx, t.stateDB, t.env.chainConfig, vm.Config{}, t.env.customOnly)
		t.stateDB.SetEVM(t.evm)
	}
	return t.evm
}

// plainTransferCandidate reports whether msg has the stateless shape of a
// value transfer to an account: a call with no calldata, no access list, no
// authorization list, no blobs, and a recipient that is not a precompile. The
// recipient's code is checked later, once the pre-checks have passed.
func (env *blockExecEnv) plainTransferCandidate(msg *core.Message) bool {
	if msg.To == nil || len(msg.Data) != 0 || len(msg.AccessList) != 0 {
		return false
	}
	if msg.SetCodeAuthorizations != nil || msg.BlobHashes != nil || msg.BlobGasFeeCap != nil {
		return false
	}
	if env.rules.IsEIP4762 {
		return false
	}
	_, isPrecompile := env.precompiles[*msg.To]
	return !isPrecompile
}

// applyPlainTransfer executes msg as a value transfer without an EVM,
// reproducing core.ApplyMessage's state transition for a call to a codeless
// account: the same pre-checks, in the same order and with the same errors,
// then gas purchase, nonce increment, value movement, gas return and fee
// payment through the same StateDB calls.
//
// It returns applied=false, with the state and gas pool untouched, when the
// recipient turns out to hold code; the caller then runs core.ApplyMessage,
// which re-reads the same keys the pre-checks recorded in the access set. An
// error is returned before any state is modified, so the caller needs no
// snapshot.
func (env *blockExecEnv) applyPlainTransfer(
	stateDB *nativeStateDB,
	gasPool *core.GasPool,
	msg *core.Message,
) (result *core.ExecutionResult, applied bool, err error) {
	// Stateless checks (core.StateTransition.StatelessChecks).
	if !msg.SkipNonceChecks {
		stNonce := stateDB.GetNonce(msg.From)
		if msgNonce := msg.Nonce; stNonce < msgNonce {
			return nil, true, fmt.Errorf("%w: address %v, tx: %d state: %d", core.ErrNonceTooHigh,
				msg.From.Hex(), msgNonce, stNonce)
		} else if stNonce > msgNonce {
			return nil, true, fmt.Errorf("%w: address %v, tx: %d state: %d", core.ErrNonceTooLow,
				msg.From.Hex(), msgNonce, stNonce)
		} else if stNonce+1 < stNonce {
			return nil, true, fmt.Errorf("%w: address %v, nonce: %d", core.ErrNonceMax,
				msg.From.Hex(), stNonce)
		}
	}
	if !msg.SkipFromEOACheck {
		code := stateDB.GetCode(msg.From)
		_, delegated := ethtypes.ParseDelegation(code)
		if len(code) > 0 && !delegated {
			return nil, true, fmt.Errorf("%w: address %v, len(code): %d", core.ErrSenderNoEOA, msg.From.Hex(), len(code))
		}
	}
	baseFee := env.blockCtx.BaseFee
	if env.rules.IsLondon {
		if l := msg.GasFeeCap.BitLen(); l > 256 {
			return nil, true, fmt.Errorf("%w: address %v, maxFeePerGas bit length: %d", core.ErrFeeCapVeryHigh,
				msg.From.Hex(), l)
		}
		if l := msg.GasTipCap.BitLen(); l > 256 {
			return nil, true, fmt.Errorf("%w: address %v, maxPriorityFeePerGas bit length: %d", core.ErrTipVeryHigh,
				msg.From.Hex(), l)
		}
		if msg.GasFeeCap.Cmp(msg.GasTipCap) < 0 {
			return nil, true, fmt.Errorf("%w: address %v, maxPriorityFeePerGas: %s, maxFeePerGas: %s", core.ErrTipAboveFeeCap,
				msg.From.Hex(), msg.GasTipCap, msg.GasFeeCap)
		}
		if msg.GasFeeCap.Cmp(baseFee) < 0 {
			return nil, true, fmt.Errorf("%w: address %v, maxFeePerGas: %s, baseFee: %s", core.ErrFeeCapTooLow,
				msg.From.Hex(), msg.GasFeeCap, baseFee)
		}
	}

	// Gas purchase check (core.StateTransition.BuyGas), debited below.
	mgval := new(big.Int).SetUint64(msg.GasLimit)
	mgval.Mul(mgval, msg.GasPrice)
	balanceCheck := new(big.Int).Set(mgval)
	if msg.GasFeeCap != nil {
		balanceCheck.SetUint64(msg.GasLimit)
		balanceCheck.Mul(balanceCheck, msg.GasFeeCap)
	}
	balanceCheck.Add(balanceCheck, msg.Value)
	balanceCheckU256, overflow := uint256.FromBig(balanceCheck)
	if overflow {
		return nil, true, fmt.Errorf("%w: address %v required balance exceeds 256 bits", core.ErrInsufficientFunds, msg.From.Hex())
	}
	if have, want := stateDB.GetBalance(msg.From), balanceCheckU256; have.Cmp(want) < 0 {
		return nil, true, fmt.Errorf("%w: address %v have %v want %v", core.ErrInsufficientFunds, msg.From.Hex(), have, want)
	}
	mgvalU256, _ := uint256.FromBig(mgval)

	// Block gas check (core.GasPool.SubGas), debited below.
	if gasPool.Gas() < msg.GasLimit {
		return nil, true, core.ErrGasLimitReached
	}

	// Intrinsic gas (core.StateTransition.Execute clauses 4-6).
	intrinsic, err := core.IntrinsicGas(msg.Data, msg.AccessList, msg.SetCodeAuthorizations, false, env.rules.IsHomestead, env.rules.IsIstanbul, env.rules.IsShanghai)
	if err != nil {
		return nil, true, err
	}
	if msg.GasLimit < intrinsic {
		return nil, true, fmt.Errorf("%w: have %d, want %d", core.ErrIntrinsicGas, msg.GasLimit, intrinsic)
	}
	var floorDataGas uint64
	if env.rules.IsPrague {
		floorDataGas, err = core.FloorDataGas(msg.Data)
		if err != nil {
			return nil, true, err
		}
		if msg.GasLimit < floorDataGas {
			return nil, true, fmt.Errorf("%w: have %d, want %d", core.ErrFloorDataGas, msg.GasLimit, floorDataGas)
		}
	}
	value, overflow := uint256.FromBig(msg.Value)
	if overflow {
		return nil, true, fmt.Errorf("%w: address %v", core.ErrInsufficientFundsForTransfer, msg.From.Hex())
	}
	// The balance check above covers gasLimit*gasPrice + value, so the post-purchase
	// CanTransfer check in StateTransition cannot fail here.

	// The recipient must hold no code; a delegation designator counts as code.
	to := *msg.To
	if stateDB.GetCodeSize(to) != 0 {
		return nil, false, nil
	}

	// State transition. Nothing above has written, and nothing below can fail.
	if err := gasPool.SubGas(msg.GasLimit); err != nil {
		return nil, true, err
	}
	stateDB.SubBalance(msg.From, mgvalU256, tracing.BalanceDecreaseGasBuy)
	gasRemaining := msg.GasLimit - intrinsic
	stateDB.SetNonce(msg.From, stateDB.GetNonce(msg.From)+1, tracing.NonceChangeEoACall)

	// vm.EVM.Call for a codeless, non-precompile recipient.
	if !stateDB.Exist(to) {
		if env.rules.IsEIP158 && value.IsZero() {
			// Calling a non-existing account, don't do anything.
		} else {
			stateDB.CreateAccount(to)
			env.blockCtx.Transfer(stateDB, msg.From, to, value)
		}
	} else {
		env.blockCtx.Transfer(stateDB, msg.From, to, value)
	}

	// Refund, floor and gas return (calcRefund, EIP-7623, returnGas).
	gasUsed := msg.GasLimit - gasRemaining
	refund := gasUsed / params.RefundQuotient
	if env.rules.IsLondon {
		refund = gasUsed / params.RefundQuotientEIP3529
	}
	if stateRefund := stateDB.GetRefund(); refund > stateRefund {
		refund = stateRefund
	}
	gasRemaining += refund
	gasUsed = msg.GasLimit - gasRemaining
	if env.rules.IsPrague && gasUsed < floorDataGas {
		gasRemaining = msg.GasLimit - floorDataGas
		gasUsed = floorDataGas
	}
	remaining := uint256.NewInt(gasRemaining)
	remaining.Mul(remaining, uint256.MustFromBig(msg.GasPrice))
	stateDB.AddBalance(msg.From, remaining, tracing.BalanceIncreaseGasReturn)
	gasPool.AddGas(gasRemaining)

	// Fee payment: Sei routes the base fee to the coinbase along with the tip.
	effectiveTip := msg.GasPrice
	if env.rules.IsLondon {
		effectiveTip = new(big.Int).Sub(msg.GasFeeCap, baseFee)
		if effectiveTip.Cmp(msg.GasTipCap) > 0 {
			effectiveTip = msg.GasTipCap
		}
	}
	fee := new(uint256.Int).SetUint64(gasUsed)
	if baseFee == nil || baseFee.Sign() == 0 {
		baseFee = common.Big0
	}
	totalFeePerGas := new(big.Int).Add(baseFee, effectiveTip)
	fee.Mul(fee, uint256.MustFromBig(totalFeePerGas))
	stateDB.AddBalance(env.blockCtx.Coinbase, fee, tracing.BalanceIncreaseRewardTransactionFee)

	return &core.ExecutionResult{
		UsedGas:     gasUsed,
		RefundedGas: refund,
	}, true, nil
}
