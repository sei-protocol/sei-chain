package keeper_test

// Shared helpers for *_behavior_test.go tests that pin current Sei EVM consensus behaviour.

import (
	"crypto/ecdsa"
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	appante "github.com/sei-protocol/sei-chain/app/ante"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// Well-known anvil/hardhat development keys. Test-only.
const (
	anvilKey0Hex = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	anvilKey1Hex = "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"
	anvilKey2Hex = "5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a"
)

const (
	gwei       = int64(1_000_000_000)
	weiPerUsei = int64(1_000_000_000_000)
)

type behaviorEnv struct {
	t   *testing.T
	app *app.App
	k   *keeper.Keeper
	ctx sdk.Context
}

// newBehaviorEnv builds a fresh app, optionally with Sei custom precompiles.
func newBehaviorEnv(t *testing.T, withPrecompiles bool) *behaviorEnv {
	a := app.Setup(t, false, withPrecompiles, false)
	ctx := a.GetContextForDeliverTx([]byte{}).
		WithBlockHeight(8).
		WithBlockTime(time.Unix(1_700_000_000, 0))
	k := &a.EvmKeeper
	k.InitGenesis(ctx, *types.DefaultGenesis())
	return &behaviorEnv{t: t, app: a, k: k, ctx: ctx}
}

func mustKey(t *testing.T, hexKey string) *ecdsa.PrivateKey {
	key, err := crypto.HexToECDSA(hexKey)
	require.NoError(t, err)
	return key
}

func keyAddrs(key *ecdsa.PrivateKey) (sdk.AccAddress, common.Address) {
	pub := secp256k1.PubKey{Key: crypto.CompressPubkey(&key.PublicKey)}
	return sdk.AccAddress(pub.Address()), crypto.PubkeyToAddress(key.PublicKey)
}

// associateAndFund associates the key's EVM and Sei addresses and funds it with usei.
func (e *behaviorEnv) associateAndFund(key *ecdsa.PrivateKey, usei int64) common.Address {
	seiAddr, evmAddr := keyAddrs(key)
	e.k.SetAddressMapping(e.ctx, seiAddr, evmAddr)
	e.fundSei(seiAddr, usei)
	return evmAddr
}

func (e *behaviorEnv) fundSei(addr sdk.AccAddress, usei int64) {
	coins := sdk.NewCoins(sdk.NewCoin(e.k.GetBaseDenom(e.ctx), sdk.NewInt(usei)))
	require.NoError(e.t, e.k.BankKeeper().MintCoins(e.ctx, types.ModuleName, coins))
	require.NoError(e.t, e.k.BankKeeper().SendCoinsFromModuleToAccount(e.ctx, types.ModuleName, addr, coins))
}

// fundEVM funds the Sei address addr maps to (direct-cast if unassociated).
func (e *behaviorEnv) fundEVM(addr common.Address, usei int64) {
	e.fundSei(e.k.GetSeiAddressOrDefault(e.ctx, addr), usei)
}

// balanceWei returns the EVM-visible balance (usei*1e12 + wei) of addr.
func (e *behaviorEnv) balanceWei(addr common.Address) *big.Int {
	return e.k.GetBalance(e.ctx, e.k.GetSeiAddressOrDefault(e.ctx, addr))
}

func (e *behaviorEnv) coinbaseWei(txIndex int) *big.Int {
	return e.k.GetBalance(e.ctx, state.GetCoinbaseAddress(txIndex))
}

func (e *behaviorEnv) signer() ethtypes.Signer {
	return ethtypes.LatestSignerForChainID(e.k.ChainID(e.ctx))
}

type behaviorTxResult struct {
	res         *types.MsgEVMTransactionResponse
	tx          *ethtypes.Transaction
	anteSurplus sdk.Int // surplus recorded by the ante handler for this tx
	execSurplus sdk.Int // surplus recorded by the msg server (DeferredInfo) for this tx
	receipt     *types.Receipt
	txIndex     int
	ctxAfterTx  sdk.Context
}

func (r behaviorTxResult) totalSurplus() sdk.Int { return r.anteSurplus.Add(r.execSurplus) }

// runTx signs txData and runs it through EvmDeliverTxAnte and the msg server at txIndex.
func (e *behaviorEnv) runTx(key *ecdsa.PrivateKey, txData ethtypes.TxData, txIndex int) behaviorTxResult {
	t := e.t
	tx, err := ethtypes.SignNewTx(key, e.signer(), txData)
	require.NoError(t, err)
	td, err := ethtx.NewTxDataFromTx(tx)
	require.NoError(t, err)
	msg, err := types.NewMsgEVMTransaction(td)
	require.NoError(t, err)

	ctx := e.ctx.WithTxIndex(txIndex)
	anteBefore := e.k.GetAnteSurplusSum(infiniteGas(ctx))
	ctx, err = appante.EvmDeliverTxAnte(ctx, e.app.GetTxConfig(), mockTx{msgs: []sdk.Msg{msg}}, &e.app.UpgradeKeeper, e.k)
	require.NoError(t, err)
	anteSurplus := e.k.GetAnteSurplusSum(infiniteGas(ctx)).Sub(anteBefore)

	res, err := keeper.NewMsgServerImpl(e.k).EVMTransaction(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)

	info, found := e.k.GetEVMTxDeferredInfo(infiniteGas(ctx))
	require.True(t, found)
	receipt, err := e.k.GetTransientReceipt(infiniteGas(ctx), tx.Hash(), uint64(txIndex))
	require.NoError(t, err)
	return behaviorTxResult{
		res:         res,
		tx:          tx,
		anteSurplus: anteSurplus,
		execSurplus: info.Surplus,
		receipt:     receipt,
		txIndex:     txIndex,
		ctxAfterTx:  ctx,
	}
}

func infiniteGas(ctx sdk.Context) sdk.Context {
	return ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
}

// legacyTx builds a protected legacy tx.
func (e *behaviorEnv) legacyTx(nonce uint64, to *common.Address, value *big.Int, gas uint64, gasPrice *big.Int, data []byte) ethtypes.TxData {
	if value == nil {
		value = big.NewInt(0)
	}
	return &ethtypes.LegacyTx{Nonce: nonce, To: to, Value: value, Gas: gas, GasPrice: gasPrice, Data: data}
}

func (e *behaviorEnv) dynTx(nonce uint64, to *common.Address, value *big.Int, gas uint64, tipCap, feeCap *big.Int, data []byte, al ethtypes.AccessList) ethtypes.TxData {
	if value == nil {
		value = big.NewInt(0)
	}
	return &ethtypes.DynamicFeeTx{
		ChainID:    e.k.ChainID(e.ctx),
		Nonce:      nonce,
		GasTipCap:  tipCap,
		GasFeeCap:  feeCap,
		Gas:        gas,
		To:         to,
		Value:      value,
		Data:       data,
		AccessList: al,
	}
}

func bigGwei(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(gwei)) }
func bigUsei(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(weiPerUsei)) }
func mulU(a uint64, b *big.Int) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(a), b)
}

