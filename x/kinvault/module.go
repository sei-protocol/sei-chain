package kinvault

import (
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/sei-protocol/sei-chain/x/kinvault/keeper"
	"github.com/sei-protocol/sei-chain/x/kinvault/types"
)

type AppModule struct {
	keeper keeper.Keeper
}

func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{keeper: k}
}

func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
}
