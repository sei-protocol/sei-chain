package migrations

import (
	"encoding/binary"
	"fmt"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V9ToV10(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	allContractInfo := dexkeeper.GetAllContractInfo(ctx)
	for _, contractInfo := range allContractInfo {

		store := prefix.NewStore(
			ctx.KVStore(dexkeeper.StoreKey),
			types.MatchResultPrefix(contractInfo.ContractAddr),
		)
		prevHeight := ctx.BlockHeight() - 1
		// Get latest match result
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(prevHeight))
		if !store.Has(key) {
			panic(fmt.Sprintf("Match result key not found for height %d", prevHeight))
		}
		bz := store.Get(key)
		result := types.MatchResult{}
		if err := result.Unmarshal(bz); err != nil {
			panic(err)
		}
		dexkeeper.SetMatchResult(ctx, contractInfo.ContractAddr, &result)

		// Now, remove all older ones
		for i := int64(0); i <= prevHeight; i++ {
			key := make([]byte, 8)
			binary.BigEndian.PutUint64(key, uint64(i))
			store.Delete(key)
		}
	}
	return nil
}
