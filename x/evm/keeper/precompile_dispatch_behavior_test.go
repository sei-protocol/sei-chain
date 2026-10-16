package keeper_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/precompiles/bank"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/solo"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var (
	bankAddr       = common.HexToAddress(bank.BankAddress)
	soloAddr       = common.HexToAddress(solo.SoloAddress)
	ecrecoverAddr  = common.BytesToAddress([]byte{0x01})
	unregisteredSA = common.HexToAddress("0x0000000000000000000000000000000000001010") // in the Sei range, not registered
	coldEOA        = common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	probeContract  = common.HexToAddress("0x00000000000000000000000000000000000c0de1")
	scriptContract = common.HexToAddress("0x00000000000000000000000000000000000c0de2")
	script2        = common.HexToAddress("0x00000000000000000000000000000000000c0de3")
	stopContract   = common.HexToAddress("0x00000000000000000000000000000000000c0de4")
	pcRecipient    = common.HexToAddress("0x000000000000000000000000000000000000bEEF")
)

func bankCalldata(t *testing.T, method string, args ...interface{}) []byte {
	bz, err := bank.GetABI().Pack(method, args...)
	require.NoError(t, err)
	return bz
}

// accessProbeCode stores gas of CALL(addr) in slot 2i (25 + access) and BALANCE(addr) in 2i+1 (7 + access).
func accessProbeCode(addrs []common.Address, balanceFirst bool) []byte {
	b := &bc{}
	callProbe := func(a common.Address, slot byte) {
		b.op(vm.GAS).push1(0).push1(0).push1(0).push1(0).push1(0).push20(a).push1(0).op(vm.CALL, vm.POP, vm.GAS, vm.SWAP1, vm.SUB).push1(slot).op(vm.SSTORE)
	}
	balProbe := func(a common.Address, slot byte) {
		b.op(vm.GAS).push20(a).op(vm.BALANCE, vm.POP, vm.GAS, vm.SWAP1, vm.SUB).push1(slot).op(vm.SSTORE)
	}
	for i, a := range addrs {
		if balanceFirst {
			balProbe(a, byte(2*i+1))
			callProbe(a, byte(2*i))
		} else {
			callProbe(a, byte(2*i))
			balProbe(a, byte(2*i+1))
		}
	}
	b.op(vm.STOP)
	return b.bytes()
}

const (
	warmAccess = uint64(100)
	coldAccess = uint64(2600)
)

// TestPrecompileAccessListWarmBehavior pins EIP-2929 cold/warm costs for Sei precompiles vs geth builtins.
// intentional: Sei precompiles (0x10xx) stay cold; warming would change consensus gas.
func TestPrecompileAccessListWarmBehavior(t *testing.T) {
	e := newBehaviorEnv(t, true)
	key := mustKey(t, anvilKey0Hex)
	e.associateAndFund(key, 10_000_000)
	addrs := []common.Address{bankAddr, ecrecoverAddr, soloAddr, unregisteredSA, coldEOA, coldEOA}
	e.k.SetCode(e.ctx, probeContract, accessProbeCode(addrs, false))
	e.k.SetCode(e.ctx, stopContract, []byte{byte(vm.STOP)})

	r := e.runTx(key, e.legacyTx(0, &probeContract, nil, 1_000_000, bigGwei(1), nil), 0)
	require.Empty(t, r.res.VmError)
	want := []struct{ call, bal uint64 }{
		{25 + coldAccess, 7 + warmAccess}, // bank: cold
		{25 + warmAccess, 7 + warmAccess}, // ecrecover: pre-warmed
		{25 + coldAccess, 7 + warmAccess}, // solo: cold
		{25 + coldAccess, 7 + warmAccess}, // 0x1010 unregistered: cold
		{25 + coldAccess, 7 + warmAccess}, // cold EOA
		{25 + warmAccess, 7 + warmAccess}, // same EOA: warm
	}
	for i, w := range want {
		require.Equal(t, w.call, e.slot(probeContract, 2*i).Uint64(), "CALL probe %d (%s)", i, addrs[i].Hex())
		require.Equal(t, w.bal, e.slot(probeContract, 2*i+1).Uint64(), "BALANCE probe %d (%s)", i, addrs[i].Hex())
	}

	// access list warms bank and EOA
	al := ethtypes.AccessList{{Address: bankAddr}, {Address: coldEOA}}
	r = e.runTx(key, e.dynTx(1, &probeContract, nil, 1_000_000, bigGwei(0), bigGwei(1), nil, al), 1)
	require.Empty(t, r.res.VmError)
	require.Equal(t, 25+warmAccess, e.slot(probeContract, 0).Uint64(), "access-listed Sei precompile is warm")
	require.Equal(t, 25+coldAccess, e.slot(probeContract, 4).Uint64(), "solo (not listed) still cold")
	require.Equal(t, 25+warmAccess, e.slot(probeContract, 8).Uint64(), "access-listed EOA is warm")

	// access-list intrinsic cost is the same for precompiles and EOAs
	for i, a := range []common.Address{bankAddr, ecrecoverAddr, coldEOA} {
		r = e.runTx(key, e.dynTx(uint64(2+i), &stopContract, nil, 100_000, bigGwei(0), bigGwei(1), nil, ethtypes.AccessList{{Address: a}}), 2+i)
		require.Empty(t, r.res.VmError)
		require.Equal(t, uint64(21000+2400), r.res.GasUsed, a.Hex())
	}
	// tx.to == precompile: no cold charge
	data := bankCalldata(t, "balance", pcRecipient, "usei")
	r = e.runTx(key, e.legacyTx(5, &bankAddr, nil, 200_000, bigGwei(1), data), 5)
	require.Empty(t, r.res.VmError)
	// observed
	require.Equal(t, uint64(25221), r.res.GasUsed)
}

