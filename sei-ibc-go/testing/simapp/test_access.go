package simapp

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"

	"github.com/sei-protocol/sei-chain/sei-wasmd/app/params"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	capabilitykeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/keeper"
	distrkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/keeper"
	evidencekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/keeper"
	slashingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/keeper"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	ibctransferkeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/keeper"
	ibckeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
)

type TestSupport struct {
	t   testing.TB
	app *SimApp
}

func NewTestSupport(t testing.TB, app *SimApp) *TestSupport {
	return &TestSupport{t: t, app: app}
}

func (s TestSupport) IBCKeeper() *ibckeeper.Keeper {
	return s.app.IBCKeeper
}

func (s TestSupport) AppCodec() codec.Codec {
	return s.app.appCodec
}

func (s TestSupport) StakingKeeper() stakingkeeper.Keeper {
	return s.app.StakingKeeper
}

func (s TestSupport) BankKeeper() bankkeeper.Keeper {
	return s.app.BankKeeper
}

func (s TestSupport) TransferKeeper() ibctransferkeeper.Keeper {
	return s.app.TransferKeeper
}

func (s TestSupport) CapabilityKeeper() *capabilitykeeper.Keeper {
	return s.app.CapabilityKeeper
}

func (s TestSupport) DistrKeeper() *distrkeeper.Keeper {
	return &s.app.DistrKeeper
}

func (s TestSupport) SlashingKeeper() *slashingkeeper.Keeper {
	return &s.app.SlashingKeeper
}

func (s TestSupport) EvidenceKeeper() *evidencekeeper.Keeper {
	return &s.app.EvidenceKeeper
}

func (s TestSupport) GetBaseApp() *baseapp.BaseApp {
	return s.app.BaseApp
}

func (s TestSupport) GetTxConfig() client.TxConfig {
	return params.MakeEncodingConfig().TxConfig
}
