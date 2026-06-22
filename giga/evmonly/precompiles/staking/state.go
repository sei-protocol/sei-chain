package staking

import (
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/util"
)

func addDelegation(store precompiles.Store, delegator string, validator string, delta *big.Int) error {
	record, ok, err := getDelegation(store, delegator, validator)
	if err != nil {
		return err
	}
	current := new(big.Int)
	if ok {
		current, err = util.ParseAmount(record.Amount)
		if err != nil {
			return err
		}
	}
	next := new(big.Int).Add(current, delta)
	if next.Sign() < 0 {
		return errors.New("delegation amount is insufficient")
	}
	if next.Sign() == 0 {
		store.Delete(delegationKey(delegator, validator))
		if err := removeStringListItem(store, delegatorDelegationsIndexKey(delegator), validator); err != nil {
			return err
		}
		return removeStringListItem(store, validatorDelegationsIndexKey(validator), delegator)
	}
	record = delegationRecord{
		DelegatorAddress: delegator,
		ValidatorAddress: validator,
		Amount:           next.String(),
	}
	if err := util.SetJSON(store, delegationKey(delegator, validator), record); err != nil {
		return err
	}
	if err := addStringListItem(store, delegatorDelegationsIndexKey(delegator), validator); err != nil {
		return err
	}
	return addStringListItem(store, validatorDelegationsIndexKey(validator), delegator)
}

func getDelegation(store precompiles.Store, delegator string, validator string) (delegationRecord, bool, error) {
	return util.GetJSON[delegationRecord](store, delegationKey(delegator, validator))
}

func validateDelegationAmount(store precompiles.Store, delegator string, validator string, amount *big.Int) error {
	record, ok, err := getDelegation(store, delegator, validator)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("delegation amount is insufficient")
	}
	current, err := util.ParseAmount(record.Amount)
	if err != nil {
		return err
	}
	if current.Cmp(amount) < 0 {
		return errors.New("delegation amount is insufficient")
	}
	return nil
}

func delegationFromRecord(record delegationRecord) (Delegation, error) {
	amount, err := util.ParseAmount(record.Amount)
	if err != nil {
		return Delegation{}, err
	}
	return Delegation{
		Balance: Balance{
			Amount: amount,
			Denom:  bondDenom,
		},
		Delegation: DelegationDetails{
			DelegatorAddress: record.DelegatorAddress,
			Shares:           new(big.Int).Set(amount),
			Decimals:         big.NewInt(precision),
			ValidatorAddress: record.ValidatorAddress,
		},
	}, nil
}

func setValidator(store precompiles.Store, validator Validator) error {
	if err := util.SetJSON(store, validatorKey(validator.OperatorAddress), validator); err != nil {
		return err
	}
	return addStringListItem(store, validatorsIndexKey(), validator.OperatorAddress)
}

func getValidator(store precompiles.Store, validatorAddress string) (Validator, bool, error) {
	return util.GetJSON[Validator](store, validatorKey(validatorAddress))
}

func removeValidator(store precompiles.Store, validatorAddress string) error {
	store.Delete(validatorKey(validatorAddress))
	return removeStringListItem(store, validatorsIndexKey(), validatorAddress)
}

func addValidatorTokens(store precompiles.Store, validatorAddress string, delta *big.Int) error {
	validator, ok, err := getValidator(store, validatorAddress)
	if err != nil {
		return err
	}
	if !ok {
		return errValidatorMissing
	}
	tokens, err := util.ParseAmount(validator.Tokens)
	if err != nil {
		return err
	}
	shares, err := util.ParseAmount(validator.DelegatorShares)
	if err != nil {
		return err
	}
	tokens.Add(tokens, delta)
	shares.Add(shares, delta)
	if tokens.Sign() < 0 || shares.Sign() < 0 {
		return errors.New("validator tokens are insufficient")
	}
	validator.Tokens = tokens.String()
	validator.DelegatorShares = shares.String()
	return setValidator(store, validator)
}

