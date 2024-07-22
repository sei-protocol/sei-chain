package evm

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
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
		k.PrefixStore(ctx, serialized.Prefix).Set(serialized.Key, serialized.Value)
	}
}

func ExportGenesis(ctx sdk.Context, k *keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(ctx)
	k.IterateSeiAddressMapping(ctx, func(evmAddr common.Address, seiAddr sdk.AccAddress) bool {
		genesis.AddressAssociations = append(genesis.AddressAssociations, &types.AddressAssociation{
			SeiAddress: seiAddr.String(),
			EthAddress: evmAddr.Hex(),
		})
		return false
	})
	k.IterateAllCode(ctx, func(addr common.Address, code []byte) bool {
		genesis.Codes = append(genesis.Codes, &types.Code{
			Address: addr.Hex(),
			Code:    code,
		})
		return false
	})
	k.IterateState(ctx, func(addr common.Address, key, val common.Hash) bool {
		genesis.States = append(genesis.States, &types.ContractState{
			Address: addr.Hex(),
			Key:     key[:],
			Value:   val[:],
		})
		return false
	})
	k.IterateAllNonces(ctx, func(addr common.Address, nonce uint64) bool {
		genesis.Nonces = append(genesis.Nonces, &types.Nonce{
			Address: addr.Hex(),
			Nonce:   nonce,
		})
		return false
	})
	for _, prefix := range [][]byte{
		types.ReceiptKeyPrefix,
		types.BlockBloomPrefix,
		types.TxHashesPrefix,
		types.PointerRegistryPrefix,
		types.PointerCWCodePrefix,
		types.PointerReverseRegistryPrefix,
	} {
		k.IterateAll(ctx, prefix, func(key, val []byte) bool {
			genesis.Serialized = append(genesis.Serialized, &types.Serialized{
				Prefix: prefix,
				Key:    key,
				Value:  val,
			})
			return false
		})
	}

	return genesis
}

// TODO: move to better location
var GENESIS_EXPORT_STREAM_SERIALIZED_LEN_MAX = 1000

func ExportGenesisStream(ctx sdk.Context, k *keeper.Keeper) <-chan *types.GenesisState {
	ch := make(chan *types.GenesisState)
	go func() {
		genesis := types.DefaultGenesis()
		genesis.Params = k.GetParams(ctx)
		ch <- genesis

		k.IterateSeiAddressMapping(ctx, func(evmAddr common.Address, seiAddr sdk.AccAddress) bool {
			var genesis types.GenesisState
			genesis.Params = k.GetParams(ctx)
			genesis.AddressAssociations = append(genesis.AddressAssociations, &types.AddressAssociation{
				SeiAddress: seiAddr.String(),
				EthAddress: evmAddr.Hex(),
			})
			ch <- &genesis
			return false
		})

		k.IterateAllCode(ctx, func(addr common.Address, code []byte) bool {
			var genesis types.GenesisState
			genesis.Params = k.GetParams(ctx)
			genesis.Codes = append(genesis.Codes, &types.Code{
				Address: addr.Hex(),
				Code:    code,
			})
			ch <- &genesis
			return false
		})

		k.IterateState(ctx, func(addr common.Address, key, val common.Hash) bool {
			var genesis types.GenesisState
			genesis.Params = k.GetParams(ctx)
			genesis.States = append(genesis.States, &types.ContractState{
				Address: addr.Hex(),
				Key:     key[:],
				Value:   val[:],
			})
			ch <- &genesis
			return false
		})

		k.IterateAllNonces(ctx, func(addr common.Address, nonce uint64) bool {
			var genesis types.GenesisState
			genesis.Params = k.GetParams(ctx)
			genesis.Nonces = append(genesis.Nonces, &types.Nonce{
				Address: addr.Hex(),
				Nonce:   nonce,
			})
			ch <- &genesis
			return false
		})

		for _, prefix := range [][]byte{
			types.ReceiptKeyPrefix,
			types.BlockBloomPrefix,
			types.TxHashesPrefix,
			types.PointerRegistryPrefix,
			types.PointerCWCodePrefix,
			types.PointerReverseRegistryPrefix,
		} {
			var genesis types.GenesisState
			genesis.Params = k.GetParams(ctx)
			k.IterateAll(ctx, prefix, func(key, val []byte) bool {
				genesis.Serialized = append(genesis.Serialized, &types.Serialized{
					Prefix: prefix,
					Key:    key,
					Value:  val,
				})
				if len(genesis.Serialized) > GENESIS_EXPORT_STREAM_SERIALIZED_LEN_MAX {
					ch <- &genesis
					genesis = types.GenesisState{}
					genesis.Params = k.GetParams(ctx)
				}
				return false
			})
			ch <- &genesis
		}
		close(ch)
	}()
	return ch
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
