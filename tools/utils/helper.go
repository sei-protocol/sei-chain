package utils

import (
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	epochmoduletypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

var ModuleKeys = sdk.NewKVStoreKeys(
	authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
	minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
	govtypes.StoreKey, paramstypes.StoreKey, ibchost.StoreKey, upgradetypes.StoreKey, feegrant.StoreKey,
	evidencetypes.StoreKey, ibctransfertypes.StoreKey, capabilitytypes.StoreKey, oracletypes.StoreKey,
	evmtypes.StoreKey, wasm.StoreKey, epochmoduletypes.StoreKey, tokenfactorytypes.StoreKey,
)

var Modules = []string{
	"authz",
	"acc",
	"bank",
	"capability",
	"distribution",
	"epoch",
	"evidence",
	"evm",
	"feegrant",
	"gov",
	"ibc",
	"mint",
	"oracle",
	"params",
	"slashing",
	"staking",
	"tokenfactory",
	"transfer",
	"upgrade",
	"wasm"}

func BuildRawPrefix(moduleName string) string {
	return fmt.Sprintf("s/k:%s/n", moduleName)
}

func BuildTreePrefix(moduleName string) string {
	return fmt.Sprintf("s/k:%s/", moduleName)
}
