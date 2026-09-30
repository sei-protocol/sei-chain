// Package kvrepairfixture seeds and damages EVM storage on a disposable test
// chain, so that a kvrepair file can be exported from a reserve and applied end
// to end. It is a test fixture and must not be merged.
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
	ChainID = "harbor-a8-kvrepair"
	// SeedHeight is where every node writes the seeded slots and starts the
	// FlatKV migration.
	SeedHeight = 30
	// DamageHeight is where nodes with FlagDamage set damage the seeded slots.
	DamageHeight = 1500
	// FlagDamage is the app.toml key that enables the damage handler.
	FlagDamage = "kvrepair-fixture.damage"
	// NumKeysToMigratePerBlock is the migration rate the seed handler sets.
	NumKeysToMigratePerBlock uint64 = 1000
	// SeededSlots is the number of storage slots the seed handler writes.
	SeededSlots = 8
	// ExtraSlot is a slot that only the damage handler writes.
	ExtraSlot = 100
)

// Contract is the address whose storage the fixture writes.
var Contract = common.HexToAddress("0x00000000000000000000000000000000000000a8")

// Sentinel is the value the damage handler writes.
var Sentinel = common.HexToHash("0xdead00000000000000000000000000000000000000000000000000000000dead")

// Slot returns the storage key of slot i.
func Slot(i int64) common.Hash { return common.BigToHash(big.NewInt(i)) }

// SeededValue returns the value the seed handler writes to slot i.
func SeededValue(i int64) common.Hash { return common.BigToHash(big.NewInt(0xa8_0000 + i)) }

// Register adds the seed handler, and the damage handler when FlagDamage is set.
func Register(m *upgrades.HardForkManager, k *evmkeeper.Keeper, pk paramskeeper.Keeper, appOpts servertypes.AppOptions) {
	m.RegisterHandler(handler{name: "kvrepair-fixture-seed", height: SeedHeight, run: func(ctx sdk.Context) error {
		for i := int64(0); i < SeededSlots; i++ {
			k.SetState(ctx, Contract, Slot(i), SeededValue(i))
		}
		subspace, _ := pk.GetSubspace(migration.SubspaceName)
		subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, NumKeysToMigratePerBlock)
		return nil
	}})
	if !cast.ToBool(appOpts.Get(FlagDamage)) {
		return
	}
	m.RegisterHandler(handler{name: "kvrepair-fixture-damage", height: DamageHeight, run: func(ctx sdk.Context) error {
		k.SetState(ctx, Contract, Slot(0), Sentinel)
		k.SetState(ctx, Contract, Slot(1), Sentinel)
		k.SetState(ctx, Contract, Slot(2), common.Hash{})
		k.SetState(ctx, Contract, Slot(ExtraSlot), Sentinel)
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