// TestPrecompileStatefulNotCachedBehavior pins that bank precompile results are not cached within a tx.
func TestPrecompileStatefulNotCachedBehavior(t *testing.T) {
	e := newBehaviorEnv(t, true)
	key := mustKey(t, anvilKey0Hex)
	sender := e.associateAndFund(key, 10_000_000)
	recvBech32 := sdk.AccAddress(pcRecipient[:]).String()

	steps := []scriptStep{
		{op: vm.STATICCALL, to: bankAddr, data: bankCalldata(t, "balance", scriptContract, "usei")},
		{op: vm.CALL, to: bankAddr, value: bigUsei(3), data: bankCalldata(t, "sendNative", recvBech32)},
		{op: vm.STATICCALL, to: bankAddr, data: bankCalldata(t, "balance", scriptContract, "usei")},
		{op: vm.STATICCALL, to: bankAddr, data: bankCalldata(t, "balance", pcRecipient, "usei")},
	}
	e.k.SetCode(e.ctx, scriptContract, buildScript(steps))
	e.fundEVM(scriptContract, 10)
	senderBefore := e.balanceWei(sender)

	r := e.runTx(key, e.legacyTx(0, &scriptContract, nil, 2_000_000, bigGwei(1), nil), 0)
	require.Empty(t, r.res.VmError)
	for i := range steps {
		require.Equal(t, uint64(1), e.slot(scriptContract, 3*i).Uint64(), "step %d success", i)
		require.Equal(t, uint64(32), e.slot(scriptContract, 3*i+2).Uint64(), "step %d returndatasize", i)
	}
	require.Equal(t, uint64(10), e.slot(scriptContract, 1).Uint64(), "balance before")
	require.Equal(t, uint64(1), e.slot(scriptContract, 4).Uint64(), "sendNative returned true")
	require.Equal(t, uint64(7), e.slot(scriptContract, 7).Uint64(), "balance after: not cached")
	require.Equal(t, uint64(3), e.slot(scriptContract, 10).Uint64(), "recipient balance")

	// contract paid the value; EOA paid only gas
	require.Equal(t, bigUsei(7), e.balanceWei(scriptContract))
	require.Equal(t, bigUsei(3), e.balanceWei(pcRecipient))
	require.Equal(t, mulU(r.res.GasUsed, bigGwei(1)).String(), new(big.Int).Sub(senderBefore, e.balanceWei(sender)).String())
	require.True(t, r.totalSurplus().IsZero())
	// observed: 4 bank calls (first cold), value transfer, 12 fresh SSTOREs
	require.Equal(t, uint64(364391), r.res.GasUsed)
}

