// Package kvrepairfixture seeds and damages EVM state on a disposable test
// chain, so that a kvrepair file can be exported from a reserve and applied end
// to end while the FlatKV migration is still running. It is a test fixture and
// must not be merged.
package kvrepairfixture

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cast"

	"github.com/sei-protocol/sei-chain/app/migration"
	"github.com/sei-protocol/sei-chain/app/upgrades"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

const (
	// ChainID is the only chain the fixture handlers run on.
	ChainID = "harbor-a8-kvrepair-v67"
	// SeedHeight is where every node writes the seeded state.
	SeedHeight = 30
	// MigrationStartHeight is where every node starts the FlatKV migration. It
	// is after SeedHeight, because a key written while the migration runs and
	// absent from memiavl goes straight to FlatKV.
	MigrationStartHeight = 60
	// DamageHeight is where nodes with FlagDamage set damage the seeded state.
	DamageHeight = 300
	// FlagDamage is the app.toml key that enables the damage handler.
	FlagDamage = "kvrepair-fixture.damage"
	// NumKeysToMigratePerBlock is the migration rate the seed handler sets. It
	// is low so that the migration is still running when the repair applies.
	NumKeysToMigratePerBlock uint64 = 2
	// SeededSlots is the number of storage slots the seed handler writes.
	SeededSlots = 2000
	// ExtraSlot is a slot that only the damage handler writes.
	ExtraSlot = 5000
	// SeededNonce and SeededZeroedNonce are the nonces the seed handler sets.
	SeededNonce       = 5
	SeededZeroedNonce = 3
)

var (
	// Contract is the address whose storage the fixture writes.
	Contract = common.HexToAddress("0x00000000000000000000000000000000000000a8")
	// CodeContract is the address whose code the fixture writes.
	CodeContract = common.HexToAddress("0x00000000000000000000000000000000000000a9")
	// NonceAccount is an account whose nonce the damage handler changes.
	NonceAccount = common.HexToAddress("0x00000000000000000000000000000000000000aa")
	// ZeroedNonceAccount is an account whose nonce the damage handler sets to 0.
	ZeroedNonceAccount = common.HexToAddress("0x00000000000000000000000000000000000000ab")

	// Sentinel is the storage value the damage handler writes.
	Sentinel = common.HexToHash("0xdead00000000000000000000000000000000000000000000000000000000dead")
	// SeededCode and DamagedCode are the bytecode of CodeContract before and
	// after the damage.
	SeededCode  = []byte{0x60, 0x01, 0x60, 0x00, 0x55}
	DamagedCode = []byte{0x60, 0xff}
)

// Slot returns the storage key of slot i.
func Slot(i int64) common.Hash { return common.BigToHash(big.NewInt(i)) }

// SeededValue returns the value the seed handler writes to slot i.
func SeededValue(i int64) common.Hash { return common.BigToHash(big.NewInt(0xa8_0000 + i)) }

// DamagedSlots lists the slots the damage handler changes or deletes: two at
// the start of the keyspace, which the migration has moved by DamageHeight, and
// two at the end, which it has not.
var DamagedSlots = struct{ Set, Delete []int64 }{
	Set:    []int64{0, SeededSlots - 10},
	Delete: []int64{1, SeededSlots - 5},
}

// Register adds the seed handler, and the damage handler when FlagDamage is set.
func Register(m *upgrades.HardForkManager, k *evmkeeper.Keeper, pk paramskeeper.Keeper, appOpts servertypes.AppOptions) {
	m.RegisterHandler(handler{name: "kvrepair-fixture-seed", height: SeedHeight, run: func(ctx sdk.Context) error {
		for i := int64(0); i < SeededSlots; i++ {
			k.SetState(ctx, Contract, Slot(i), SeededValue(i))
		}
		k.SetCode(ctx, CodeContract, SeededCode)
		k.SetNonce(ctx, NonceAccount, SeededNonce)
		k.SetNonce(ctx, ZeroedNonceAccount, SeededZeroedNonce)
		return nil
	}})
	m.RegisterHandler(handler{name: "kvrepair-fixture-migrate", height: MigrationStartHeight, run: func(ctx sdk.Context) error {
		subspace, _ := pk.GetSubspace(migration.SubspaceName)
		subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, NumKeysToMigratePerBlock)
		return nil
	}})
	if !cast.ToBool(appOpts.Get(FlagDamage)) {
		return
	}
	m.RegisterHandler(handler{name: "kvrepair-fixture-damage", height: DamageHeight, run: func(ctx sdk.Context) error {
		for _, i := range DamagedSlots.Set {
			k.SetState(ctx, Contract, Slot(i), Sentinel)
		}
		for _, i := range DamagedSlots.Delete {
			k.SetState(ctx, Contract, Slot(i), common.Hash{})
		}
		k.SetState(ctx, Contract, Slot(ExtraSlot), Sentinel)
		k.SetCode(ctx, CodeContract, DamagedCode)
		k.SetNonce(ctx, NonceAccount, SeededNonce+4)
		k.SetNonce(ctx, ZeroedNonceAccount, 0)
		return nil
	}})
}

type handler struct {
	name   string
	height int64
	run    func(sdk.Context) error
}

func (h handler) GetName() string                      { return h.name }
func (h handler) GetTargetChainID() string             { return ChainID }
func (h handler) GetTargetHeight() int64               { return h.height }
func (h handler) ExecuteHandler(ctx sdk.Context) error { return h.run(ctx) }