func addUnbondingDelegation(store precompiles.Store, delegator string, validator string, amount *big.Int, creationHeight uint64, completionTime int64) error {
	record, _, err := getUnbondingDelegation(store, delegator, validator)
	if err != nil {
		return err
	}
	record.DelegatorAddress = delegator
	record.ValidatorAddress = validator
	record.Entries = append(record.Entries, UnbondingDelegationEntry{
		CreationHeight: int64(creationHeight), //nolint:gosec // block heights fit signed ABI output in normal operation.
		CompletionTime: completionTime,
		InitialBalance: amount.String(),
		Balance:        amount.String(),
	})
	if err := util.SetJSON(store, unbondingDelegationKey(delegator, validator), record); err != nil {
		return err
	}
	if err := addStringListItem(store, delegatorUnbondingsIndexKey(delegator), validator); err != nil {
		return err
	}
	if err := addStringListItem(store, validatorUnbondingsIndexKey(validator), delegator); err != nil {
		return err
	}
	return insertUnbondingQueue(store, completionTime, delegator, validator)
}

func getUnbondingDelegation(store precompiles.Store, delegator string, validator string) (UnbondingDelegation, bool, error) {
	return util.GetJSON[UnbondingDelegation](store, unbondingDelegationKey(delegator, validator))
}

func addRedelegation(store precompiles.Store, delegator string, srcValidator string, dstValidator string, amount *big.Int, completionTime int64) error {
	record, _, err := getRedelegation(store, delegator, srcValidator, dstValidator)
	if err != nil {
		return err
	}
	record.DelegatorAddress = delegator
	record.ValidatorSrcAddress = srcValidator
	record.ValidatorDstAddress = dstValidator
	record.Entries = append(record.Entries, RedelegationEntry{
		CreationHeight: 0,
		CompletionTime: completionTime,
		InitialBalance: amount.String(),
		SharesDst:      amount.String(),
	})
	if err := util.SetJSON(store, redelegationKey(delegator, srcValidator, dstValidator), record); err != nil {
		return err
	}
	if err := addStringListItem(store, redelegationsIndexKey(), redelegationID(delegator, srcValidator, dstValidator)); err != nil {
		return err
	}
	return insertRedelegationQueue(store, completionTime, delegator, srcValidator, dstValidator)
}

func getRedelegation(store precompiles.Store, delegator string, srcValidator string, dstValidator string) (Redelegation, bool, error) {
	return util.GetJSON[Redelegation](store, redelegationKey(delegator, srcValidator, dstValidator))
}

func loadParams(store precompiles.Store) (Params, error) {
	params, ok, err := util.GetJSON[Params](store, paramsKey())
	if err != nil {
		return Params{}, err
	}
	if ok {
		params.BondDenom = bondDenom
		return params, nil
	}
	return Params{
		UnbondingTime:                      1_814_400,
		MaxValidators:                      100,
		MaxEntries:                         7,
		HistoricalEntries:                  10_000,
		BondDenom:                          bondDenom,
		MinCommissionRate:                  "0.000000000000000000",
		MaxVotingPowerRatio:                "0.000000000000000000",
		MaxVotingPowerEnforcementThreshold: "0.000000000000000000",
	}, nil
}

func loadPool(store precompiles.Store) (Pool, error) {
	pool, ok, err := util.GetJSON[Pool](store, poolKey())
	if err != nil {
		return Pool{}, err
	}
	if ok {
		return pool, nil
	}
	return Pool{NotBondedTokens: "0", BondedTokens: "0"}, nil
}

func addPoolBonded(store precompiles.Store, delta *big.Int) error {
	pool, err := loadPool(store)
	if err != nil {
		return err
	}
	bonded, err := util.ParseAmount(pool.BondedTokens)
	if err != nil {
		return err
	}
	bonded.Add(bonded, delta)
	if bonded.Sign() < 0 {
		return errors.New("bonded pool tokens are insufficient")
	}
	pool.BondedTokens = bonded.String()
	return util.SetJSON(store, poolKey(), pool)
}

