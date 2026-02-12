package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

// GetParams returns the total set of ibc core module parameters.
func (k *Keeper) GetParams(ctx sdk.Context) types.Params {
	var p types.Params
	k.paramSpace.GetParamSet(ctx, &p)
	return p
}

// SetParams sets the ibc core module parameters.
func (k *Keeper) SetParams(ctx sdk.Context, p types.Params) {
	k.paramSpace.SetParamSet(ctx, &p)
}

// IsInboundEnabled returns true if inbound IBC is enabled.
func (k *Keeper) IsInboundEnabled(ctx sdk.Context) bool {
	return k.GetParams(ctx).InboundEnabled
}

// IsOutboundEnabled returns true if outbound IBC is enabled.
func (k *Keeper) IsOutboundEnabled(ctx sdk.Context) bool {
	return k.GetParams(ctx).OutboundEnabled
}

// SetInboundEnabled sets inbound enabled flag.
func (k *Keeper) SetInboundEnabled(ctx sdk.Context, enabled bool) {
	p := k.GetParams(ctx)
	p.InboundEnabled = enabled
	k.SetParams(ctx, p)
}

// SetOutboundEnabled sets outbound enabled flag.
func (k *Keeper) SetOutboundEnabled(ctx sdk.Context, enabled bool) {
	p := k.GetParams(ctx)
	p.OutboundEnabled = enabled
	k.SetParams(ctx, p)
}

// GetParamSpace returns the keeper's paramSpace (for other packages if needed).
func (k *Keeper) GetParamSpace() paramtypes.Subspace {
	return k.paramSpace
}
