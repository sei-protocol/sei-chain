package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) QueryERCSingleOutput(ctx sdk.Context, typ string, addr common.Address, query string) (interface{}, error) {
	moduleAddr := k.AccountKeeper().GetModuleAddress(types.ModuleName)
	q, _ := artifacts.GetParsedABI(typ).Pack(query)
	r, err := k.StaticCallEVM(ctx, moduleAddr, &addr, q)
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("Error calling %s for %s due to %s, skipping", addr.Hex(), query, err))
		return nil, err
	}
	o, _ := artifacts.GetParsedABI(typ).Unpack("name", r)
	if len(o) != 1 {
		ctx.Logger().Error(fmt.Sprintf("Getting %d outputs when %s for %s, skipping", len(o), addr.Hex(), query))
		return nil, err
	}
	return o[0], nil
}