// TestPrecompileCallerIdentityBehavior pins that bank.sendNative debits the immediate caller (EOA or contract).
func TestPrecompileCallerIdentityBehavior(t *testing.T) {
	e := newBehaviorEnv(t, true)
	key := mustKey(t, anvilKey0Hex)
	sender := e.associateAndFund(key, 10_000_000)
	recvBech32 := sdk.AccAddress(pcRecipient[:]).String()

	// EOA -> bank.sendNative
	senderBefore := e.balanceWei(sender)
	r := e.runTx(key, e.legacyTx(0, &bankAddr, bigUsei(2), 200_000, bigGwei(1), bankCalldata(t, "sendNative", recvBech32)), 0)
	require.Empty(t, r.res.VmError)
	require.Equal(t, bigUsei(2), e.balanceWei(pcRecipient))
	fee := mulU(r.res.GasUsed, bigGwei(1))
	require.Equal(t, new(big.Int).Add(fee, bigUsei(2)).String(), new(big.Int).Sub(senderBefore, e.balanceWei(sender)).String())
	require.Equal(t, int64(0), e.balanceWei(bankAddr).Int64(), "precompile does not retain value")
	require.True(t, r.totalSurplus().IsZero())

	// contract -> bank.sendNative: contract pays; SetCode auto-associates it
	e.k.SetCode(e.ctx, script2, buildScript([]scriptStep{{op: vm.CALL, to: bankAddr, value: bigUsei(1), data: bankCalldata(t, "sendNative", recvBech32)}}))
	seiOfContract, ok := e.k.GetSeiAddress(e.ctx, script2)
	require.True(t, ok)
	require.Equal(t, sdk.AccAddress(script2[:]), seiOfContract)
	e.fundEVM(script2, 10)
	senderBefore = e.balanceWei(sender)
	r = e.runTx(key, e.legacyTx(1, &script2, nil, 2_000_000, bigGwei(1), nil), 1)
	require.Empty(t, r.res.VmError)
	require.Equal(t, uint64(1), e.slot(script2, 0).Uint64(), "CALL succeeded")
	require.Equal(t, bigUsei(9), e.balanceWei(script2), "contract paid the value")
	require.Equal(t, bigUsei(3), e.balanceWei(pcRecipient))
	require.Equal(t, mulU(r.res.GasUsed, bigGwei(1)).String(), new(big.Int).Sub(senderBefore, e.balanceWei(sender)).String(), "EOA paid only gas")
	require.True(t, r.totalSurplus().IsZero())
}

// TestPrecompileCallCodeDelegateCallBehavior pins that CALLCODE/DELEGATECALL to bank allow views and reject sendNative.
func TestPrecompileCallCodeDelegateCallBehavior(t *testing.T) {
	e := newBehaviorEnv(t, true)
	key := mustKey(t, anvilKey0Hex)
	e.associateAndFund(key, 10_000_000)
	recvBech32 := sdk.AccAddress(pcRecipient[:]).String()
	steps := []scriptStep{
		{op: vm.CALLCODE, to: bankAddr, gas: 100_000, data: bankCalldata(t, "balance", scriptContract, "usei")},
		{op: vm.CALLCODE, to: bankAddr, gas: 100_000, value: bigUsei(1), data: bankCalldata(t, "sendNative", recvBech32)},
		{op: vm.DELEGATECALL, to: bankAddr, gas: 100_000, data: bankCalldata(t, "balance", scriptContract, "usei")},
		{op: vm.DELEGATECALL, to: bankAddr, gas: 100_000, data: bankCalldata(t, "sendNative", recvBech32)},
	}
	e.k.SetCode(e.ctx, scriptContract, buildScript(steps))
	e.fundEVM(scriptContract, 10)

	r := e.runTx(key, e.legacyTx(0, &scriptContract, nil, 2_000_000, bigGwei(1), nil), 0)
	require.Empty(t, r.res.VmError)
	type obs struct{ ok, word, rds uint64 }
	got := make([]obs, len(steps))
	for i := range steps {
		got[i] = obs{e.slot(scriptContract, 3*i).Uint64(), e.slot(scriptContract, 3*i+1).Uint64(), e.slot(scriptContract, 3*i+2).Uint64()}
	}
	require.Equal(t, []obs{
		{1, 10, 32}, // CALLCODE view method: allowed
		{0, 0, 0},   // CALLCODE sendNative: rejected
		{1, 10, 32}, // DELEGATECALL view method: allowed
		{0, 0, 0},   // DELEGATECALL sendNative: rejected
	}, got)
	require.Equal(t, bigUsei(10), e.balanceWei(scriptContract))
	require.Equal(t, int64(0), e.balanceWei(pcRecipient).Int64())
	require.True(t, r.totalSurplus().IsZero())
	// a failing Sei precompile consumes all forwarded gas, unlike a revert
	require.Equal(t, uint64(386286), r.res.GasUsed)

	// forwarding 50k more gas to a rejected call costs exactly 50k more
	gasFor := func(nonce uint64, idx int, fwd uint64) uint64 {
		addr := common.BigToAddress(big.NewInt(0xc0de10 + int64(idx)))
		e.k.SetCode(e.ctx, addr, buildScript([]scriptStep{{op: vm.DELEGATECALL, to: bankAddr, gas: fwd, data: bankCalldata(t, "sendNative", recvBech32)}}))
		rr := e.runTx(key, e.legacyTx(nonce, &addr, nil, 1_000_000, bigGwei(1), nil), idx)
		require.Empty(t, rr.res.VmError)
		require.Equal(t, uint64(0), e.slot(addr, 0).Uint64())
		return rr.res.GasUsed
	}
	g50 := gasFor(1, 1, 50_000)
	g100 := gasFor(2, 2, 100_000)
	require.Equal(t, uint64(50_000), g100-g50)
}