func addPoolNotBonded(store precompiles.Store, delta *big.Int) error {
	pool, err := loadPool(store)
	if err != nil {
		return err
	}
	notBonded, err := util.ParseAmount(pool.NotBondedTokens)
	if err != nil {
		return err
	}
	notBonded.Add(notBonded, delta)
	if notBonded.Sign() < 0 {
		return errors.New("not bonded pool tokens are insufficient")
	}
	pool.NotBondedTokens = notBonded.String()
	return util.SetJSON(store, poolKey(), pool)
}

func movePoolsForRedelegation(store precompiles.Store, srcStatus int32, dstStatus int32, amount *big.Int) error {
	srcBonded := srcStatus == bondStatusBonded
	dstBonded := dstStatus == bondStatusBonded
	switch {
	case srcBonded == dstBonded:
		return nil
	case srcBonded:
		if err := addPoolBonded(store, new(big.Int).Neg(amount)); err != nil {
			return err
		}
		return addPoolNotBonded(store, amount)
	default:
		if err := addPoolNotBonded(store, new(big.Int).Neg(amount)); err != nil {
			return err
		}
		return addPoolBonded(store, amount)
	}
}

func setHistoricalInfo(store precompiles.Store, height uint64) error {
	if height > uint64(1<<63-1) {
		return nil
	}
	validatorAddresses, err := getStringList(store, validatorsIndexKey())
	if err != nil {
		return err
	}
	info := HistoricalInfo{
		Height:     int64(height), //nolint:gosec // bounded above.
		Validators: make([]Validator, 0, len(validatorAddresses)),
	}
	for _, validatorAddress := range validatorAddresses {
		validator, ok, err := getValidator(store, validatorAddress)
		if err != nil {
			return err
		}
		if ok {
			info.Validators = append(info.Validators, validator)
		}
	}
	return util.SetJSON(store, historicalInfoKey(info.Height), info)
}

func getHistoricalInfo(store precompiles.Store, height int64) (HistoricalInfo, bool, error) {
	return util.GetJSON[HistoricalInfo](store, historicalInfoKey(height))
}

func getStringList(store precompiles.Store, key []byte) ([]string, error) {
	items, ok, err := util.GetJSON[[]string](store, key)
	if err != nil || !ok {
		return nil, err
	}
	sort.Strings(items)
	return items, nil
}

func setStringList(store precompiles.Store, key []byte, items []string) error {
	if len(items) == 0 {
		store.Delete(key)
		return nil
	}
	sort.Strings(items)
	return util.SetJSON(store, key, items)
}

func addStringListItem(store precompiles.Store, key []byte, item string) error {
	items, err := getStringList(store, key)
	if err != nil {
		return err
	}
	for _, existing := range items {
		if existing == item {
			return nil
		}
	}
	items = append(items, item)
	sort.Strings(items)
	return util.SetJSON(store, key, items)
}

func removeStringListItem(store precompiles.Store, key []byte, item string) error {
	items, err := getStringList(store, key)
	if err != nil {
		return err
	}
	out := items[:0]
	for _, existing := range items {
		if existing != item {
			out = append(out, existing)
		}
	}
	if len(out) == 0 {
		store.Delete(key)
		return nil
	}
	return setStringList(store, key, out)
}

func insertUnbondingQueue(store precompiles.Store, completionTime int64, delegator string, validator string) error {
	id := timeQueueID(completionTime)
	items, err := getDelegationPairList(store, unbondingQueueKey(id))
	if err != nil {
		return err
	}
	item := delegationPair{DelegatorAddress: delegator, ValidatorAddress: validator}
	for _, existing := range items {
		if existing == item {
			return nil
		}
	}
	items = append(items, item)
	if err := util.SetJSON(store, unbondingQueueKey(id), items); err != nil {
		return err
	}
	return addStringListItem(store, unbondingQueueIndexKey(), id)
}

func insertRedelegationQueue(store precompiles.Store, completionTime int64, delegator string, srcValidator string, dstValidator string) error {
	id := timeQueueID(completionTime)
	items, err := getRedelegationTripletList(store, redelegationQueueKey(id))
	if err != nil {
		return err
	}
	item := redelegationTriplet{DelegatorAddress: delegator, ValidatorSrcAddress: srcValidator, ValidatorDstAddress: dstValidator}
	for _, existing := range items {
		if existing == item {
			return nil
		}
	}
	items = append(items, item)
	if err := util.SetJSON(store, redelegationQueueKey(id), items); err != nil {
		return err
	}
	return addStringListItem(store, redelegationQueueIndexKey(), id)
}

