package app

import (
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	govkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/keeper"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	mintkeeper "github.com/sei-protocol/sei-chain/x/mint/keeper"
)

type PrecompileKeepers struct {
	putils.BankKeeper
	putils.BankMsgServer
	putils.BankQuerier
	putils.EVMKeeper
	putils.AccountKeeper
	putils.AuthQuerier
	putils.AuthzQuerier
	putils.OracleKeeper
	putils.WasmdKeeper
	putils.WasmdViewKeeper
	putils.StakingKeeper
	putils.StakingQuerier
	putils.GovKeeper
	putils.GovMsgServer
	putils.GovQuerier
	putils.DistributionKeeper
	putils.DistributionQuerier
	putils.EvidenceQuerier
	putils.FeegrantQuerier
	putils.MintQuerier
	putils.ParamsQuerier
	putils.SlashingQuerier
	putils.UpgradeQuerier
	putils.TransferKeeper
	putils.ClientKeeper
	putils.ConnectionKeeper
	putils.ChannelKeeper
	txConf client.TxConfig
	cdc    codec.Codec
}

func NewPrecompileKeepers(a *App) *PrecompileKeepers {
	return &PrecompileKeepers{
		BankKeeper:          a.BankKeeper,
		BankMsgServer:       bankkeeper.NewMsgServerImpl(a.BankKeeper),
		BankQuerier:         a.BankKeeper,
		EVMKeeper:           &a.EvmKeeper,
		AccountKeeper:       a.AccountKeeper,
		AuthQuerier:         a.AccountKeeper,
		AuthzQuerier:        a.AuthzKeeper,
		OracleKeeper:        a.OracleKeeper,
		WasmdKeeper:         wasmkeeper.NewDefaultPermissionKeeper(a.WasmKeeper),
		WasmdViewKeeper:     a.WasmKeeper,
		StakingKeeper:       stakingkeeper.NewMsgServerImpl(a.StakingKeeper),
		StakingQuerier:      stakingkeeper.Querier{Keeper: a.StakingKeeper},
		GovKeeper:           a.GovKeeper,
		GovMsgServer:        govkeeper.NewMsgServerImpl(a.GovKeeper),
		GovQuerier:          a.GovKeeper,
		DistributionKeeper:  a.DistrKeeper,
		DistributionQuerier: a.DistrKeeper,
		EvidenceQuerier:     a.EvidenceKeeper,
		FeegrantQuerier:     a.FeeGrantKeeper,
		MintQuerier:         mintkeeper.NewQuerier(a.MintKeeper),
		ParamsQuerier:       a.ParamsKeeper,
		SlashingQuerier:     a.SlashingKeeper,
		UpgradeQuerier:      a.UpgradeKeeper,
		TransferKeeper:      a.TransferKeeper,
		ClientKeeper:        a.IBCKeeper.ClientKeeper,
		ConnectionKeeper:    a.IBCKeeper.ConnectionKeeper,
		ChannelKeeper:       a.IBCKeeper.ChannelKeeper,
		txConf:              a.GetTxConfig(),
		cdc:                 a.appCodec,
	}
}

func (pk *PrecompileKeepers) BankK() putils.BankKeeper                 { return pk.BankKeeper }
func (pk *PrecompileKeepers) BankMS() putils.BankMsgServer             { return pk.BankMsgServer }
func (pk *PrecompileKeepers) BankQ() putils.BankQuerier                { return pk.BankQuerier }
func (pk *PrecompileKeepers) EVMK() putils.EVMKeeper                   { return pk.EVMKeeper }
func (pk *PrecompileKeepers) AccountK() putils.AccountKeeper           { return pk.AccountKeeper }
func (pk *PrecompileKeepers) AuthQ() putils.AuthQuerier                { return pk.AuthQuerier }
func (pk *PrecompileKeepers) AuthzQ() putils.AuthzQuerier              { return pk.AuthzQuerier }
func (pk *PrecompileKeepers) OracleK() putils.OracleKeeper             { return pk.OracleKeeper }
func (pk *PrecompileKeepers) WasmdK() putils.WasmdKeeper               { return pk.WasmdKeeper }
func (pk *PrecompileKeepers) WasmdVK() putils.WasmdViewKeeper          { return pk.WasmdViewKeeper }
func (pk *PrecompileKeepers) StakingK() putils.StakingKeeper           { return pk.StakingKeeper }
func (pk *PrecompileKeepers) StakingQ() putils.StakingQuerier          { return pk.StakingQuerier }
func (pk *PrecompileKeepers) GovK() putils.GovKeeper                   { return pk.GovKeeper }
func (pk *PrecompileKeepers) GovMS() putils.GovMsgServer               { return pk.GovMsgServer }
func (pk *PrecompileKeepers) GovQ() putils.GovQuerier                  { return pk.GovQuerier }
func (pk *PrecompileKeepers) DistributionK() putils.DistributionKeeper { return pk.DistributionKeeper }
func (pk *PrecompileKeepers) DistributionQ() putils.DistributionQuerier {
	return pk.DistributionQuerier
}
func (pk *PrecompileKeepers) EvidenceQ() putils.EvidenceQuerier    { return pk.EvidenceQuerier }
func (pk *PrecompileKeepers) FeegrantQ() putils.FeegrantQuerier    { return pk.FeegrantQuerier }
func (pk *PrecompileKeepers) MintQ() putils.MintQuerier            { return pk.MintQuerier }
func (pk *PrecompileKeepers) ParamsQ() putils.ParamsQuerier        { return pk.ParamsQuerier }
func (pk *PrecompileKeepers) SlashingQ() putils.SlashingQuerier    { return pk.SlashingQuerier }
func (pk *PrecompileKeepers) UpgradeQ() putils.UpgradeQuerier      { return pk.UpgradeQuerier }
func (pk *PrecompileKeepers) TransferK() putils.TransferKeeper     { return pk.TransferKeeper }
func (pk *PrecompileKeepers) ClientK() putils.ClientKeeper         { return pk.ClientKeeper }
func (pk *PrecompileKeepers) ConnectionK() putils.ConnectionKeeper { return pk.ConnectionKeeper }
func (pk *PrecompileKeepers) ChannelK() putils.ChannelKeeper       { return pk.ChannelKeeper }
func (pk *PrecompileKeepers) TxConfig() client.TxConfig            { return pk.txConf }
func (pk *PrecompileKeepers) Codec() codec.Codec                   { return pk.cdc }
