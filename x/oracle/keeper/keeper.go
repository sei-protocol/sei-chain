package keeper

import (
	"fmt"
	"sort"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

// Keeper of the oracle store
type Keeper struct {
	cdc        codec.BinaryCodec
	storeKey   sdk.StoreKey
	paramSpace paramstypes.Subspace

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	distrKeeper   types.DistributionKeeper
	StakingKeeper types.StakingKeeper

	distrName string
}

// NewKeeper constructs a new keeper for oracle
func NewKeeper(cdc codec.BinaryCodec, storeKey sdk.StoreKey,
	paramspace paramstypes.Subspace, accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper, distrKeeper types.DistributionKeeper,
	stakingKeeper types.StakingKeeper, distrName string,
) Keeper {
	// ensure oracle module account is set
	if addr := accountKeeper.GetModuleAddress(types.ModuleName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.ModuleName))
	}

	// set KeyTable if it has not already been set
	if !paramspace.HasKeyTable() {
		paramspace = paramspace.WithKeyTable(types.ParamKeyTable())
	}

	return Keeper{
		cdc:           cdc,
		storeKey:      storeKey,
		paramSpace:    paramspace,
		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,
		distrKeeper:   distrKeeper,
		StakingKeeper: stakingKeeper,
		distrName:     distrName,
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

//-----------------------------------
// ExchangeRate logic

func (k Keeper) GetBaseExchangeRate(ctx sdk.Context, denom string) (sdk.Dec, sdk.Int, error) {
	if denom == utils.MicroBaseDenom {
		votePeriod := k.GetParams(ctx).VotePeriod
		lastVotingBlockHeight := ((ctx.BlockHeight() / int64(votePeriod)) * int64(votePeriod)) - 1
		if lastVotingBlockHeight < 0 {
			lastVotingBlockHeight = 0
		}
		return sdk.OneDec(), sdk.NewInt(lastVotingBlockHeight), nil
	}

	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetExchangeRateKey(denom))
	if b == nil {
		return sdk.ZeroDec(), sdk.ZeroInt(), sdkerrors.Wrap(types.ErrUnknownDenom, denom)
	}

	exchangeRate := types.OracleExchangeRate{}
	k.cdc.MustUnmarshal(b, &exchangeRate)
	return exchangeRate.ExchangeRate, exchangeRate.LastUpdate, nil
}

func (k Keeper) SetBaseExchangeRate(ctx sdk.Context, denom string, exchangeRate sdk.Dec) {
	store := ctx.KVStore(k.storeKey)
	currHeight := sdk.NewInt(ctx.BlockHeight())
	rate := types.OracleExchangeRate{ExchangeRate: exchangeRate, LastUpdate: currHeight}
	bz := k.cdc.MustMarshal(&rate)
	store.Set(types.GetExchangeRateKey(denom), bz)
}

func (k Keeper) SetBaseExchangeRateWithEvent(ctx sdk.Context, denom string, exchangeRate sdk.Dec) {
	k.SetBaseExchangeRate(ctx, denom, exchangeRate)
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeExchangeRateUpdate,
			sdk.NewAttribute(types.AttributeKeyDenom, denom),
			sdk.NewAttribute(types.AttributeKeyExchangeRate, exchangeRate.String()),
		),
	)
}

func (k Keeper) DeleteBaseExchangeRate(ctx sdk.Context, denom string) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.GetExchangeRateKey(denom))
}

func (k Keeper) IterateBaseExchangeRates(ctx sdk.Context, handler func(denom string, exchangeRate types.OracleExchangeRate) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.ExchangeRateKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		denom := string(iter.Key()[len(types.ExchangeRateKey):])
		rate := types.OracleExchangeRate{}
		k.cdc.MustUnmarshal(iter.Value(), &rate)
		if handler(denom, rate) {
			break
		}
	}
}

//-----------------------------------
// Oracle delegation logic

