package keeper

import (
	"fmt"
	"strings"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

const KeySeparator = "|"

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate2to3 migrates from version 2 to 3.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	// Reset params after removing the denom creation fee param
	defaultParams := types.Params{}
	m.keeper.paramSpace.SetParamSet(ctx, &defaultParams)

	// We remove the denom creation fee whitelist in this migration
	store := ctx.KVStore(m.keeper.storeKey)
	oldCreateDenomFeeWhitelistKey := "createdenomfeewhitelist"

	oldCreateDenomFeeWhitelistPrefix := []byte(strings.Join([]string{oldCreateDenomFeeWhitelistKey, ""}, KeySeparator))
	iter := sdk.KVStorePrefixIterator(store, oldCreateDenomFeeWhitelistPrefix)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		store.Delete(iter.Key())
	}
	return nil
}

func (m Migrator) Migrate3to4(ctx sdk.Context) error {
	// Set denom metadata for all denoms
	iter := m.keeper.GetAllDenomsIterator(ctx)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		denom := string(iter.Value())
		denomMetadata, found := m.keeper.bankKeeper.GetDenomMetaData(ctx, denom)
		if !found {
			panic(fmt.Errorf("denom %s does not exist", denom))
		}
		fmt.Printf("Migrating denom: %s\n", denom)
		m.SetMetadata(&denomMetadata)
		m.keeper.bankKeeper.SetDenomMetaData(ctx, denomMetadata)
	}
	return nil
}

func (m Migrator) Migrate4to5(ctx sdk.Context) error {
	// Add new params and set all to defaults
	defaultParams := types.DefaultParams()
	m.keeper.SetParams(ctx, defaultParams)
	return nil
}

func (m Migrator) SetMetadata(denomMetadata *banktypes.Metadata) {
	if len(denomMetadata.Base) == 0 {
		panic(fmt.Errorf("no base exists for denom %v", denomMetadata))
	}
	if len(denomMetadata.Display) == 0 {
		denomMetadata.Display = denomMetadata.Base
		denomMetadata.Name = denomMetadata.Base
		denomMetadata.Symbol = denomMetadata.Base
	} else {
		fmt.Printf("Denom %s already has denom set", denomMetadata.Base)
	}
}