func insertValidatorQueue(store precompiles.Store, completionTime int64, completionHeight int64, validator string) error {
	id := validatorQueueID(completionTime, completionHeight)
	if err := addStringListItem(store, validatorQueueKey(id), validator); err != nil {
		return err
	}
	return addStringListItem(store, validatorQueueIndexKey(), id)
}

func deleteValidatorQueue(store precompiles.Store, completionTime int64, completionHeight int64, validator string) error {
	id := validatorQueueID(completionTime, completionHeight)
	if err := removeStringListItem(store, validatorQueueKey(id), validator); err != nil {
		return err
	}
	if items, err := getStringList(store, validatorQueueKey(id)); err != nil {
		return err
	} else if len(items) == 0 {
		return removeStringListItem(store, validatorQueueIndexKey(), id)
	}
	return nil
}

func getDelegationPairList(store precompiles.Store, key []byte) ([]delegationPair, error) {
	items, ok, err := util.GetJSON[[]delegationPair](store, key)
	if err != nil || !ok {
		return nil, err
	}
	return items, nil
}

func getRedelegationTripletList(store precompiles.Store, key []byte) ([]redelegationTriplet, error) {
	items, ok, err := util.GetJSON[[]redelegationTriplet](store, key)
	if err != nil || !ok {
		return nil, err
	}
	return items, nil
}

func matureTimeQueueIDs(store precompiles.Store, indexKey []byte, blockTime uint64) ([]string, error) {
	ids, err := getStringList(store, indexKey)
	if err != nil {
		return nil, err
	}
	mature := ids[:0]
	for _, id := range ids {
		time, err := parseTimeQueueID(id)
		if err != nil {
			return nil, err
		}
		if time <= int64(blockTime) {
			mature = append(mature, id)
		}
	}
	sort.SliceStable(mature, func(i, j int) bool {
		left, _ := parseTimeQueueID(mature[i])
		right, _ := parseTimeQueueID(mature[j])
		if left == right {
			return mature[i] < mature[j]
		}
		return left < right
	})
	return mature, nil
}

func matureValidatorQueueIDs(store precompiles.Store, blockTime uint64, blockHeight uint64) ([]string, error) {
	ids, err := getStringList(store, validatorQueueIndexKey())
	if err != nil {
		return nil, err
	}
	mature := ids[:0]
	for _, id := range ids {
		time, height, err := parseValidatorQueueID(id)
		if err != nil {
			return nil, err
		}
		if time <= int64(blockTime) && height <= int64(blockHeight) {
			mature = append(mature, id)
		}
	}
	sort.SliceStable(mature, func(i, j int) bool {
		leftTime, leftHeight, _ := parseValidatorQueueID(mature[i])
		rightTime, rightHeight, _ := parseValidatorQueueID(mature[j])
		if leftTime != rightTime {
			return leftTime < rightTime
		}
		if leftHeight != rightHeight {
			return leftHeight < rightHeight
		}
		return mature[i] < mature[j]
	})
	return mature, nil
}

func setLastValidatorPower(store precompiles.Store, validator string, power int64) error {
	if err := util.SetJSON(store, lastValidatorPowerKey(validator), power); err != nil {
		return err
	}
	return addStringListItem(store, lastValidatorsIndexKey(), validator)
}

func deleteLastValidatorPower(store precompiles.Store, validator string) error {
	store.Delete(lastValidatorPowerKey(validator))
	return removeStringListItem(store, lastValidatorsIndexKey(), validator)
}

func getLastValidatorPowers(store precompiles.Store) (map[string]int64, error) {
	validators, err := getStringList(store, lastValidatorsIndexKey())
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(validators))
	for _, validator := range validators {
		power, ok, err := util.GetJSON[int64](store, lastValidatorPowerKey(validator))
		if err != nil {
			return nil, err
		}
		if ok {
			out[validator] = power
		}
	}
	return out, nil
}