// GetFeederDelegation gets the account address that the validator operator delegated oracle vote rights to
func (k Keeper) GetFeederDelegation(ctx sdk.Context, operator sdk.ValAddress) sdk.AccAddress {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.GetFeederDelegationKey(operator))
	if bz == nil {
		// By default the right is delegated to the validator itself
		return sdk.AccAddress(operator)
	}

	return sdk.AccAddress(bz)
}

// SetFeederDelegation sets the account address that the validator operator delegated oracle vote rights to
func (k Keeper) SetFeederDelegation(ctx sdk.Context, operator sdk.ValAddress, delegatedFeeder sdk.AccAddress) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.GetFeederDelegationKey(operator), delegatedFeeder.Bytes())
}

// IterateFeederDelegations iterates over the feed delegates and performs a callback function.
func (k Keeper) IterateFeederDelegations(ctx sdk.Context,
	handler func(delegator sdk.ValAddress, delegate sdk.AccAddress) (stop bool),
) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.FeederDelegationKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		delegator := sdk.ValAddress(iter.Key()[2:])
		delegate := sdk.AccAddress(iter.Value())

		if handler(delegator, delegate) {
			break
		}
	}
}

//-----------------------------------
// Miss counter logic

// GetVotePenaltyCounter retrieves the # of vote periods missed and abstained in this oracle slash window
func (k Keeper) GetVotePenaltyCounter(ctx sdk.Context, operator sdk.ValAddress) types.VotePenaltyCounter {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.GetVotePenaltyCounterKey(operator))
	if bz == nil {
		// By default the empty counter has values of 0
		return types.VotePenaltyCounter{}
	}

	var votePenaltyCounter types.VotePenaltyCounter
	k.cdc.MustUnmarshal(bz, &votePenaltyCounter)
	return votePenaltyCounter
}

// SetVotePenaltyCounter updates the # of vote periods missed in this oracle slash window
func (k Keeper) SetVotePenaltyCounter(ctx sdk.Context, operator sdk.ValAddress, missCount uint64, abstainCount uint64) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&types.VotePenaltyCounter{MissCount: missCount, AbstainCount: abstainCount})
	store.Set(types.GetVotePenaltyCounterKey(operator), bz)
}

func (k Keeper) IncrementMissCount(ctx sdk.Context, operator sdk.ValAddress) {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	k.SetVotePenaltyCounter(ctx, operator, votePenaltyCounter.MissCount+1, votePenaltyCounter.AbstainCount)
}

func (k Keeper) IncrementAbstainCount(ctx sdk.Context, operator sdk.ValAddress) {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	k.SetVotePenaltyCounter(ctx, operator, votePenaltyCounter.MissCount, votePenaltyCounter.AbstainCount+1)
}

func (k Keeper) GetMissCount(ctx sdk.Context, operator sdk.ValAddress) uint64 {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	return votePenaltyCounter.MissCount
}

func (k Keeper) GetAbstainCount(ctx sdk.Context, operator sdk.ValAddress) uint64 {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	return votePenaltyCounter.AbstainCount
}

// DeleteVotePenaltyCounter removes miss counter for the validator
func (k Keeper) DeleteVotePenaltyCounter(ctx sdk.Context, operator sdk.ValAddress) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.GetVotePenaltyCounterKey(operator))
}

// IterateVotePenaltyCounters iterates over the miss counters and performs a callback function.
func (k Keeper) IterateVotePenaltyCounters(ctx sdk.Context,
	handler func(operator sdk.ValAddress, votePenaltyCounter types.VotePenaltyCounter) (stop bool),
) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.VotePenaltyCounterKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		operator := sdk.ValAddress(iter.Key()[2:])

		var votePenaltyCounter types.VotePenaltyCounter
		k.cdc.MustUnmarshal(iter.Value(), &votePenaltyCounter)

		if handler(operator, votePenaltyCounter) {
			break
		}
	}
}

//-----------------------------------
// AggregateExchangeRatePrevote logic

