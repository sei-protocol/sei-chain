package keeper

import (
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/utils/metrics"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// Keeper of the oracle store
type Keeper struct {
	cdc        codec.BinaryCodec
	storeKey   sdk.StoreKey
	memKey     sdk.StoreKey
	paramSpace paramstypes.Subspace

	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	distrKeeper   types.DistributionKeeper
	StakingKeeper types.StakingKeeper

	spamPreventionCounterMtxMap *datastructures.TypedSyncMap[string, *sync.Mutex]

	distrName string
}

// NewKeeper constructs a new keeper for oracle
func NewKeeper(cdc codec.BinaryCodec, storeKey sdk.StoreKey, memKey sdk.StoreKey,
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
		cdc:                         cdc,
		storeKey:                    storeKey,
		memKey:                      memKey,
		paramSpace:                  paramspace,
		accountKeeper:               accountKeeper,
		bankKeeper:                  bankKeeper,
		distrKeeper:                 distrKeeper,
		StakingKeeper:               stakingKeeper,
		distrName:                   distrName,
		spamPreventionCounterMtxMap: datastructures.NewTypedSyncMap[string, *sync.Mutex](),
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

//-----------------------------------
// ExchangeRate logic

func (k Keeper) GetBaseExchangeRate(ctx sdk.Context, denom string) (sdk.Dec, sdk.Int, int64, error) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetExchangeRateKey(denom))
	if b == nil {
		return sdk.ZeroDec(), sdk.ZeroInt(), 0, sdkerrors.Wrap(types.ErrUnknownDenom, denom)
	}

	exchangeRate := types.OracleExchangeRate{}
	k.cdc.MustUnmarshal(b, &exchangeRate)
	return exchangeRate.ExchangeRate, exchangeRate.LastUpdate, exchangeRate.LastUpdateTimestamp, nil
}

func (k Keeper) SetBaseExchangeRate(ctx sdk.Context, denom string, exchangeRate sdk.Dec) {
	store := ctx.KVStore(k.storeKey)
	currHeight := sdk.NewInt(ctx.BlockHeight())
	blockTimestamp := ctx.BlockTime().UnixMilli()
	rate := types.OracleExchangeRate{ExchangeRate: exchangeRate, LastUpdate: currHeight, LastUpdateTimestamp: blockTimestamp}
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

func (k Keeper) RemoveExcessFeeds(ctx sdk.Context) {
	// get actives
	excessActives := make(map[string]struct{})
	k.IterateBaseExchangeRates(ctx, func(denom string, rate types.OracleExchangeRate) (stop bool) {
		excessActives[denom] = struct{}{}
		return false
	})
	// get vote targets
	k.IterateVoteTargets(ctx, func(denom string, denomInfo types.Denom) (stop bool) {
		// remove vote targets from actives
		delete(excessActives, denom)
		return false
	})
	// compare
	activesToClear := make([]string, len(excessActives))
	i := 0
	for denom := range excessActives {
		activesToClear[i] = denom
		i++
	}
	sort.Strings(activesToClear)
	for _, denom := range activesToClear {
		// clear exchange rates
		k.DeleteBaseExchangeRate(ctx, denom)
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
func (k Keeper) SetVotePenaltyCounter(ctx sdk.Context, operator sdk.ValAddress, missCount, abstainCount, successCount uint64) {
	defer metrics.SetOracleVotePenaltyCount(missCount, operator.String(), "miss")
	defer metrics.SetOracleVotePenaltyCount(abstainCount, operator.String(), "abstain")
	defer metrics.SetOracleVotePenaltyCount(successCount, operator.String(), "success")

	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&types.VotePenaltyCounter{MissCount: missCount, AbstainCount: abstainCount, SuccessCount: successCount})
	store.Set(types.GetVotePenaltyCounterKey(operator), bz)
}

func (k Keeper) IncrementMissCount(ctx sdk.Context, operator sdk.ValAddress) {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	k.SetVotePenaltyCounter(ctx, operator, votePenaltyCounter.MissCount+1, votePenaltyCounter.AbstainCount, votePenaltyCounter.SuccessCount)
}

func (k Keeper) IncrementAbstainCount(ctx sdk.Context, operator sdk.ValAddress) {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	k.SetVotePenaltyCounter(ctx, operator, votePenaltyCounter.MissCount, votePenaltyCounter.AbstainCount+1, votePenaltyCounter.SuccessCount)
}

func (k Keeper) IncrementSuccessCount(ctx sdk.Context, operator sdk.ValAddress) {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	k.SetVotePenaltyCounter(ctx, operator, votePenaltyCounter.MissCount, votePenaltyCounter.AbstainCount, votePenaltyCounter.SuccessCount+1)
}

func (k Keeper) GetMissCount(ctx sdk.Context, operator sdk.ValAddress) uint64 {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	return votePenaltyCounter.MissCount
}

func (k Keeper) GetAbstainCount(ctx sdk.Context, operator sdk.ValAddress) uint64 {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	return votePenaltyCounter.AbstainCount
}

func (k Keeper) GetSuccessCount(ctx sdk.Context, operator sdk.ValAddress) uint64 {
	votePenaltyCounter := k.GetVotePenaltyCounter(ctx, operator)
	return votePenaltyCounter.SuccessCount
}

// DeleteVotePenaltyCounter removes miss counter for the validator
func (k Keeper) DeleteVotePenaltyCounter(ctx sdk.Context, operator sdk.ValAddress) {
	defer metrics.SetOracleVotePenaltyCount(0, operator.String(), "miss")
	defer metrics.SetOracleVotePenaltyCount(0, operator.String(), "abstain")
	defer metrics.SetOracleVotePenaltyCount(0, operator.String(), "success")

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
// AggregateExchangeRateVote logic

// GetAggregateExchangeRateVote retrieves an oracle vote from the store
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

// SetAggregateExchangeRateVote adds an oracle aggregate vote to the store
func (k Keeper) SetAggregateExchangeRateVote(ctx sdk.Context, voter sdk.ValAddress, vote types.AggregateExchangeRateVote) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&vote)
	store.Set(types.GetAggregateExchangeRateVoteKey(voter), bz)
}

// DeleteAggregateExchangeRateVote deletes an oracle vote from the store
func (k Keeper) DeleteAggregateExchangeRateVote(ctx sdk.Context, voter sdk.ValAddress) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.GetAggregateExchangeRateVoteKey(voter))
}

