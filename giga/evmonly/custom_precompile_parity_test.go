package evmonly

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Inputs understood by bankPrecompile. Every call except bankRead first
// increments the caller's own counter.
const (
	bankRead           byte = 0x00 // return the caller's counter
	bankOwn            byte = 0x01 // log the new counter
	bankShared         byte = 0x02 // also increment the shared counter
	bankFail           byte = 0x03 // then fail
	bankPay            byte = 0x04 // then pay the 20-byte address that follows 1 wei
	bankRevert         byte = 0x05 // then revert with the counter as data
	bankSnapshotCaller byte = 0x06 // then store the caller's balance
)

var (
	bankSharedSlot = common.Hash{0xee}
	bankOps        = []byte{bankRead, bankOwn, bankShared, bankFail, bankPay, bankRevert, bankSnapshotCaller}
)

// bankPrecompile keeps a counter per caller, a shared counter, and a snapshot
// of each caller's balance in its own storage, and can pay out of its balance.
type bankPrecompile struct{}

func (bankPrecompile) RequiredGas(input []byte) uint64 {
	return 1_000 + 10*uint64(len(input))
}

func (bankPrecompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	own := bankCounterSlot(ctx.Caller)
	if len(input) == 0 || input[0] == bankRead {
		return ctx.State.GetState(ctx.Address, own).Bytes(), nil
	}
	counter := bankIncrement(ctx, own)
	switch input[0] {
	case bankShared:
		bankIncrement(ctx, bankSharedSlot)
	case bankFail:
		return nil, errProbeFailed
	case bankPay:
		if len(input) < 1+common.AddressLength {
			return nil, errProbeFailed
		}
		if err := ctx.State.SubBalance(ctx.Address, big.NewInt(1)); err != nil {
			return nil, err
		}
		ctx.State.AddBalance(common.BytesToAddress(input[1:1+common.AddressLength]), big.NewInt(1))
	case bankRevert:
		return counter.Bytes(), vm.ErrExecutionReverted
	case bankSnapshotCaller:
		ctx.State.SetState(ctx.Address, bankBalanceSlot(ctx.Caller), common.BigToHash(ctx.State.GetBalance(ctx.Caller)))
	}
	ctx.Logs.AddLog(&ethtypes.Log{Address: ctx.Address, Topics: []common.Hash{own}, Data: counter.Bytes()})
	return counter.Bytes(), nil
}

func bankIncrement(ctx *precompiles.Context, slot common.Hash) common.Hash {
	next := common.BigToHash(new(big.Int).Add(ctx.State.GetState(ctx.Address, slot).Big(), big.NewInt(1)))
	ctx.State.SetState(ctx.Address, slot, next)
	return next
}

func bankCounterSlot(addr common.Address) common.Hash {
	return common.BytesToHash(addr.Bytes())
}

func bankBalanceSlot(addr common.Address) common.Hash {
	slot := bankCounterSlot(addr)
	slot[0] = 0xbb
	return slot
}

var bankRegistry = registryOf{probeAddr: bankPrecompile{}}

func bankPrecompileMap() map[common.Address]vm.PrecompiledContract {
	return customPrecompileMap(bankRegistry)
}

// bankProxy is a precompileProxyRuntime contract in front of the bank precompile.
type bankProxy struct {
	addr        common.Address
	op          vm.OpCode
	revertAfter bool
}

func bankProxies() []bankProxy {
	var proxies []bankProxy
	for _, op := range []vm.OpCode{vm.CALL, vm.CALLCODE, vm.DELEGATECALL, vm.STATICCALL} {
		for _, revertAfter := range []bool{false, true} {
			proxies = append(proxies, bankProxy{
				addr:        common.BigToAddress(big.NewInt(int64(0x2000 + len(proxies)))),
				op:          op,
				revertAfter: revertAfter,
			})
		}
	}
	return proxies
}

// bankWorld is the state a bank scenario runs against.
type bankWorld struct {
	users   []testAccount
	proxies []bankProxy
	nonces  []uint64
}

