package cmd

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/sei-protocol/goutils"
	"github.com/sei-protocol/sei-chain/app/params"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

type ModuleParser func([]byte) ([]string, error)

const UNRECOGNIZED = "Unrecognized Prefix"

var ModuleParserMap = map[string]ModuleParser{
	"bank":    BankParser,
	"mint":    MintParser,
	"dex":     DexParser,
	"staking": StakingParser,
	"acc":     AccountParser,
}

func MintParser(key []byte) ([]string, error) {
	keyItems := []string{}
	switch {
	case bytes.HasPrefix(key, minttypes.MinterKey):
		goutils.InPlaceAppend(&keyItems, "MinterKey")
	default:
		goutils.InPlaceAppend(&keyItems, UNRECOGNIZED)
	}
	return keyItems, nil
}

func AccountParser(key []byte) ([]string, error) {
	keyItems := []string{}
	switch {
	case bytes.HasPrefix(key, authtypes.AddressStoreKeyPrefix):
		goutils.InPlaceAppend(&keyItems, "AddressStore")
		remaining := bytes.TrimPrefix(key, authtypes.AddressStoreKeyPrefix)
		bech32Addr, err := sdk.Bech32ifyAddressBytes(params.Bech32PrefixAccAddr, remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("AddrBech32: %s", bech32Addr))
	default:
		goutils.InPlaceAppend(&keyItems, UNRECOGNIZED)
	}
	return keyItems, nil
}

func BankParser(key []byte) ([]string, error) {
	keyItems := []string{}
	switch {
	case bytes.HasPrefix(key, banktypes.SupplyKey):
		goutils.InPlaceAppend(&keyItems, "Supply")
		// rest of the key is the denom
		remaining := bytes.TrimPrefix(key, banktypes.SupplyKey)
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("Denom: %s", string(remaining)))
	case bytes.HasPrefix(key, banktypes.DenomMetadataPrefix):
		goutils.InPlaceAppend(&keyItems, "DenomMetadata")
		// rest of the key is the denom
		remaining := bytes.TrimPrefix(key, banktypes.DenomMetadataPrefix)
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("Denom: %s", string(remaining)))
	case bytes.HasPrefix(key, banktypes.BalancesPrefix):
		goutils.InPlaceAppend(&keyItems, "Balances")
		// remaining is length prefixed addr + denom
		remaining := bytes.TrimPrefix(key, banktypes.BalancesPrefix)
		items, denom, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("Denom: %s", string(denom)))
	default:
		goutils.InPlaceAppend(&keyItems, UNRECOGNIZED)
	}

	return keyItems, nil
}

func DexParser(key []byte) ([]string, error) {
	keyItems := []string{}
	if matched, items, _, err := MatchAndExtractDexAddressPrefixKeys(key); matched {
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
		return keyItems, nil
	}
	switch {
	case bytes.HasPrefix(key, []byte(dexkeeper.EpochKey)):
		// do nothing since the key is a string and no other data to be parsed
	default:
		goutils.InPlaceAppend(&keyItems, UNRECOGNIZED)
	}
	return keyItems, nil
}

func MatchAndExtractDexAddressPrefixKeys(key []byte) (bool, []string, []byte, error) {
	keyItems := []string{}
	keysToMatch := []string{
		// Source of truth: github.com/sei-protocol/sei-chain/x/dex/types/keys.go - contains key constants represented here
		dextypes.LongBookKey,
		dextypes.ShortBookKey,
		dextypes.PriceKey,
		dextypes.TwapKey,
		dextypes.SettlementEntryKey,
		dextypes.RegisteredPairKey,
		dextypes.OrderKey,
		dextypes.CancelKey,
		dextypes.AccountActiveOrdersKey,
		dextypes.NextOrderIDKey,
		dextypes.NextSettlementIDKey,
		dextypes.MatchResultKey,
		dextypes.MemOrderKey,
		dextypes.MemCancelKey,
		dextypes.MemDepositKey,
		dexkeeper.ContractPrefixKey,
	}

	for _, prefix := range keysToMatch {
		if bytes.HasPrefix(key, dextypes.KeyPrefix(prefix)) {
			goutils.InPlaceAppend(&keyItems, prefix)
			remaining := bytes.TrimPrefix(key, dextypes.KeyPrefix(prefix))
			items, remaining, err := parseLengthPrefixedAddress(remaining)
			if err != nil {
				return true, keyItems, remaining, err
			}
			goutils.InPlaceAppend(&keyItems, items...)
			if len(remaining) > 0 {
				goutils.InPlaceAppend(&keyItems, fmt.Sprintf("RemainingString: %s", string(remaining)))
			}
			return true, keyItems, remaining, nil
		}
	}
	return false, keyItems, key, nil
}

func parseLengthPrefixedAddress(remainingKey []byte) ([]string, []byte, error) {
	keyItems := []string{}
	lengthPrefix, remaining := int(remainingKey[0]), remainingKey[1:]
	accountAddr := remaining[0:lengthPrefix]
	remaining = remaining[lengthPrefix:]
	bech32Addr, err := sdk.Bech32ifyAddressBytes(params.Bech32PrefixAccAddr, accountAddr)
	if err != nil {
		return keyItems, remaining, err
	}
	goutils.InPlaceAppend(&keyItems, fmt.Sprintf("AddrBech32: %s", bech32Addr))
	return keyItems, remaining, nil
}

