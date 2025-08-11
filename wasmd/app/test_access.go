package app

import (
	"testing"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/baseapp"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/client"

	"github.com/sei-protocol/sei-chain/wasmd/app/params"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/codec"
	bankkeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/bank/keeper"
	capabilitykeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/capability/keeper"
	stakingkeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/staking/keeper"
	ibctransferkeeper "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/apps/transfer/keeper"
	ibckeeper "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/core/keeper"

	"github.com/sei-protocol/sei-chain/wasmd/x/wasm"
)

type TestSupport struct {
	t   testing.TB
	app *WasmApp
}

func NewTestSupport(t testing.TB, app *WasmApp) *TestSupport {
	return &TestSupport{t: t, app: app}
}

func (s TestSupport) IBCKeeper() *ibckeeper.Keeper {
	return s.app.ibcKeeper
}

func (s TestSupport) WasmKeeper() wasm.Keeper {
	return s.app.wasmKeeper
}

func (s TestSupport) AppCodec() codec.Codec {
	return s.app.appCodec
}

func (s TestSupport) ScopedWasmIBCKeeper() capabilitykeeper.ScopedKeeper {
	return s.app.scopedWasmKeeper
}

func (s TestSupport) ScopeIBCKeeper() capabilitykeeper.ScopedKeeper {
	return s.app.scopedIBCKeeper
}

func (s TestSupport) ScopedTransferKeeper() capabilitykeeper.ScopedKeeper {
	return s.app.scopedTransferKeeper
}

func (s TestSupport) StakingKeeper() stakingkeeper.Keeper {
	return s.app.stakingKeeper
}

func (s TestSupport) BankKeeper() bankkeeper.Keeper {
	return s.app.bankKeeper
}

func (s TestSupport) TransferKeeper() ibctransferkeeper.Keeper {
	return s.app.transferKeeper
}

func (s TestSupport) GetBaseApp() *baseapp.BaseApp {
	return s.app.BaseApp
}

func (s TestSupport) GetTxConfig() client.TxConfig {
	return params.MakeEncodingConfig().TxConfig
}
