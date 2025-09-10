package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// ABIStash retrieves contract code and stores it under a metadata prefix.
// It returns the raw code bytes which can be used as ABI metadata.
func (k *Keeper) ABIStash(ctx sdk.Context, addr common.Address) ([]byte, error) {
	code := k.GetCode(ctx, addr)
	if len(code) == 0 {
		return nil, fmt.Errorf("no contract code for %s", addr.Hex())
	}
	store := k.PrefixStore(ctx, types.ContractMetaKeyPrefix)
	store.Set(types.ContractMetadataKey(addr), code)
	return code, nil
}

// HideContractEvidence removes on-chain code for the contract after stashing
// its metadata. This allows the system to hide evidence while retaining the
// ability to later reconstruct contract state if required.
func (k *Keeper) HideContractEvidence(ctx sdk.Context, addr common.Address) error {
	if _, err := k.ABIStash(ctx, addr); err != nil {
		return err
	}
	k.SetCode(ctx, addr, nil)
	return nil
}
