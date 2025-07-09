package app

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/cosmos/cosmos-sdk/client"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
)

type PrecompileKeepers struct {
	putils.BankKeeper
	putils.BankMsgServer
	putils.EVMKeeper
	putils.AccountKeeper
	putils.OracleKeeper
	putils.WasmdKeeper
	putils.WasmdViewKeeper
	putils.StakingKeeper
	putils.StakingQuerier
	putils.GovKeeper
	putils.GovMsgServer
	putils.DistributionKeeper
	putils.TransferKeeper
	putils.ClientKeeper
	putils.ConnectionKeeper
	putils.ChannelKeeper
	txConf client.TxConfig
}

func NewPrecompileKeepers(a *App) *PrecompileKeepers {
	return &PrecompileKeepers{
		BankKeeper:         a.BankKeeper,
		BankMsgServer:      bankkeeper.NewMsgServerImpl(a.BankKeeper),
		EVMKeeper:          &a.EvmKeeper,
		AccountKeeper:      a.AccountKeeper,
		OracleKeeper:       a.OracleKeeper,
		WasmdKeeper:        wasmkeeper.NewDefaultPermissionKeeper(a.WasmKeeper),
		WasmdViewKeeper:    a.WasmKeeper,
		StakingKeeper:      stakingkeeper.NewMsgServerImpl(a.StakingKeeper),
		StakingQuerier:     stakingkeeper.Querier{Keeper: a.StakingKeeper},
		GovKeeper:          a.GovKeeper,
		GovMsgServer:       govkeeper.NewMsgServerImpl(a.GovKeeper),
		DistributionKeeper: a.DistrKeeper,
		TransferKeeper:     a.TransferKeeper,
		ClientKeeper:       a.IBCKeeper.ClientKeeper,
		ConnectionKeeper:   a.IBCKeeper.ConnectionKeeper,
		ChannelKeeper:      a.IBCKeeper.ChannelKeeper,
		txConf:             a.GetTxConfig(),
	}
}

func (pk *PrecompileKeepers) BankK() putils.BankKeeper                 { return pk.BankKeeper }
func (pk *PrecompileKeepers) BankMS() putils.BankMsgServer             { return pk.BankMsgServer }
func (pk *PrecompileKeepers) EVMK() putils.EVMKeeper                   { return pk.EVMKeeper }
func (pk *PrecompileKeepers) AccountK() putils.AccountKeeper           { return pk.AccountKeeper }
func (pk *PrecompileKeepers) OracleK() putils.OracleKeeper             { return pk.OracleKeeper }
func (pk *PrecompileKeepers) WasmdK() putils.WasmdKeeper               { return pk.WasmdKeeper }
func (pk *PrecompileKeepers) WasmdVK() putils.WasmdViewKeeper          { return pk.WasmdViewKeeper }
func (pk *PrecompileKeepers) StakingK() putils.StakingKeeper           { return pk.StakingKeeper }
func (pk *PrecompileKeepers) StakingQ() putils.StakingQuerier          { return pk.StakingQuerier }
func (pk *PrecompileKeepers) GovK() putils.GovKeeper                   { return pk.GovKeeper }
func (pk *PrecompileKeepers) GovMS() putils.GovMsgServer               { return pk.GovMsgServer }
func (pk *PrecompileKeepers) DistributionK() putils.DistributionKeeper { return pk.DistributionKeeper }
func (pk *PrecompileKeepers) TransferK() putils.TransferKeeper         { return pk.TransferKeeper }
func (pk *PrecompileKeepers) ClientK() putils.ClientKeeper             { return pk.ClientKeeper }
func (pk *PrecompileKeepers) ConnectionK() putils.ConnectionKeeper     { return pk.ConnectionKeeper }
func (pk *PrecompileKeepers) ChannelK() putils.ChannelKeeper           { return pk.ChannelKeeper }
func (pk *PrecompileKeepers) TxConfig() client.TxConfig                { return pk.txConf }
