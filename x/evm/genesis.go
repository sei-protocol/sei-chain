package evm

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func InitGenesis(ctx sdk.Context, k *keeper.Keeper, genState types.GenesisState) {
	k.InitGenesis(ctx, genState)
	k.SetParams(ctx, genState.Params)
	for _, aa := range genState.AddressAssociations {
		k.SetAddressMapping(ctx, sdk.MustAccAddressFromBech32(aa.SeiAddress), common.HexToAddress(aa.EthAddress))
	}
	for _, code := range genState.Codes {
		k.SetCode(ctx, common.HexToAddress(code.Address), code.Code)
	}
	for _, state := range genState.States {
		k.SetState(ctx, common.HexToAddress(state.Address), common.BytesToHash(state.Key), common.BytesToHash(state.Value))
	}
	for _, nonce := range genState.Nonces {
		k.SetNonce(ctx, common.HexToAddress(nonce.Address), nonce.Nonce)
	}
	for _, serialized := range genState.Serialized {
		if len(serialized.Key) == 0 {
			ctx.KVStore(k.GetStoreKey()).Set(serialized.Prefix, serialized.Value)
			continue
		}
		k.PrefixStore(ctx, serialized.Prefix).Set(serialized.Key, serialized.Value)
	}
}

// GetGenesisStateFromAppState returns x/evm GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *types.GenesisState {
	var genesisState types.GenesisState

	if appState[types.ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[types.ModuleName], &genesisState)
	}

	return &genesisState
}
