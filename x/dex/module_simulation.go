package dex

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	simappparams "github.com/cosmos/cosmos-sdk/simapp/params"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	"github.com/sei-protocol/sei-chain/testutil/sample"
	dexsimulation "github.com/sei-protocol/sei-chain/x/dex/simulation"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// avoid unused import issue
var (
	_ = sample.AccAddress
	_ = dexsimulation.FindAccount
	_ = simappparams.StakePerAccount
	_ = simulation.MsgEntryKind
	_ = baseapp.Paramspace
)

//nolint:deadcode,unused,gosec // Assume this will be used later, and gosec is nolint because there are no hard-coded credentials here.
const (
	opWeightMsgLimitBuy = "op_weight_msg_create_chain"
	// TODO: Determine the simulation weight value
	defaultWeightMsgLimitBuy int = 100

	opWeightMsgLimitSell = "op_weight_msg_create_chain"
	// TODO: Determine the simulation weight value
	defaultWeightMsgLimitSell int = 100

	opWeightMsgMarketBuy = "op_weight_msg_create_chain"
	// TODO: Determine the simulation weight value
	defaultWeightMsgMarketBuy int = 100

	opWeightMsgMarketSell = "op_weight_msg_create_chain"
	// TODO: Determine the simulation weight value
	defaultWeightMsgMarketSell int = 100

	opWeightMsgCancelBuy = "op_weight_msg_create_chain"
	// TODO: Determine the simulation weight value
	defaultWeightMsgCancelBuy int = 100

	opWeightMsgCancelSell = "op_weight_msg_create_chain"
	// TODO: Determine the simulation weight value
	defaultWeightMsgCancelSell int = 100

	opWeightMsgCancelAll = "op_weight_msg_create_chain"
	// TODO: Determine the simulation weight value
	defaultWeightMsgCancelAll int = 100

	// this line is used by starport scaffolding # simapp/module/const
)

// GenerateGenesisState creates a randomized GenState of the module
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	dexGenesis := types.GenesisState{
		// this line is used by starport scaffolding # simapp/module/genesisState
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&dexGenesis)
}

// ProposalContents doesn't return any content functions for governance proposals
func (AppModule) ProposalContents(_ module.SimulationState) []simtypes.WeightedProposalContent {
	return nil
}

// RandomizedParams creates randomized  param changes for the simulator
func (am AppModule) RandomizedParams(_ *rand.Rand) []simtypes.ParamChange {
	return []simtypes.ParamChange{}
}

// RegisterStoreDecoder registers a decoder
func (am AppModule) RegisterStoreDecoder(_ sdk.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)

	// this line is used by starport scaffolding # simapp/module/operation

	return operations
}
