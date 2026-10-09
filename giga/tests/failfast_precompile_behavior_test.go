package giga_test

// Pins giga-to-v2 fallback when a Sei custom precompile triggers a fail-fast abort.

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	evmstate "github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

var (
	ffBankAddr    = common.HexToAddress("0x0000000000000000000000000000000000001001")
	ffCallerAddr  = common.HexToAddress("0x00000000000000000000000000000000000ca110")
	ffOuterAddr   = common.HexToAddress("0x00000000000000000000000000000000000ca111")
	ffFactoryAddr = common.HexToAddress("0x00000000000000000000000000000000000ca112")
)

// ffBankSupplyUsei is the ABI encoding of IBank.supply("usei").
func ffBankSupplyUsei() []byte {
	data := append([]byte{}, crypto.Keccak256([]byte("supply(string)"))[:4]...)
	data = append(data, common.LeftPadBytes([]byte{0x20}, 32)...)
	data = append(data, common.LeftPadBytes([]byte{0x04}, 32)...)
	return append(data, common.RightPadBytes([]byte("usei"), 32)...)
}

// ffCallProgram increments slot0, calls target via op, then stores success (slot1) and RETURNDATASIZE (slot2).
func ffCallProgram(op vm.OpCode, target common.Address, input []byte) []byte {
	inLen := uint16(len(input)) //nolint:gosec // tiny test input.
	code := []byte{
		byte(vm.PUSH1), 0x00, byte(vm.SLOAD), byte(vm.PUSH1), 0x01, byte(vm.ADD),
		byte(vm.PUSH1), 0x00, byte(vm.SSTORE),
	}
	code = append(code, byte(vm.PUSH2), byte(inLen>>8), byte(inLen))
	offsetPos := len(code) + 1
	code = append(code, byte(vm.PUSH2), 0x00, 0x00, byte(vm.PUSH1), 0x00, byte(vm.CODECOPY))
	code = append(code, byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00)
	code = append(code, byte(vm.PUSH2), byte(inLen>>8), byte(inLen), byte(vm.PUSH1), 0x00)
	if op == vm.CALL || op == vm.CALLCODE {
		code = append(code, byte(vm.PUSH1), 0x00)
	}
	code = append(code, byte(vm.PUSH20))
	code = append(code, target.Bytes()...)
	code = append(code, byte(vm.GAS), byte(op))
	code = append(code, byte(vm.PUSH1), 0x01, byte(vm.SSTORE))
	code = append(code, byte(vm.RETURNDATASIZE), byte(vm.PUSH1), 0x02, byte(vm.SSTORE), byte(vm.STOP))
	dataOffset := uint16(len(code)) //nolint:gosec // bounded.
	code[offsetPos] = byte(dataOffset >> 8)
	code[offsetPos+1] = byte(dataOffset)
	return append(code, input...)
}

// ffCreateProgram deploys childInit via CREATE or CREATE2 (salt 0) and stores the address in slot3.
func ffCreateProgram(op vm.OpCode, childInit []byte) []byte {
	l := uint16(len(childInit)) //nolint:gosec // bounded.
	code := []byte{byte(vm.PUSH2), byte(l >> 8), byte(l)}
	offsetPos := len(code) + 1
	code = append(code, byte(vm.PUSH2), 0x00, 0x00, byte(vm.PUSH1), 0x00, byte(vm.CODECOPY))
	if op == vm.CREATE2 {
		code = append(code, byte(vm.PUSH1), 0x00)
	}
	code = append(code, byte(vm.PUSH2), byte(l>>8), byte(l), byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(op))
	code = append(code, byte(vm.PUSH1), 0x03, byte(vm.SSTORE), byte(vm.STOP))
	dataOffset := uint16(len(code)) //nolint:gosec // bounded.
	code[offsetPos] = byte(dataOffset >> 8)
	code[offsetPos+1] = byte(dataOffset)
	return append(code, childInit...)
}

type ffOutcome struct {
	ctx      *GigaTestContext
	results  []*abci.ExecTxResult
	receipt  *types.Receipt
	slots    [4]common.Hash
	nonce    uint64
	balance  *big.Int
	codeSize int
}

