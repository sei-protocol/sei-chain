package utils

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/wasmd/x/wasm"
	sdk "github.com/sei-protocol/sei-chain/cosmos-sdk/types"
	acltypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/bank/types"
	capabilitytypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/capability/types"
	distrtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/distribution/types"
	evidencetypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/evidence/types"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/feegrant"
	govtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/gov/types"
	paramstypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/params/types"
	slashingtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/upgrade/types"
	ibctransfertypes "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/apps/transfer/types"
	ibchost "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/core/24-host"
	epochmoduletypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

var ModuleKeys = sdk.NewKVStoreKeys(
	acltypes.StoreKey, authtypes.StoreKey, authzkeeper.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
	minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
	govtypes.StoreKey, paramstypes.StoreKey, ibchost.StoreKey, upgradetypes.StoreKey, feegrant.StoreKey,
	evidencetypes.StoreKey, ibctransfertypes.StoreKey, capabilitytypes.StoreKey, oracletypes.StoreKey,
	evmtypes.StoreKey, wasm.StoreKey, epochmoduletypes.StoreKey, tokenfactorytypes.StoreKey,
)

var Modules = []string{
	"aclaccesscontrol",
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
