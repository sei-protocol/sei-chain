package composite

import (
	"fmt"

	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	authzkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz/keeper"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	capabilitytypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	evidencetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	ibctransfertypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
	ibchost "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	epochmoduletypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

// MemIAVLStoreKeys is the canonical list of module KV store keys that are
// mounted on the memiavl backend in a default production deployment
// (WriteMode = MemIAVLOnly). It mirrors the slice passed to
// sdk.NewKVStoreKeys in app.New (see app/app.go). Keep this list in sync
// with that call site.
var MemIAVLStoreKeys = []string{
	authtypes.StoreKey,
	authzkeeper.StoreKey,
	banktypes.StoreKey,
	stakingtypes.StoreKey,
	minttypes.StoreKey,
	distrtypes.StoreKey,
	slashingtypes.StoreKey,
	govtypes.StoreKey,
	paramstypes.StoreKey,
	ibchost.StoreKey,
	upgradetypes.StoreKey,
	feegrant.StoreKey,
	evidencetypes.StoreKey,
	ibctransfertypes.StoreKey,
	capabilitytypes.StoreKey,
	oracletypes.StoreKey,
	evmtypes.StoreKey,
	wasmtypes.StoreKey,
	epochmoduletypes.StoreKey,
	tokenfactorytypes.StoreKey,
}

var evmStoreKey = evmtypes.StoreKey
var bankStoreKey = banktypes.StoreKey

// Returns a list of modules excluding the specified modules. Returns an error if an exluded module
// is not a part of MemIAVLStoreKeys. The returned slice is safe to modify.
func AllModulesExcept(modulesNotToInclude ...string) ([]string, error) {
	exclude := make(map[string]bool, len(modulesNotToInclude))
	for _, m := range modulesNotToInclude {
		exclude[m] = true
	}

	known := make(map[string]bool, len(MemIAVLStoreKeys))
	for _, k := range MemIAVLStoreKeys {
		known[k] = true
	}
	for _, m := range modulesNotToInclude {
		if !known[m] {
			return nil, fmt.Errorf("module %q is not a member of MemIAVLStoreKeys", m)
		}
	}

	result := make([]string, 0, len(MemIAVLStoreKeys)-len(exclude))
	for _, k := range MemIAVLStoreKeys {
		if !exclude[k] {
			result = append(result, k)
		}
	}
	return result, nil
}
