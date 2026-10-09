package state_test

// Behavior tests pinning state.GetDBImpl and tracing hooks on a hooked-StateDB-wrapped DBImpl.

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// hookCounts records how many times each hook fired.
type hookCounts struct {
	balance, nonce, code, storage, log int
	lastBalancePrev, lastBalanceNew    *big.Int
	lastNonceReason                    tracing.NonceChangeReason
}

func (c *hookCounts) hooks() *tracing.Hooks {
	return &tracing.Hooks{
		OnBalanceChange: func(_ common.Address, prev, next *big.Int, _ tracing.BalanceChangeReason) {
			c.balance++
			c.lastBalancePrev, c.lastBalanceNew = prev, next
		},
		OnNonceChangeV2: func(_ common.Address, _, _ uint64, reason tracing.NonceChangeReason) {
			c.nonce++
			c.lastNonceReason = reason
		},
		OnCodeChange: func(common.Address, common.Hash, []byte, common.Hash, []byte) { c.code++ },
		OnStorageChange: func(common.Address, common.Hash, common.Hash, common.Hash) {
			c.storage++
		},
	}
}

// newHookedDBImpl wraps a StateDB in go-ethereum's hooked StateDB.
func newHookedDBImpl(db vm.StateDB, hooks *tracing.Hooks) vm.StateDB {
	return gethstate.NewHookedState(db, hooks)
}

func newBehaviorDBImpl(t *testing.T) (*state.DBImpl, common.Address) {
	t.Helper()
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	db := state.NewDBImpl(ctx, k, false)
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(db.Ctx(), seiAddr, evmAddr)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(20)))
	require.NoError(t, k.BankKeeper().MintCoins(db.Ctx(), types.ModuleName, amt))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(db.Ctx(), types.ModuleName, seiAddr, amt))
	return db, evmAddr
}

func TestBehaviorGetDBImplUnwrapsHookedState(t *testing.T) {
	db, _ := newBehaviorDBImpl(t)

	require.Same(t, db, state.GetDBImpl(db))

	hooked := newHookedDBImpl(db, &tracing.Hooks{})
	require.Same(t, db, state.GetDBImpl(hooked))

	// nested wrapping is unwrapped recursively
	require.Same(t, db, state.GetDBImpl(newHookedDBImpl(hooked, nil)))

	// nil hooks are accepted
	require.Same(t, db, state.GetDBImpl(newHookedDBImpl(db, nil)))

	// anything else yields nil
	require.Nil(t, state.GetDBImpl(nil))
}

// Wrapper-only hooks fire for balance/nonce/code/storage changes.
func TestBehaviorHookedDBImplWrapperHooksOnly(t *testing.T) {
	db, addr := newBehaviorDBImpl(t)
	c := &hookCounts{}
	sdb := newHookedDBImpl(db, c.hooks())

	sdb.AddBalance(addr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeTransfer)
	sdb.SubBalance(addr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeTransfer)
	require.Equal(t, 2, c.balance) // go-ethereum v1.17.7 fires OnBalanceChange; the old fork did not

	sdb.SetNonce(addr, 7, tracing.NonceChangeEoACall)
	require.Equal(t, 1, c.nonce)
	require.Equal(t, tracing.NonceChangeEoACall, c.lastNonceReason)

	sdb.SetCode(addr, []byte{0x60, 0x00}, tracing.CodeChangeUnspecified)
	require.Equal(t, 1, c.code)

	key, val := common.HexToHash("0x01"), common.HexToHash("0x02")
	sdb.SetState(addr, key, val)
	require.Equal(t, 1, c.storage)
	// unchanged value: no storage hook from the wrapper
	sdb.SetState(addr, key, val)
	require.Equal(t, 1, c.storage)

	require.Nil(t, db.Err())
	require.Equal(t, uint64(7), db.GetNonce(addr))
	require.Equal(t, val, db.GetState(addr, key))
}

// Hooks on both DBImpl and wrapper fire every balance/nonce/code/storage change twice.
func TestBehaviorHookedDBImplWithDBImplLogger(t *testing.T) {
	db, addr := newBehaviorDBImpl(t)
	c := &hookCounts{}
	hooks := c.hooks()
	db.SetLogger(hooks)
	sdb := newHookedDBImpl(db, hooks)

	prev := sdb.AddBalance(addr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeTransfer)
	require.Equal(t, *uint256.NewInt(20_000_000_000_000), prev)
	require.Equal(t, 2, c.balance)
	require.Equal(t, big.NewInt(20_000_000_000_000), c.lastBalancePrev)
	require.Equal(t, big.NewInt(21_000_000_000_000), c.lastBalanceNew)
	prev = sdb.SubBalance(addr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeTransfer)
	require.Equal(t, *uint256.NewInt(21_000_000_000_000), prev)
	require.Equal(t, 4, c.balance)
	require.Equal(t, big.NewInt(21_000_000_000_000), c.lastBalancePrev)
	require.Equal(t, big.NewInt(20_000_000_000_000), c.lastBalanceNew)

	sdb.SetNonce(addr, 3, tracing.NonceChangeEoACall)
	require.Equal(t, 2, c.nonce)

	sdb.SetCode(addr, []byte{0x60, 0x00}, tracing.CodeChangeUnspecified)
	require.Equal(t, 2, c.code)

	sdb.SetState(addr, common.HexToHash("0x01"), common.HexToHash("0x02"))
	require.Equal(t, 2, c.storage)

	// DBImpl alone fires each hook once
	c2 := &hookCounts{}
	db.SetLogger(c2.hooks())
	db.AddBalance(addr, uint256.NewInt(1_000_000_000_000), tracing.BalanceChangeTransfer)
	db.SetNonce(addr, 4, tracing.NonceChangeEoACall)
	db.SetCode(addr, []byte{0x60, 0x01}, tracing.CodeChangeUnspecified)
	db.SetState(addr, common.HexToHash("0x01"), common.HexToHash("0x03"))
	require.Equal(t, hookCounts{balance: 1, nonce: 1, code: 1, storage: 1,
		lastBalancePrev: big.NewInt(20_000_000_000_000), lastBalanceNew: big.NewInt(21_000_000_000_000),
		lastNonceReason: tracing.NonceChangeEoACall}, *c2)
	require.Nil(t, db.Err())
}