func parseLengthPrefixedOperAddress(remainingKey []byte) ([]string, []byte, error) {
	keyItems := []string{}
	lengthPrefix, remaining := int(remainingKey[0]), remainingKey[1:]
	accountAddr := remaining[0:lengthPrefix]
	remaining = remaining[lengthPrefix:]
	bech32Addr, err := sdk.Bech32ifyAddressBytes(params.Bech32PrefixValAddr, accountAddr)
	if err != nil {
		return keyItems, remaining, err
	}
	goutils.InPlaceAppend(&keyItems, fmt.Sprintf("ValBech32: %s", bech32Addr))
	return keyItems, remaining, nil
}

func parseLengthPrefixedConsAddress(remainingKey []byte) ([]string, []byte, error) {
	keyItems := []string{}
	lengthPrefix, remaining := int(remainingKey[0]), remainingKey[1:]
	accountAddr := remaining[0:lengthPrefix]
	remaining = remaining[lengthPrefix:]
	bech32Addr, err := sdk.Bech32ifyAddressBytes(params.Bech32PrefixConsAddr, accountAddr)
	if err != nil {
		return keyItems, remaining, err
	}
	goutils.InPlaceAppend(&keyItems, fmt.Sprintf("ConsBech32: %s", bech32Addr))
	return keyItems, remaining, nil
}

func StakingParser(key []byte) ([]string, error) {
	keyItems := []string{}
	switch {
	case bytes.HasPrefix(key, stakingtypes.LastValidatorPowerKey):
		goutils.InPlaceAppend(&keyItems, "LastValidatorPower")
		remaining := bytes.TrimPrefix(key, stakingtypes.LastValidatorPowerKey)
		items, _, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.LastTotalPowerKey):
		goutils.InPlaceAppend(&keyItems, "LastTotalPower")
	case bytes.HasPrefix(key, stakingtypes.ValidatorsKey):
		goutils.InPlaceAppend(&keyItems, "Validators")
		remaining := bytes.TrimPrefix(key, stakingtypes.ValidatorsKey)
		items, _, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.ValidatorsByConsAddrKey):
		goutils.InPlaceAppend(&keyItems, "ValidatorsByConsAddr")
		remaining := bytes.TrimPrefix(key, stakingtypes.ValidatorsByConsAddrKey)
		items, _, err := parseLengthPrefixedConsAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.ValidatorsByPowerIndexKey):
		goutils.InPlaceAppend(&keyItems, "ValidatorsByPowerIndex")
		operAddr := stakingtypes.ParseValidatorPowerRankKey(key)
		valAddr, err := sdk.Bech32ifyAddressBytes(params.Bech32PrefixValAddr, operAddr)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("ValBech32: %s", valAddr))
	case bytes.HasPrefix(key, stakingtypes.DelegationKey):
		goutils.InPlaceAppend(&keyItems, "Delegation")
		remaining := bytes.TrimPrefix(key, stakingtypes.DelegationKey)
		// delegator addr
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
		items, _, err = parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.UnbondingDelegationKey):
		goutils.InPlaceAppend(&keyItems, "UnbondingDelegation")
		remaining := bytes.TrimPrefix(key, stakingtypes.UnbondingDelegationKey)
		// delegator addr
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
		items, _, err = parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.UnbondingDelegationByValIndexKey):
		goutils.InPlaceAppend(&keyItems, "UnbondingDelegationByValIndex")
		remaining := bytes.TrimPrefix(key, stakingtypes.UnbondingDelegationByValIndexKey)
		items, remaining, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
		items, _, err = parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.RedelegationKey):
		goutils.InPlaceAppend(&keyItems, "Redelegation")
		remaining := bytes.TrimPrefix(key, stakingtypes.RedelegationKey)
		items, _, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.RedelegationByValSrcIndexKey):
		goutils.InPlaceAppend(&keyItems, "RedelegationByValSrcIndex")
		remaining := bytes.TrimPrefix(key, stakingtypes.RedelegationByValSrcIndexKey)
		items, remaining, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
		items, _, err = parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.RedelegationByValDstIndexKey):
		goutils.InPlaceAppend(&keyItems, "RedelegationByValDstIndex")
		remaining := bytes.TrimPrefix(key, stakingtypes.RedelegationByValDstIndexKey)
		items, _, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
		items, _, err = parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.UnbondingQueueKey):
		goutils.InPlaceAppend(&keyItems, "UnbondingQueue")
		remaining := bytes.TrimPrefix(key, stakingtypes.UnbondingQueueKey)
		time, err := sdk.ParseTimeBytes(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("Timestamp: %s", time.String()))
	case bytes.HasPrefix(key, stakingtypes.RedelegationQueueKey):
		goutils.InPlaceAppend(&keyItems, "RedelegationQueue")
		remaining := bytes.TrimPrefix(key, stakingtypes.RedelegationQueueKey)
		time, err := sdk.ParseTimeBytes(remaining)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("Timestamp: %s", time.String()))
	case bytes.HasPrefix(key, stakingtypes.ValidatorQueueKey):
		goutils.InPlaceAppend(&keyItems, "ValidatorQueue")
		time, height, err := stakingtypes.ParseValidatorQueueKey(key)
		if err != nil {
			return keyItems, err
		}
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("Time: %s", time.String()))
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("Height: %d", height))
	case bytes.HasPrefix(key, stakingtypes.HistoricalInfoKey):
		goutils.InPlaceAppend(&keyItems, "HistoricalInfo")
		remaining := bytes.TrimPrefix(key, stakingtypes.HistoricalInfoKey)
		goutils.InPlaceAppend(&keyItems, fmt.Sprintf("Height: %s", remaining))
	default:
		goutils.InPlaceAppend(&keyItems, UNRECOGNIZED)
	}
	return keyItems, nil
}
