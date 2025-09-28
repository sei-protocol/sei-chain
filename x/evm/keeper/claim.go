package keeper

import (
	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// Claim moves all native balances from the sender specified in the message to
// the claimer's Sei address that corresponds to the provided EVM address. The
// message must be signed by the sender, and the claimer receives funds in their
// associated Sei account (or the default casting if no mapping exists).
func (k *Keeper) Claim(ctx sdk.Context, msg *types.MsgClaim) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}

	sender, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return err
	}

	claimer := common.HexToAddress(msg.Claimer)
	recipient := k.GetSeiAddressOrDefault(ctx, claimer)
	balances := k.bankKeeper.GetAllBalances(ctx, sender)
	if len(balances) == 0 {
		return nil
	}

	if err := k.bankKeeper.SendCoins(ctx, sender, recipient, balances); err != nil {
		return err
	}

	return nil
}