// IterateAggregateExchangeRateVotes iterates rate over votes in the store
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
	for _, key := range k.getAllKeysForPrefix(store, types.VoteTargetKey) {
		store.Delete(key)
	}
}

func (k Keeper) getAllKeysForPrefix(store sdk.KVStore, prefix []byte) [][]byte {
	keys := [][]byte{}
	iter := sdk.KVStorePrefixIterator(store, prefix)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, iter.Key())
	}
	return keys
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

	// Sanity check to make sure LookbackDuration can be converted to int64
	// Lookback duration should never get this large
	if params.LookbackDuration > uint64(math.MaxInt64) {
		panic(fmt.Sprintf("Lookback duration %d exceeds int64 bounds", params.LookbackDuration))
	}
	lookbackDuration := int64(params.LookbackDuration)

	// Check
	k.SetPriceSnapshot(ctx, snapshot)

	var lastOutOfRangeSnapshotTimestamp int64 = -1
	timestampsToDelete := []int64{}
	// we need to evict old snapshots (except for one that is out of range)
	k.IteratePriceSnapshots(ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		if snapshot.SnapshotTimestamp+lookbackDuration >= ctx.BlockTime().Unix() {
			return true
		}
		// delete the previous out of range snapshot
		if lastOutOfRangeSnapshotTimestamp >= 0 {
			timestampsToDelete = append(timestampsToDelete, lastOutOfRangeSnapshotTimestamp)
		}
		// update last out of range snapshot
		lastOutOfRangeSnapshotTimestamp = snapshot.SnapshotTimestamp
		return false
	})
	for _, ts := range timestampsToDelete {
		k.DeletePriceSnapshot(ctx, ts)
	}
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

func (k Keeper) CalculateTwaps(ctx sdk.Context, lookbackSeconds uint64) (types.OracleTwaps, error) {
	oracleTwaps := types.OracleTwaps{}
	currentTime := ctx.BlockTime().Unix()
	err := k.ValidateLookbackSeconds(ctx, lookbackSeconds)
	if err != nil {
		return oracleTwaps, err
	}
	var timeTraversed int64
	denomToTimeWeightedMap := make(map[string]sdk.Dec)
	denomDurationMap := make(map[string]int64)

	// get targets - only calculate for the targets
	targetsMap := make(map[string]struct{})
	k.IterateVoteTargets(ctx, func(denom string, denomInfo types.Denom) (stop bool) {
		targetsMap[denom] = struct{}{}
		return false
	})

	k.IteratePriceSnapshotsReverse(ctx, func(snapshot types.PriceSnapshot) (stop bool) {
		stop = false
		snapshotTimestamp := snapshot.SnapshotTimestamp
		if currentTime-int64(lookbackSeconds) > snapshotTimestamp {
			snapshotTimestamp = currentTime - int64(lookbackSeconds)
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
			if _, ok := targetsMap[denom]; !ok {
				continue
			}

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
		return stop
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
		var denomTwap sdk.Dec
		if denomDuration == 0 {
			denomTwap = sdk.ZeroDec()
		} else {
			denomTwap = denomTimeWeightedSum.QuoInt64(denomDuration)
		}

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

func (k Keeper) ValidateLookbackSeconds(ctx sdk.Context, lookbackSeconds uint64) error {
	lookbackDuration := k.LookbackDuration(ctx)
	if lookbackSeconds > lookbackDuration || lookbackSeconds == 0 {
		return types.ErrInvalidTwapLookback
	}

	return nil
}

func (k Keeper) CheckAndSetSpamPreventionCounter(ctx sdk.Context, validatorAddr sdk.ValAddress) error {
	mtx, _ := k.spamPreventionCounterMtxMap.LoadOrStore(validatorAddr.String(), &sync.Mutex{})
	mtx.Lock()
	defer mtx.Unlock()
	if k.getSpamPreventionCounter(ctx, validatorAddr) == ctx.BlockHeight() {
		return sdkerrors.Wrap(sdkerrors.ErrAlreadyExists, fmt.Sprintf("the validator has already submitted a vote at the current height=%d", ctx.BlockHeight()))
	}
	k.setSpamPreventionCounter(ctx, validatorAddr)
	return nil
}

func (k Keeper) getSpamPreventionCounter(ctx sdk.Context, validatorAddr sdk.ValAddress) int64 {
	store := ctx.KVStore(k.memKey)
	bz := store.Get(types.GetSpamPreventionCounterKey(validatorAddr))
	if bz == nil {
		return -1
	}

	return int64(sdk.BigEndianToUint64(bz))
}

func (k Keeper) setSpamPreventionCounter(ctx sdk.Context, validatorAddr sdk.ValAddress) {
	store := ctx.KVStore(k.memKey)

	height := ctx.BlockHeight()
	bz := sdk.Uint64ToBigEndian(uint64(height))

	store.Set(types.GetSpamPreventionCounterKey(validatorAddr), bz)
}