// GetAggregateExchangeRatePrevote retrieves an oracle prevote from the store
func (k Keeper) GetAggregateExchangeRatePrevote(ctx sdk.Context, voter sdk.ValAddress) (aggregatePrevote types.AggregateExchangeRatePrevote, err error) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetAggregateExchangeRatePrevoteKey(voter))
	if b == nil {
		err = sdkerrors.Wrap(types.ErrNoAggregatePrevote, voter.String())
		return
	}
	k.cdc.MustUnmarshal(b, &aggregatePrevote)
	return
}

// SetAggregateExchangeRatePrevote set an oracle aggregate prevote to the store
func (k Keeper) SetAggregateExchangeRatePrevote(ctx sdk.Context, voter sdk.ValAddress, prevote types.AggregateExchangeRatePrevote) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&prevote)

	store.Set(types.GetAggregateExchangeRatePrevoteKey(voter), bz)
}

// DeleteAggregateExchangeRatePrevote deletes an oracle prevote from the store
func (k Keeper) DeleteAggregateExchangeRatePrevote(ctx sdk.Context, voter sdk.ValAddress) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.GetAggregateExchangeRatePrevoteKey(voter))
}

// IterateAggregateExchangeRatePrevotes iterates rate over prevotes in the store
func (k Keeper) IterateAggregateExchangeRatePrevotes(ctx sdk.Context, handler func(voterAddr sdk.ValAddress, aggregatePrevote types.AggregateExchangeRatePrevote) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.AggregateExchangeRatePrevoteKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		voterAddr := sdk.ValAddress(iter.Key()[2:])

		var aggregatePrevote types.AggregateExchangeRatePrevote
		k.cdc.MustUnmarshal(iter.Value(), &aggregatePrevote)
		if handler(voterAddr, aggregatePrevote) {
			break
		}
	}
}

//-----------------------------------
// AggregateExchangeRateVote logic

// GetAggregateExchangeRateVote retrieves an oracle prevote from the store
func (k Keeper) GetAggregateExchangeRateVote(ctx sdk.Context, voter sdk.ValAddress) (aggregateVote types.AggregateExchangeRateVote, err error) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetAggregateExchangeRateVoteKey(voter))
	if b == nil {
		err = sdkerrors.Wrap(types.ErrNoAggregateVote, voter.String())
		return
	}
	k.cdc.MustUnmarshal(b, &aggregateVote)
	return
}

// SetAggregateExchangeRateVote adds an oracle aggregate prevote to the store
func (k Keeper) SetAggregateExchangeRateVote(ctx sdk.Context, voter sdk.ValAddress, vote types.AggregateExchangeRateVote) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&vote)
	store.Set(types.GetAggregateExchangeRateVoteKey(voter), bz)
}

// DeleteAggregateExchangeRateVote deletes an oracle prevote from the store
func (k Keeper) DeleteAggregateExchangeRateVote(ctx sdk.Context, voter sdk.ValAddress) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.GetAggregateExchangeRateVoteKey(voter))
}

// IterateAggregateExchangeRateVotes iterates rate over prevotes in the store
func (k Keeper) IterateAggregateExchangeRateVotes(ctx sdk.Context, handler func(voterAddr sdk.ValAddress, aggregateVote types.AggregateExchangeRateVote) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.AggregateExchangeRateVoteKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		voterAddr := sdk.ValAddress(iter.Key()[2:])

		var aggregateVote types.AggregateExchangeRateVote
		k.cdc.MustUnmarshal(iter.Value(), &aggregateVote)
		if handler(voterAddr, aggregateVote) {
			break
		}
	}
}

func (k Keeper) GetVoteTarget(ctx sdk.Context, denom string) (types.Denom, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.GetVoteTargetKey(denom))
	if bz == nil {
		err := sdkerrors.Wrap(types.ErrNoVoteTarget, denom)
		return types.Denom{}, err
	}

	voteTarget := types.Denom{}
	k.cdc.MustUnmarshal(bz, &voteTarget)

	return voteTarget, nil
}

func (k Keeper) SetVoteTarget(ctx sdk.Context, denom string) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&types.Denom{Name: denom})
	store.Set(types.GetVoteTargetKey(denom), bz)
}