// ---------------------------------------------------------------------------
// Tiny bytecode assembler
// ---------------------------------------------------------------------------

type bc struct{ code []byte }

func (b *bc) op(ops ...vm.OpCode) *bc {
	for _, o := range ops {
		b.code = append(b.code, byte(o))
	}
	return b
}

func (b *bc) push1(v byte) *bc { b.code = append(b.code, byte(vm.PUSH1), v); return b }
func (b *bc) push2(v uint16) *bc {
	b.code = append(b.code, byte(vm.PUSH2), byte(v>>8), byte(v))
	return b
}
func (b *bc) push20(a common.Address) *bc {
	b.code = append(b.code, byte(vm.PUSH20))
	b.code = append(b.code, a.Bytes()...)
	return b
}
func (b *bc) push32(v *big.Int) *bc {
	b.code = append(b.code, byte(vm.PUSH32))
	b.code = append(b.code, common.BigToHash(v).Bytes()...)
	return b
}
func (b *bc) bytes() []byte { return b.code }
func (b *bc) pc() int       { return len(b.code) }

// initcodeFor wraps runtime in a constructor that SSTOREs each {slot, val} in preStores.
func initcodeFor(runtime []byte, preStores ...[2]byte) []byte {
	pre := &bc{}
	for _, sv := range preStores {
		pre.push1(sv[1]).push1(sv[0]).op(vm.SSTORE)
	}
	// PUSH1 len DUP1 PUSH1 off PUSH1 0 CODECOPY PUSH1 0 RETURN (11 bytes)
	off := pre.pc() + 11
	if len(runtime) > 255 || off > 255 {
		panic("initcodeFor: runtime too large for PUSH1 encoding")
	}
	pre.push1(byte(len(runtime))).op(vm.DUP1).push1(byte(off)).push1(0).op(vm.CODECOPY).push1(0).op(vm.RETURN)
	return append(pre.bytes(), runtime...)
}

