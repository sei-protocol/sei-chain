package keeper

import (
	"fmt"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
)

type SlashInfo struct {
	height             int64
	power              int64
	distributionHeight int64
	minHeight          int64
	minSignedPerWindow int64
}

// This performs similar logic to the above HandleValidatorSignature, but only performs READs such that it can be performed in parallel for all validators.
// Instead of updating appropriate validator bit arrays / signing infos, this will return the pending values to be written in a consistent order
func (k Keeper) HandleValidatorSignatureConcurrent(ctx sdk.Context, addr cryptotypes.Address, power int64, signed bool) (consAddr sdk.ConsAddress, missedInfo types.ValidatorMissedBlockArray, signInfo types.ValidatorSigningInfo, shouldSlash bool, slashInfo SlashInfo) {
	logger := k.Logger(ctx)
	height := ctx.BlockHeight()

	// fetch the validator public key
	consAddr = sdk.ConsAddress(addr)
	if _, err := k.GetPubkey(ctx, addr); err != nil {
		panic(fmt.Sprintf("Validator consensus-address %s not found", consAddr))
	}

	// fetch signing info
	signInfo, found := k.GetValidatorSigningInfo(ctx, consAddr)
	if !found {
		panic(fmt.Sprintf("Expected signing info for validator %s but not found", consAddr))
	}

	window := k.SignedBlocksWindow(ctx)

	missedInfo, found = k.GetValidatorMissedBlocks(ctx, consAddr)
	if !found {
		missedInfo = types.ValidatorMissedBlockArray{
			Address:       consAddr.String(),
			MissedHeights: make([]int64, 0),
		}
	}
	cutoff := height - window
	// go through and increment number evicted as necessary
	numberEvicted := 0
	for _, missedHeight := range missedInfo.MissedHeights {
		if missedHeight <= cutoff {
			numberEvicted += 1
		} else {
			break
		}
	}
	// update Missed Heights by excluding heights outside of the window
	missedInfo.MissedHeights = missedInfo.MissedHeights[numberEvicted:]
	missed := !signed
	if missed {
		missedInfo.MissedHeights = append(missedInfo.MissedHeights, height-1)
	}
	signInfo.MissedBlocksCounter = int64(len(missedInfo.MissedHeights))

	minSignedPerWindow := k.MinSignedPerWindow(ctx)
	if missed {
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeLiveness,
				sdk.NewAttribute(types.AttributeKeyAddress, consAddr.String()),
				sdk.NewAttribute(types.AttributeKeyMissedBlocks, fmt.Sprintf("%d", signInfo.MissedBlocksCounter)),
				sdk.NewAttribute(types.AttributeKeyHeight, fmt.Sprintf("%d", height)),
			),
		)

		logger.Debug(
			"absent validator",
			"height", height,
			"validator", consAddr.String(),
			"missed", signInfo.MissedBlocksCounter,
			"threshold", minSignedPerWindow,
		)
	}

	minHeight := signInfo.StartHeight + k.SignedBlocksWindow(ctx)
	maxMissed := window - minSignedPerWindow
	shouldSlash = false
	// if we are past the minimum height and the validator has missed too many blocks, punish them
	if height > minHeight && signInfo.MissedBlocksCounter > maxMissed {
		validator := k.sk.ValidatorByConsAddr(ctx, consAddr)
		if validator != nil && !validator.IsJailed() {
			// Downtime confirmed: slash and jail the validator
			// We need to retrieve the stake distribution which signed the block, so we subtract ValidatorUpdateDelay from the evidence height,
			// and subtract an additional 1 since this is the LastCommit.
			// Note that this *can* result in a negative "distributionHeight" up to -ValidatorUpdateDelay-1,
			// i.e. at the end of the pre-genesis block (none) = at the beginning of the genesis block.
			// That's fine since this is just used to filter unbonding delegations & redelegations.
			shouldSlash = true
			distributionHeight := height - sdk.ValidatorUpdateDelay - 1
			slashInfo = SlashInfo{
				height:             height,
				power:              power,
				distributionHeight: distributionHeight,
				minHeight:          minHeight,
				minSignedPerWindow: minSignedPerWindow,
			}
			// This value is passed back and the validator is slashed and jailed appropriately
		} else {
			// validator was (a) not found or (b) already jailed so we do not slash
			logger.Info(
				"validator would have been slashed for downtime, but was either not found in store or already jailed",
				"validator", consAddr.String(),
			)
		}
	}
	return
}

func (k Keeper) SlashJailAndUpdateSigningInfo(ctx sdk.Context, consAddr sdk.ConsAddress, slashInfo SlashInfo, signInfo types.ValidatorSigningInfo) types.ValidatorSigningInfo {
	logger := k.Logger(ctx)
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSlash,
			sdk.NewAttribute(types.AttributeKeyAddress, consAddr.String()),
			sdk.NewAttribute(types.AttributeKeyPower, fmt.Sprintf("%d", slashInfo.power)),
			sdk.NewAttribute(types.AttributeKeyReason, types.AttributeValueMissingSignature),
			sdk.NewAttribute(types.AttributeKeyJailed, consAddr.String()),
		),
	)

	k.sk.Slash(ctx, consAddr, slashInfo.distributionHeight, slashInfo.power, k.SlashFractionDowntime(ctx))
	k.sk.Jail(ctx, consAddr)
	signInfo.JailedUntil = ctx.BlockHeader().Time.Add(k.DowntimeJailDuration(ctx))
	signInfo.MissedBlocksCounter = 0
	logger.Info(
		"slashing and jailing validator due to liveness fault",
		"height", slashInfo.height,
		"validator", consAddr.String(),
		"min_height", slashInfo.minHeight,
		"threshold", slashInfo.minSignedPerWindow,
		"slashed", k.SlashFractionDowntime(ctx).String(),
		"jailed_until", signInfo.JailedUntil,
	)
	return signInfo
}