func (k Keeper) IterateVoteTargets(ctx sdk.Context, handler func(denom string, denomInfo types.Denom) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.VoteTargetKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		denom := types.ExtractDenomFromVoteTargetKey(iter.Key())

		var denomInfo types.Denom
		k.cdc.MustUnmarshal(iter.Value(), &denomInfo)
		if handler(denom, denomInfo) {
			break
		}
	}
}

func (k Keeper) ClearVoteTargets(ctx sdk.Context) {
	store := ctx.KVStore(k.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.VoteTargetKey)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		store.Delete(iter.Key())
	}
}

// ValidateFeeder return the given feeder is allowed to feed the message or not
func (k Keeper) ValidateFeeder(ctx sdk.Context, feederAddr sdk.AccAddress, validatorAddr sdk.ValAddress) error {
	if !feederAddr.Equals(validatorAddr) {
		delegate := k.GetFeederDelegation(ctx, validatorAddr)
		if !delegate.Equals(feederAddr) {
			return sdkerrors.Wrap(types.ErrNoVotingPermission, feederAddr.String())
		}
	}

	// Check that the given validator exists
	if val := k.StakingKeeper.Validator(ctx, validatorAddr); val == nil || !val.IsBonded() {
		return sdkerrors.Wrapf(stakingtypes.ErrNoValidatorFound, "validator %s is not active set", validatorAddr.String())
	}

	return nil
}

func (k Keeper) GetPriceSnapshot(ctx sdk.Context, timestamp int64) types.PriceSnapshot {
	store := ctx.KVStore(k.storeKey)
	snapshotBytes := store.Get(types.GetPriceSnapshotKey(uint64(timestamp)))
	if snapshotBytes == nil {
		return types.PriceSnapshot{}
	}

	priceSnapshot := types.PriceSnapshot{}
	k.cdc.MustUnmarshal(snapshotBytes, &priceSnapshot)
	return priceSnapshot
}

func (k Keeper) SetPriceSnapshot(ctx sdk.Context, snapshot types.PriceSnapshot) {
	// shouldn't be used directly, use "add" instead for individual price snapshots
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&snapshot)
	store.Set(types.GetPriceSnapshotKey(uint64(snapshot.SnapshotTimestamp)), bz)
}

func (k Keeper) AddPriceSnapshot(ctx sdk.Context, snapshot types.PriceSnapshot) {
	params := k.GetParams(ctx)
	lookbackDuration := params.LookbackDuration
	k.SetPriceSnapshot(ctx, snapshot)

	var lastOutOfRangeSnapshotTimestamp int64 = -1
	// we need to evict old snapshots (except for one that is out of range)
	k.IteratePriceSnapshots(ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		if snapshot.SnapshotTimestamp+lookbackDuration >= ctx.BlockTime().Unix() {
			return true
		}
		// delete the previous out of range snapshot
		if lastOutOfRangeSnapshotTimestamp >= 0 {
			k.DeletePriceSnapshot(ctx, lastOutOfRangeSnapshotTimestamp)
		}
		// update last out of range snapshot
		lastOutOfRangeSnapshotTimestamp = snapshot.SnapshotTimestamp
		return false
	})
}

func (k Keeper) IteratePriceSnapshots(ctx sdk.Context, handler func(snapshot types.PriceSnapshot) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.PriceSnapshotKey)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.PriceSnapshot
		k.cdc.MustUnmarshal(iterator.Value(), &val)
		if handler(val) {
			break
		}
	}
}

func (k Keeper) IteratePriceSnapshotsReverse(ctx sdk.Context, handler func(snapshot types.PriceSnapshot) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iterator := sdk.KVStoreReversePrefixIterator(store, types.PriceSnapshotKey)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.PriceSnapshot
		k.cdc.MustUnmarshal(iterator.Value(), &val)
		if handler(val) {
			break
		}
	}
}

func (k Keeper) DeletePriceSnapshot(ctx sdk.Context, timestamp int64) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.GetPriceSnapshotKey(uint64(timestamp)))
}