func newBankWorld(t *testing.T, users int, states ...*MemoryState) *bankWorld {
	t.Helper()
	world := &bankWorld{proxies: bankProxies(), nonces: make([]uint64, users)}
	for range users {
		world.users = append(world.users, newTestAccount(t, states...))
	}
	for _, state := range states {
		state.SetBalance(probeAddr, big.NewInt(1_000))
		for _, proxy := range world.proxies {
			state.SetCode(proxy.addr, precompileProxyRuntime(proxy.op, probeAddr, 1, proxy.revertAfter))
			state.SetBalance(proxy.addr, big.NewInt(1_000))
		}
	}
	return world
}

func (w *bankWorld) tx(t *testing.T, user int, to common.Address, value int64, data []byte) []byte {
	t.Helper()
	nonce := w.nonces[user]
	w.nonces[user]++
	return signLegacyTxWithGasPrice(t, w.users[user].key, big.NewInt(testChainID), nonce, &to, big.NewInt(value), data, 2_000_000, big.NewInt(1))
}

// addresses returns every account a bank scenario can touch.
func (w *bankWorld) addresses() []common.Address {
	addrs := []common.Address{probeAddr, blockContext(big.NewInt(testChainID)).Coinbase}
	for _, user := range w.users {
		addrs = append(addrs, user.addr)
	}
	for _, proxy := range w.proxies {
		addrs = append(addrs, proxy.addr)
	}
	return addrs
}

// slots returns every storage slot a bank scenario can write.
func (w *bankWorld) slots() []common.Hash {
	slots := []common.Hash{bankSharedSlot, proxySuccessSlot, proxyReturnSizeSlot, proxyReturnSlot}
	for _, addr := range w.addresses() {
		slots = append(slots, bankCounterSlot(addr), bankBalanceSlot(addr))
	}
	return slots
}

func requireBankStateParity(t *testing.T, world *bankWorld, state *MemoryState, gethResult *gethReferenceResult) {
	t.Helper()
	slots := world.slots()
	for _, addr := range world.addresses() {
		requireAddressParity(t, state, gethResult.state, addr, slots...)
	}
}

func requireSameMemoryState(t *testing.T, world *bankWorld, want, got *MemoryState) {
	t.Helper()
	for _, addr := range world.addresses() {
		require.Equal(t, want.GetBalance(addr), got.GetBalance(addr), "balance of %s", addr)
		require.Equal(t, want.GetNonce(addr), got.GetNonce(addr), "nonce of %s", addr)
		for _, slot := range world.slots() {
			require.Equal(t, want.GetState(addr, slot), got.GetState(addr, slot), "slot %s of %s", slot, addr)
		}
	}
}

func bankInput(op byte, payee common.Address) []byte {
	if op == bankPay {
		return append([]byte{op}, payee.Bytes()...)
	}
	return []byte{op}
}

func TestCustomPrecompileMatchesGethStateDB(t *testing.T) {
	cfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: bankRegistry}
	ctx := blockContext(big.NewInt(testChainID))
	proxies := bankProxies()
	type scenario struct {
		name  string
		build func(t *testing.T, w *bankWorld) [][]byte
	}
	scenarios := []scenario{
		{name: "first call materializes the account", build: func(t *testing.T, w *bankWorld) [][]byte {
			return [][]byte{w.tx(t, 0, probeAddr, 0, []byte{bankOwn}), w.tx(t, 0, probeAddr, 0, []byte{bankOwn})}
		}},
		{name: "value sent with the call", build: func(t *testing.T, w *bankWorld) [][]byte {
			return [][]byte{w.tx(t, 0, probeAddr, 9, []byte{bankShared}), w.tx(t, 1, probeAddr, 3, []byte{bankRead})}
		}},
		{name: "payouts until the balance is exhausted", build: func(t *testing.T, w *bankWorld) [][]byte {
			return [][]byte{
				w.tx(t, 0, probeAddr, 0, bankInput(bankPay, w.users[1].addr)),
				w.tx(t, 1, probeAddr, 0, bankInput(bankPay, w.users[0].addr)),
				w.tx(t, 0, probeAddr, 0, bankInput(bankSnapshotCaller, common.Address{})),
			}
		}},
		{name: "failures and reverts", build: func(t *testing.T, w *bankWorld) [][]byte {
			return [][]byte{
				w.tx(t, 0, probeAddr, 5, []byte{bankFail}),
				w.tx(t, 0, probeAddr, 5, []byte{bankRevert}),
				w.tx(t, 0, probeAddr, 0, []byte{bankOwn}),
			}
		}},
	}
	for _, op := range bankOps {
		for _, proxy := range proxies {
			scenarios = append(scenarios, scenario{
				name: fmt.Sprintf("op %#x through %s revert=%v", op, proxy.op, proxy.revertAfter),
				build: func(t *testing.T, w *bankWorld) [][]byte {
					return [][]byte{
						w.tx(t, 0, proxy.addr, 0, bankInput(op, w.users[1].addr)),
						w.tx(t, 1, proxy.addr, 0, bankInput(op, w.users[0].addr)),
					}
				},
			})
		}
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			state := NewMemoryState()
			world := newBankWorld(t, 2, state)
			rawTxs := sc.build(t, world)

			gethResult, err := executeGethReferenceBlockWithPrecompiles(t, state, cfg, ctx, rawTxs, bankPrecompileMap())
			require.NoError(t, err)
			execResult, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: ctx, Txs: rawTxs})
			require.NoError(t, err)
			state.ApplyChangeSet(execResult.ChangeSet)

			requireExecutionParity(t, execResult, gethResult)
			requireBankStateParity(t, world, state, gethResult)
		})
	}
}