// TestPrecompileDepthBehavior pins the EVM depth precompiles observe (1 direct, 2 via contract).
func TestPrecompileDepthBehavior(t *testing.T) {
	e := newBehaviorEnv(t, true)
	key := mustKey(t, anvilKey0Hex)
	sender := e.associateAndFund(key, 10_000_000)

	soloABI := pcommon.MustGetABI(solo.F, "abi.json")
	claim, err := soloABI.Pack("claim", []byte{})
	require.NoError(t, err)

	// direct: depth check passes, fails on payload
	r := e.runTx(key, e.legacyTx(0, &soloAddr, nil, 500_000, bigGwei(1), claim), 0)
	require.NotEmpty(t, r.res.VmError)
	require.NotContains(t, r.receipt.VmError, "claim must be called by an EOA directly")
	// via contract: precompile sees depth 2
	e.k.SetCode(e.ctx, scriptContract, buildScript([]scriptStep{{op: vm.CALL, to: soloAddr, data: claim, revertOnFailure: true}}))
	r = e.runTx(key, e.legacyTx(1, &scriptContract, nil, 500_000, bigGwei(1), nil), 1)
	require.NotEmpty(t, r.res.VmError)
	require.True(t, strings.Contains(r.receipt.VmError, "claim must be called by an EOA directly"), r.receipt.VmError)

	// tracer view
	type enter struct {
		depth int
		typ   vm.OpCode
		from  common.Address
		to    common.Address
	}
	collect := func(to common.Address, data []byte, value *big.Int) []enter {
		var got []enter
		hooks := &tracing.Hooks{OnEnter: func(depth int, typ byte, from, to common.Address, _ []byte, _ uint64, _ *big.Int) {
			got = append(got, enter{depth, vm.OpCode(typ), from, to})
		}}
		_, err := e.traceCall(sender, to, data, value, hooks)
		require.NoError(t, err)
		return got
	}
	recvBech32 := sdk.AccAddress(pcRecipient[:]).String()
	send := bankCalldata(t, "sendNative", recvBech32)

	// EOA -> bank: synthetic entries at depth 2
	got := collect(bankAddr, send, bigUsei(1))
	require.Equal(t, []enter{
		{0, vm.CALL, sender, bankAddr},
		{2, vm.CALL, bankAddr, sender},    // refund to payer
		{2, vm.CALL, sender, pcRecipient}, // synthetic native transfer
	}, got)

	// EOA -> contract -> bank: synthetic entries at depth 3
	e.k.SetCode(e.ctx, script2, buildScript([]scriptStep{{op: vm.CALL, to: bankAddr, value: bigUsei(1), data: send}}))
	e.fundEVM(script2, 5)
	got = collect(script2, nil, nil)
	require.Equal(t, []enter{
		{0, vm.CALL, sender, script2},
		{1, vm.CALL, script2, bankAddr},
		{3, vm.CALL, bankAddr, script2},
		{3, vm.CALL, script2, pcRecipient},
	}, got)
}

// traceCall runs a top-level evm.Call on a fresh DBImpl with tracer hooks.
func (e *behaviorEnv) traceCall(from, to common.Address, data []byte, value *big.Int, hooks *tracing.Hooks) ([]byte, error) {
	ctx := e.ctx.WithIsEVM(true)
	db := state.NewDBImpl(ctx, e.k, false)
	blockCtx, err := e.k.GetVMBlockContext(ctx, e.k.GetGasPool())
	if err != nil {
		return nil, err
	}
	sstore := e.k.GetSstoreSetGasEIP2200(ctx)
	cfg := types.DefaultChainConfig().EthereumConfigWithSstore(e.k.ChainID(ctx), &sstore)
	evm := vm.NewEVM(*blockCtx, db, cfg, vm.Config{Tracer: hooks}, e.k.CustomPrecompiles(ctx))
	evm.SetTxContext(vm.TxContext{Origin: from, GasPrice: big.NewInt(0)})
	v := uint256.NewInt(0)
	if value != nil {
		v = uint256.MustFromBig(value)
	}
	ret, _, err := evm.Call(from, to, data, 1_000_000, v)
	if err != nil {
		return ret, err
	}
	_, err = db.Finalize()
	return ret, err
}