func TestGigaVsV2_FailFastPrecompileFallback_Behavior(t *testing.T) {
	input := ffBankSupplyUsei()
	childInit := ffCallProgram(vm.CALL, ffBankAddr, input)
	one := common.BigToHash(big.NewInt(1))
	thirtyTwo := common.BigToHash(big.NewInt(32))

	type tc struct {
		name   string
		code   map[common.Address][]byte
		to     *common.Address // nil => CREATE tx with data as init code
		data   []byte
		marker func(sender common.Address) common.Address
		// v2 outcome every mode must reproduce
		wantCode    uint32
		wantGasUsed int64
		wantStatus  uint32
		wantVMError string
		wantSlots   [3]common.Hash // slot0..slot2 at marker
	}
	cases := []tc{
		{
			name:        "direct tx to bank precompile",
			to:          &ffBankAddr,
			data:        input,
			marker:      func(common.Address) common.Address { return ffBankAddr },
			wantGasUsed: 23_904,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{},
		},
		{
			name:        "CALL",
			code:        map[common.Address][]byte{ffCallerAddr: ffCallProgram(vm.CALL, ffBankAddr, input)},
			to:          &ffCallerAddr,
			marker:      func(common.Address) common.Address { return ffCallerAddr },
			wantGasUsed: 92_360,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{one, one, thirtyTwo},
		},
		{
			name:        "STATICCALL",
			code:        map[common.Address][]byte{ffCallerAddr: ffCallProgram(vm.STATICCALL, ffBankAddr, input)},
			to:          &ffCallerAddr,
			marker:      func(common.Address) common.Address { return ffCallerAddr },
			wantGasUsed: 92_357,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{one, one, thirtyTwo},
		},
		{
			name:        "DELEGATECALL",
			code:        map[common.Address][]byte{ffCallerAddr: ffCallProgram(vm.DELEGATECALL, ffBankAddr, input)},
			to:          &ffCallerAddr,
			marker:      func(common.Address) common.Address { return ffCallerAddr },
			wantGasUsed: 92_357,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{one, one, thirtyTwo},
		},
		{
			name:        "CALLCODE",
			code:        map[common.Address][]byte{ffCallerAddr: ffCallProgram(vm.CALLCODE, ffBankAddr, input)},
			to:          &ffCallerAddr,
			marker:      func(common.Address) common.Address { return ffCallerAddr },
			wantGasUsed: 92_360,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{one, one, thirtyTwo},
		},
		{
			name: "nested CALL through intermediate contract",
			code: map[common.Address][]byte{
				ffOuterAddr:  ffCallProgram(vm.CALL, ffCallerAddr, nil),
				ffCallerAddr: ffCallProgram(vm.CALL, ffBankAddr, input),
			},
			to:          &ffOuterAddr,
			marker:      func(common.Address) common.Address { return ffCallerAddr },
			wantGasUsed: 141_412,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{one, one, thirtyTwo},
		},
		{
			name:        "CREATE tx constructor",
			data:        childInit,
			marker:      func(sender common.Address) common.Address { return crypto.CreateAddress(sender, 0) },
			wantGasUsed: 125_502,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{one, one, thirtyTwo},
		},
		{
			name:        "CREATE opcode constructor",
			code:        map[common.Address][]byte{ffFactoryAddr: ffCreateProgram(vm.CREATE, childInit)},
			to:          &ffFactoryAddr,
			marker:      func(common.Address) common.Address { return crypto.CreateAddress(ffFactoryAddr, 0) },
			wantGasUsed: 146_524,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{one, one, thirtyTwo},
		},
		{
			name: "CREATE2 opcode constructor",
			code: map[common.Address][]byte{ffFactoryAddr: ffCreateProgram(vm.CREATE2, childInit)},
			to:   &ffFactoryAddr,
			marker: func(common.Address) common.Address {
				return crypto.CreateAddress2(ffFactoryAddr, common.Hash{}, crypto.Keccak256(childInit))
			},
			wantGasUsed: 146_557,
			wantStatus:  1,
			wantSlots:   [3]common.Hash{one, one, thirtyTwo},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			blockTime := time.Now()
			accts := utils.NewTestAccounts(3)
			signer := utils.NewSigner()
			marker := c.marker(signer.EvmAddress)

			run := func(mode ExecutorMode) ffOutcome {
				tCtx := NewGigaTestContext(t, accts, blockTime, 1, mode)
				for addr, code := range c.code {
					tCtx.TestApp.EvmKeeper.SetCode(tCtx.Ctx, addr, code)
				}
				var txs [][]byte
				if c.to == nil {
					txs = CreateContractDeployTxs(t, tCtx, []EVMContractDeploy{{Signer: signer, Bytecode: c.data, Nonce: 0}}, mode != ModeV2withOCC)
				} else {
					txs = CreateContractCallTxs(t, tCtx, []EVMContractCall{{Signer: signer, Contract: *c.to, Data: c.data, Nonce: 0}}, mode != ModeV2withOCC)
				}
				_, results, err := RunBlock(t, tCtx, txs)
				require.NoError(t, err)
				require.Len(t, results, 1)
				out := ffOutcome{ctx: tCtx, results: results}
				require.NotNil(t, results[0].EvmTxInfo, "%s: log=%s", mode, results[0].Log)
				receipt, rerr := tCtx.GetTransientReceipt(t, common.HexToHash(results[0].EvmTxInfo.TxHash), 0)
				require.NoError(t, rerr)
				out.receipt = receipt
				for i := range out.slots {
					out.slots[i] = tCtx.TestApp.EvmKeeper.GetState(tCtx.Ctx, marker, common.BigToHash(big.NewInt(int64(i))))
				}
				out.nonce = tCtx.TestApp.EvmKeeper.GetNonce(tCtx.Ctx, signer.EvmAddress)
				usei := tCtx.TestApp.BankKeeper.GetBalance(tCtx.Ctx, signer.AccountAddress, "usei").Amount
				wei := tCtx.TestApp.BankKeeper.GetWeiBalance(tCtx.Ctx, signer.AccountAddress)
				out.balance = new(big.Int).Add(new(big.Int).Mul(usei.BigInt(), evmstate.UseiToSweiMultiplier), wei.BigInt())
				out.codeSize = len(tCtx.TestApp.EvmKeeper.GetCode(tCtx.Ctx, marker))
				return out
			}

			v2 := run(ModeV2withOCC)
			r := v2.results[0]
			require.Equal(t, c.wantCode, r.Code)
			require.Equal(t, c.wantGasUsed, r.GasUsed)
			require.Equal(t, c.wantStatus, v2.receipt.Status)
			require.Equal(t, c.wantVMError, v2.receipt.VmError)
			require.Equal(t, c.wantSlots[0], v2.slots[0])
			require.Equal(t, c.wantSlots[1], v2.slots[1])
			require.Equal(t, c.wantSlots[2], v2.slots[2])
			require.Equal(t, uint64(1), v2.nonce)

			// Giga modes must match v2 with no leaked giga partial state.
			for _, mode := range []ExecutorMode{ModeGigaSequential, ModeGigaOCC} {
				got := run(mode)
				name := c.name + "/" + mode.String()
				CompareDeterministicFields(t, name, v2.results, got.results)
				CompareLastResultsHash(t, name, v2.results, got.results)
				assertReceiptsEqual(t, name, 0, v2.ctx.Mode, got.ctx.Mode, v2.receipt, got.receipt)
				require.Equal(t, v2.results[0].Log, got.results[0].Log, name)
				require.Equal(t, v2.slots, got.slots, name)
				require.Equal(t, v2.nonce, got.nonce, name)
				require.Equal(t, 0, v2.balance.Cmp(got.balance), "%s: sender balance v2=%s giga=%s", name, v2.balance, got.balance)
				require.Equal(t, v2.codeSize, got.codeSize, name)
			}
		})
	}
}