func setLastTotalPower(store precompiles.Store, power int64) error {
	return util.SetJSON(store, lastTotalPowerKey(), power)
}

func pageStrings(items []string, nextKey []byte) ([]string, []byte, error) {
	start := 0
	if len(nextKey) != 0 {
		parsed, err := strconv.Atoi(string(nextKey))
		if err != nil || parsed < 0 {
			return nil, nil, errors.New("invalid pagination key")
		}
		start = parsed
	}
	if start >= len(items) {
		return nil, nil, nil
	}
	end := start + pageLimit
	if end > len(items) {
		end = len(items)
	}
	var outNextKey []byte
	if end < len(items) {
		outNextKey = []byte(strconv.Itoa(end))
	}
	return items[start:end], outNextKey, nil
}

func paramsKey() []byte {
	return []byte("params")
}

func poolKey() []byte {
	return []byte("pool")
}

func validatorsIndexKey() []byte {
	return []byte("validators/index")
}

func validatorKey(validator string) []byte {
	return []byte("validator/" + validator)
}

func delegationKey(delegator string, validator string) []byte {
	return []byte("delegation/" + delegator + "/" + validator)
}

func delegatorDelegationsIndexKey(delegator string) []byte {
	return []byte("delegator-delegations/" + delegator)
}

func validatorDelegationsIndexKey(validator string) []byte {
	return []byte("validator-delegations/" + validator)
}

func unbondingDelegationKey(delegator string, validator string) []byte {
	return []byte("unbonding/" + delegator + "/" + validator)
}

func delegatorUnbondingsIndexKey(delegator string) []byte {
	return []byte("delegator-unbondings/" + delegator)
}

func validatorUnbondingsIndexKey(validator string) []byte {
	return []byte("validator-unbondings/" + validator)
}

func redelegationsIndexKey() []byte {
	return []byte("redelegations/index")
}

func redelegationKey(delegator string, srcValidator string, dstValidator string) []byte {
	return []byte("redelegation/" + redelegationID(delegator, srcValidator, dstValidator))
}

func redelegationID(delegator string, srcValidator string, dstValidator string) string {
	return delegator + "\x00" + srcValidator + "\x00" + dstValidator
}

func splitRedelegationID(id string) (string, string, string, bool) {
	parts := strings.Split(id, "\x00")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func historicalInfoKey(height int64) []byte {
	return []byte("historical/" + strconv.FormatInt(height, 10))
}

func lastValidatorsIndexKey() []byte {
	return []byte("last-validators/index")
}

func lastValidatorPowerKey(validator string) []byte {
	return []byte("last-validators/power/" + validator)
}

func lastTotalPowerKey() []byte {
	return []byte("last-validators/total-power")
}

func unbondingQueueIndexKey() []byte {
	return []byte("unbonding-queue/index")
}

func unbondingQueueKey(id string) []byte {
	return []byte("unbonding-queue/" + id)
}

func redelegationQueueIndexKey() []byte {
	return []byte("redelegation-queue/index")
}

func redelegationQueueKey(id string) []byte {
	return []byte("redelegation-queue/" + id)
}

func validatorQueueIndexKey() []byte {
	return []byte("validator-queue/index")
}

func validatorQueueKey(id string) []byte {
	return []byte("validator-queue/" + id)
}

func timeQueueID(completionTime int64) string {
	return strconv.FormatInt(completionTime, 10)
}

func parseTimeQueueID(id string) (int64, error) {
	return strconv.ParseInt(id, 10, 64)
}

func validatorQueueID(completionTime int64, completionHeight int64) string {
	return strconv.FormatInt(completionTime, 10) + "/" + strconv.FormatInt(completionHeight, 10)
}

func parseValidatorQueueID(id string) (int64, int64, error) {
	timePart, heightPart, ok := strings.Cut(id, "/")
	if !ok {
		return 0, 0, errors.New("invalid validator queue id")
	}
	completionTime, err := strconv.ParseInt(timePart, 10, 64)
	if err != nil {
		return 0, 0, err
	}
	completionHeight, err := strconv.ParseInt(heightPart, 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return completionTime, completionHeight, nil
}