// randomBankBlock returns txCount random transfers and bank calls, direct and
// through proxies, from world's users.
func randomBankBlock(t *testing.T, rng *rand.Rand, world *bankWorld, txCount int) [][]byte {
	t.Helper()
	targets := world.addresses()
	rawTxs := make([][]byte, 0, txCount)
	for range txCount {
		user := rng.IntN(len(world.users))
		payee := targets[rng.IntN(len(targets))]
		op := bankOps[rng.IntN(len(bankOps))]
		switch rng.IntN(3) {
		case 0:
			rawTxs = append(rawTxs, world.tx(t, user, payee, rng.Int64N(100)+1, nil))
		case 1:
			rawTxs = append(rawTxs, world.tx(t, user, probeAddr, rng.Int64N(2), bankInput(op, payee)))
		default:
			proxy := world.proxies[rng.IntN(len(world.proxies))]
			rawTxs = append(rawTxs, world.tx(t, user, proxy.addr, 0, bankInput(op, payee)))
		}
	}
	return rawTxs
}

func TestCustomPrecompileRandomBlocksOCCMatchesSequentialAndGeth(t *testing.T) {
	const (
		seeds         = 24
		blocksPerSeed = 3
		txsPerBlock   = 32
		usersPerWorld = 6
	)
	ctx := blockContext(big.NewInt(testChainID))
	seqCfg := Config{MinGasPrice: big.NewInt(0), CustomPrecompiles: bankRegistry}
	for seed := range uint64(seeds) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
			occCfg := seqCfg
			occCfg.OCCWorkers = []int{2, 4, 8}[seed%3]
			seqState := NewMemoryState()
			occState := NewMemoryState()
			world := newBankWorld(t, usersPerWorld, seqState, occState)

			for block := range blocksPerSeed {
				rawTxs := randomBankBlock(t, rng, world, txsPerBlock)
				req := BlockRequest{Context: ctx, Txs: rawTxs}
				gethResult, err := executeGethReferenceBlockWithPrecompiles(t, seqState, seqCfg, ctx, rawTxs, bankPrecompileMap())
				require.NoError(t, err)
				seqResult, err := NewExecutor(seqCfg, withTestState(seqState)).ExecuteBlock(t.Context(), req)
				require.NoError(t, err)
				occResult, err := NewExecutor(occCfg, withTestState(occState)).ExecuteBlock(t.Context(), req)
				require.NoError(t, err, "block %d", block)
				requireOCCRan(t, occCfg, occResult)

				requireExecutionParity(t, seqResult, gethResult)
				requireExecutionParity(t, occResult, gethResult)
				seqState.ApplyChangeSet(seqResult.ChangeSet)
				occState.ApplyChangeSet(occResult.ChangeSet)
				requireBankStateParity(t, world, seqState, gethResult)
				requireSameMemoryState(t, world, seqState, occState)
			}
		})
	}
}