type sdOutcome struct {
	ctx         *GigaTestContext
	results     []*abci.ExecTxResult
	receipt     *types.Receipt
	codeSize    int
	slot0       common.Hash
	contractBal *big.Int
	benefBal    *big.Int
	nonce       uint64
}

// TestGigaVsV2_SelfDestructFallback_Behavior pins SELFDESTRUCT falling back from giga to v2.
func TestGigaVsV2_SelfDestructFallback_Behavior(t *testing.T) {
	beneficiary := common.HexToAddress("0x00000000000000000000000000000000000bef01")
	preexisting := common.HexToAddress("0x00000000000000000000000000000000000de501")
	prefund := new(big.Int).Mul(big.NewInt(7), evmstate.UseiToSweiMultiplier) // 7 usei
	one := common.BigToHash(big.NewInt(1))

	selfDestructTo := func(b common.Address) []byte {
		return append(append([]byte{byte(vm.PUSH20)}, b.Bytes()...), byte(vm.SELFDESTRUCT))
	}
	// SSTORE(0,1) then SELFDESTRUCT(target).
	storeThenDestruct := func(target []byte) []byte {
		code := []byte{byte(vm.PUSH1), 0x01, byte(vm.PUSH1), 0x00, byte(vm.SSTORE)}
		return append(code, target...)
	}

	cases := []struct {
		name         string
		preexisting  bool
		initCode     []byte
		benef        func(contract common.Address) common.Address
		wantGasUsed  int64
		wantCodeSize int
		wantSlot0    common.Hash
		wantContract *big.Int
		wantBenef    *big.Int
	}{
		{
			name:         "pre-existing contract only moves balance",
			preexisting:  true,
			benef:        func(common.Address) common.Address { return beneficiary },
			wantGasUsed:  53_603,
			wantCodeSize: 22,
			wantSlot0:    one,
			wantContract: big.NewInt(0),
			wantBenef:    prefund,
		},
		{
			name:         "prefunded address created and destroyed in same tx",
			initCode:     storeThenDestruct(selfDestructTo(beneficiary)),
			benef:        func(common.Address) common.Address { return beneficiary },
			wantGasUsed:  107_927,
			wantCodeSize: 0,
			wantSlot0:    common.Hash{},
			wantContract: big.NewInt(0),
			wantBenef:    prefund,
		},
		{
			name:         "same-tx created contract destructs to itself",
			initCode:     storeThenDestruct([]byte{byte(vm.ADDRESS), byte(vm.SELFDESTRUCT)}),
			benef:        func(c common.Address) common.Address { return c },
			wantGasUsed:  80_210,
			wantCodeSize: 0,
			wantSlot0:    common.Hash{},
			wantContract: big.NewInt(0),
			wantBenef:    big.NewInt(0),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			blockTime := time.Now()
			accts := utils.NewTestAccounts(3)
			signer := utils.NewSigner()
			contract := preexisting
			if !c.preexisting {
				contract = crypto.CreateAddress(signer.EvmAddress, 0)
			}
			benef := c.benef(contract)

			run := func(mode ExecutorMode) sdOutcome {
				tCtx := NewGigaTestContext(t, accts, blockTime, 1, mode)
				ek := &tCtx.TestApp.EvmKeeper
				fundAccount(t, tCtx, ek.GetSeiAddressOrDefault(tCtx.Ctx, contract), prefund)
				var txs [][]byte
				if c.preexisting {
					ek.SetCode(tCtx.Ctx, contract, selfDestructTo(benef))
					ek.SetState(tCtx.Ctx, contract, common.Hash{}, one)
					txs = CreateContractCallTxs(t, tCtx, []EVMContractCall{{Signer: signer, Contract: contract, Nonce: 0}}, mode != ModeV2withOCC)
				} else {
					txs = CreateContractDeployTxs(t, tCtx, []EVMContractDeploy{{Signer: signer, Bytecode: c.initCode, Nonce: 0}}, mode != ModeV2withOCC)
				}
				_, results, err := RunBlock(t, tCtx, txs)
				require.NoError(t, err)
				require.Len(t, results, 1)
				require.NotNil(t, results[0].EvmTxInfo, "%s: log=%s", mode, results[0].Log)
				receipt, rerr := tCtx.GetTransientReceipt(t, common.HexToHash(results[0].EvmTxInfo.TxHash), 0)
				require.NoError(t, rerr)
				return sdOutcome{
					ctx:         tCtx,
					results:     results,
					receipt:     receipt,
					codeSize:    len(ek.GetCode(tCtx.Ctx, contract)),
					slot0:       ek.GetState(tCtx.Ctx, contract, common.Hash{}),
					contractBal: ek.GetBalance(tCtx.Ctx, ek.GetSeiAddressOrDefault(tCtx.Ctx, contract)),
					benefBal:    ek.GetBalance(tCtx.Ctx, ek.GetSeiAddressOrDefault(tCtx.Ctx, benef)),
					nonce:       ek.GetNonce(tCtx.Ctx, signer.EvmAddress),
				}
			}

			v2 := run(ModeV2withOCC)
			require.Equal(t, uint32(0), v2.results[0].Code)
			require.Equal(t, uint32(1), v2.receipt.Status)
			require.Equal(t, c.wantGasUsed, v2.results[0].GasUsed)
			require.Equal(t, c.wantCodeSize, v2.codeSize)
			require.Equal(t, c.wantSlot0, v2.slot0)
			require.Equal(t, 0, c.wantContract.Cmp(v2.contractBal), "contract balance %s", v2.contractBal)
			if benef != contract {
				require.Equal(t, 0, c.wantBenef.Cmp(v2.benefBal), "beneficiary balance %s", v2.benefBal)
			}
			require.Equal(t, uint64(1), v2.nonce)

			for _, mode := range []ExecutorMode{ModeGigaSequential, ModeGigaOCC} {
				got := run(mode)
				name := c.name + "/" + mode.String()
				CompareDeterministicFields(t, name, v2.results, got.results)
				CompareLastResultsHash(t, name, v2.results, got.results)
				assertReceiptsEqual(t, name, 0, v2.ctx.Mode, got.ctx.Mode, v2.receipt, got.receipt)
				require.Equal(t, v2.codeSize, got.codeSize, name)
				require.Equal(t, v2.slot0, got.slot0, name)
				require.Equal(t, 0, v2.contractBal.Cmp(got.contractBal), name)
				require.Equal(t, 0, v2.benefBal.Cmp(got.benefBal), name)
				require.Equal(t, v2.nonce, got.nonce, name)
			}
		})
	}
}