func (k Keeper) CalculateTwaps(ctx sdk.Context, lookbackSeconds int64) (types.OracleTwaps, error) {
	oracleTwaps := types.OracleTwaps{}
	currentTime := ctx.BlockTime().Unix()
	err := k.ValidateLookbackSeconds(ctx, lookbackSeconds)
	if err != nil {
		return oracleTwaps, err
	}
	var timeTraversed int64
	denomToTimeWeightedMap := make(map[string]sdk.Dec)
	denomDurationMap := make(map[string]int64)

	k.IteratePriceSnapshotsReverse(ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		stop = false
		snapshotTimestamp := snapshot.SnapshotTimestamp
		if currentTime-lookbackSeconds > snapshotTimestamp {
			snapshotTimestamp = currentTime - lookbackSeconds
			stop = true
		}
		// update time traversed to represent current snapshot
		// replace SnapshotTimestamp with lookback duration bounding
		timeTraversed = currentTime - snapshotTimestamp

		// iterate through denoms in the snapshot
		// if we find a new one, we have to setup the TWAP calc for that one
		snapshotPriceItems := snapshot.PriceSnapshotItems
		for _, priceItem := range snapshotPriceItems {
			denom := priceItem.Denom

			_, exists := denomToTimeWeightedMap[denom]
			if !exists {
				// set up the TWAP info for a denom
				denomToTimeWeightedMap[denom] = sdk.ZeroDec()
				denomDurationMap[denom] = 0
			}
			// get the denom specific TWAP data
			denomTimeWeightedSum := denomToTimeWeightedMap[denom]
			denomDuration := denomDurationMap[denom]

			// calculate the new Time Weighted Sum for the denom exchange rate
			// we calculate a weighted sum of exchange rates previously by multiplying each exchange rate by time interval that it was active
			// then we divide by the overall time in the lookback window, which gives us the time weighted average
			durationDifference := timeTraversed - denomDuration
			exchangeRate := priceItem.OracleExchangeRate.ExchangeRate
			denomTimeWeightedSum = denomTimeWeightedSum.Add(exchangeRate.MulInt64(durationDifference))

			// set the denom TWAP data
			denomToTimeWeightedMap[denom] = denomTimeWeightedSum
			denomDurationMap[denom] = timeTraversed
		}
		return
	})

	denomKeys := make([]string, 0, len(denomToTimeWeightedMap))
	for k := range denomToTimeWeightedMap {
		denomKeys = append(denomKeys, k)
	}
	sort.Strings(denomKeys)

	// iterate over all denoms with TWAP data
	for _, denomKey := range denomKeys {
		// divide the denom time weighed sum by denom duration
		denomTimeWeightedSum := denomToTimeWeightedMap[denomKey]
		denomDuration := denomDurationMap[denomKey]
		denomTwap := denomTimeWeightedSum.QuoInt64(denomDuration)

		denomOracleTwap := types.OracleTwap{
			Denom:           denomKey,
			Twap:            denomTwap,
			LookbackSeconds: denomDuration,
		}
		oracleTwaps = append(oracleTwaps, denomOracleTwap)
	}

	if len(oracleTwaps) == 0 {
		return oracleTwaps, types.ErrNoTwapData
	}

	return oracleTwaps, nil
}

func (k Keeper) ValidateLookbackSeconds(ctx sdk.Context, lookbackSeconds int64) error {
	lookbackDuration := k.LookbackDuration(ctx)
	if lookbackSeconds > lookbackDuration || lookbackSeconds <= 0 {
		return types.ErrInvalidTwapLookback
	}

	return nil
}

func (k Keeper) IsPrevoteFromPreviousWindow(ctx sdk.Context, valAddr sdk.ValAddress) bool {
	votePeriod := k.VotePeriod(ctx)
	prevote, err := k.GetAggregateExchangeRatePrevote(ctx, valAddr)
	if err != nil {
		return false
	}
	return (uint64(ctx.BlockHeight())/votePeriod)-(prevote.SubmitBlock/votePeriod) == 1
}