// scriptStep is one external call performed by a script contract.
type scriptStep struct {
	op              vm.OpCode // CALL, CALLCODE, DELEGATECALL, STATICCALL
	to              common.Address
	value           *big.Int // CALL / CALLCODE only
	data            []byte
	gas             uint64 // gas to forward; 0 means all available (GAS opcode)
	revertOnFailure bool   // REVERT the frame if the call fails
}

// buildScript assembles a contract running steps in order; step i stores
// slot 3i = success, 3i+1 = first return word, 3i+2 = RETURNDATASIZE.
func buildScript(steps []scriptStep) []byte {
	b := &bc{}
	type fix struct {
		pos  int // position of the 2-byte immediate
		step int
	}
	var fixes []fix
	for i, s := range steps {
		slot := byte(3 * i)
		b.push1(0).push1(0).op(vm.MSTORE)
		// CODECOPY(destOffset=32, offset=<data>, size=len)
		b.push2(uint16(len(s.data)))
		b.code = append(b.code, byte(vm.PUSH2), 0, 0)
		fixes = append(fixes, fix{pos: b.pc() - 2, step: i})
		b.push1(32).op(vm.CODECOPY)
		// call args
		b.push1(32)                  // retSize
		b.push1(0)                   // retOffset
		b.push2(uint16(len(s.data))) // argsSize
		b.push1(32)                  // argsOffset
		if s.op == vm.CALL || s.op == vm.CALLCODE {
			v := s.value
			if v == nil {
				v = big.NewInt(0)
			}
			b.push32(v)
		}
		b.push20(s.to)
		if s.gas > 0 {
			b.push32(new(big.Int).SetUint64(s.gas))
		} else {
			b.op(vm.GAS)
		}
		b.op(s.op)
		if s.revertOnFailure {
			// DUP1 PUSH2 ok JUMPI PUSH1 0 PUSH1 0 REVERT ok: JUMPDEST
			ok := b.pc() + 10
			b.op(vm.DUP1).push2(uint16(ok)).op(vm.JUMPI).push1(0).push1(0).op(vm.REVERT).op(vm.JUMPDEST)
		}
		b.push1(slot).op(vm.SSTORE)
		b.push1(0).op(vm.MLOAD).push1(slot + 1).op(vm.SSTORE)
		b.op(vm.RETURNDATASIZE).push1(slot + 2).op(vm.SSTORE)
	}
	b.op(vm.STOP)
	offsets := make([]int, len(steps))
	for i, s := range steps {
		offsets[i] = b.pc()
		b.code = append(b.code, s.data...)
	}
	for _, f := range fixes {
		binary.BigEndian.PutUint16(b.code[f.pos:], uint16(offsets[f.step]))
	}
	return b.code
}

func slotHash(i int) common.Hash { return common.BigToHash(big.NewInt(int64(i))) }

func (e *behaviorEnv) slot(addr common.Address, i int) *big.Int {
	return e.k.GetState(e.ctx, addr, slotHash(i)).Big()
}