type fakePrecompile struct{ calls *int }

func (f fakePrecompile) RequiredGas([]byte) uint64 { return 0 }
func (f fakePrecompile) Run(*vm.EVM, common.Address, common.Address, []byte, *big.Int, bool, bool, *tracing.Hooks) ([]byte, error) {
	*f.calls++
	return []byte("fake"), nil
}

// TestBuiltinPrecompileWinsOnCollisionBehavior pins that geth builtins override colliding custom precompiles.
func TestBuiltinPrecompileWinsOnCollisionBehavior(t *testing.T) {
	e := newBehaviorEnv(t, true)
	key := mustKey(t, anvilKey0Hex)
	_, signerAddr := keyAddrs(key)

	hash := crypto.Keccak256([]byte("sei"))
	sig, err := crypto.Sign(hash, key)
	require.NoError(t, err)
	input := make([]byte, 128)
	copy(input[0:32], hash)
	input[63] = sig[64] + 27
	copy(input[64:96], sig[0:32])
	copy(input[96:128], sig[32:64])

	calls := 0
	db := state.NewDBImpl(e.ctx, e.k, true)
	blockCtx, err := e.k.GetVMBlockContext(e.ctx, e.k.GetGasPool())
	require.NoError(t, err)
	cfg := types.DefaultChainConfig().EthereumConfig(e.k.ChainID(e.ctx))
	custom := map[common.Address]vm.PrecompiledContract{
		ecrecoverAddr:  fakePrecompile{calls: &calls},
		unregisteredSA: fakePrecompile{calls: &calls},
	}
	evm := vm.NewEVM(*blockCtx, db, cfg, vm.Config{}, custom)
	ret, left, err := evm.Call(signerAddr, ecrecoverAddr, input, 10_000, uint256.NewInt(0))
	require.NoError(t, err)
	require.Equal(t, common.LeftPadBytes(signerAddr.Bytes(), 32), ret)
	require.Equal(t, uint64(10_000-3000), left)
	require.Equal(t, 0, calls, "custom precompile at builtin address never invoked")

	// non-colliding custom address dispatches to custom contract
	ret, _, err = evm.Call(signerAddr, unregisteredSA, nil, 10_000, uint256.NewInt(0))
	require.NoError(t, err)
	require.Equal(t, []byte("fake"), ret)
	require.Equal(t, 1, calls)

	// Sei precompiles do not overlap geth builtins
	rules := cfg.Rules(big.NewInt(e.ctx.BlockHeight()), true, uint64(e.ctx.BlockTime().Unix()))
	for _, a := range vm.ActivePrecompiles(rules) {
		_, clash := e.k.CustomPrecompiles(e.ctx)[a]
		require.False(t, clash, a.Hex())
	}
}

// TestPreparePrecompileWarmingBehavior pins Prepare warming: Sei precompiles warm only when tracing pacific-1 < 94496767.
func TestPreparePrecompileWarmingBehavior(t *testing.T) {
	e := newBehaviorEnv(t, true)
	_, sender := keyAddrs(mustKey(t, anvilKey0Hex))
	cfg := types.DefaultChainConfig().EthereumConfig(e.k.ChainID(e.ctx))
	rules := cfg.Rules(big.NewInt(e.ctx.BlockHeight()), true, uint64(e.ctx.BlockTime().Unix()))
	precompiles := append(vm.ActivePrecompiles(rules), bankAddr, soloAddr)
	for _, tc := range []struct {
		name       string
		ctx        sdk.Context
		customWarm bool
	}{
		{"deliver", e.ctx, false},
		{"tracing_non_pacific", e.ctx.WithIsTracing(true), false},
		{"tracing_pacific_below_94496767", e.ctx.WithIsTracing(true).WithChainID("pacific-1").WithBlockHeight(94496766), true},
		{"tracing_pacific_at_94496767", e.ctx.WithIsTracing(true).WithChainID("pacific-1").WithBlockHeight(94496767), false},
		{"not_tracing_pacific_below", e.ctx.WithChainID("pacific-1").WithBlockHeight(94496766), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := state.NewDBImpl(tc.ctx, e.k, true)
			db.Prepare(rules, sender, coldEOA, &stopContract, precompiles, nil)
			require.True(t, db.AddressInAccessList(ecrecoverAddr))
			require.Equal(t, tc.customWarm, db.AddressInAccessList(bankAddr))
			require.Equal(t, tc.customWarm, db.AddressInAccessList(soloAddr))
			require.True(t, db.AddressInAccessList(stopContract))
			require.True(t, db.AddressInAccessList(coldEOA), "coinbase warmed")
		})
	}
}
