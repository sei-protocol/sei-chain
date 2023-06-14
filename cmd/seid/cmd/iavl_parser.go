package cmd

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
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
		keyItems = append(keyItems, "MinterKey")
	default:
		keyItems = append(keyItems, UNRECOGNIZED)
	}
	return keyItems, nil
}

func AccountParser(key []byte) ([]string, error) {
	keyItems := []string{}
	switch {
	case bytes.HasPrefix(key, authtypes.AddressStoreKeyPrefix):
		keyItems = append(keyItems, "AddressStore")
		remaining := bytes.TrimPrefix(key, authtypes.AddressStoreKeyPrefix)
		bech32Addr, err := sdk.Bech32ifyAddressBytes(params.Bech32PrefixAccAddr, remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, fmt.Sprintf("AddrBech32: %s", bech32Addr))
	default:
		keyItems = append(keyItems, UNRECOGNIZED)
	}
	return keyItems, nil
}

func BankParser(key []byte) ([]string, error) {
	keyItems := []string{}
	switch {
	case bytes.HasPrefix(key, banktypes.SupplyKey):
		keyItems = append(keyItems, "Supply")
		// rest of the key is the denom
		remaining := bytes.TrimPrefix(key, banktypes.SupplyKey)
		keyItems = append(keyItems, fmt.Sprintf("Denom: %s", string(remaining)))
	case bytes.HasPrefix(key, banktypes.DenomMetadataPrefix):
		keyItems = append(keyItems, "DenomMetadata")
		// rest of the key is the denom
		remaining := bytes.TrimPrefix(key, banktypes.DenomMetadataPrefix)
		keyItems = append(keyItems, fmt.Sprintf("Denom: %s", string(remaining)))
	case bytes.HasPrefix(key, banktypes.BalancesPrefix):
		keyItems = append(keyItems, "Balances")
		// remaining is length prefixed addr + denom
		remaining := bytes.TrimPrefix(key, banktypes.BalancesPrefix)
		items, denom, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		keyItems = append(keyItems, fmt.Sprintf("Denom: %s", string(denom)))
	default:
		keyItems = append(keyItems, UNRECOGNIZED)
	}

	return keyItems, nil
}

func DexParser(key []byte) ([]string, error) {
	keyItems := []string{}
	if matched, items, _, err := MatchAndExtractDexAddressPrefixKeys(key); matched {
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		return keyItems, nil
	}
	switch {
	case bytes.HasPrefix(key, []byte(dexkeeper.EpochKey)):
		// do nothing since the key is a string and no other data to be parsed
	default:
		keyItems = append(keyItems, UNRECOGNIZED)
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
			keyItems = append(keyItems, prefix)
			remaining := bytes.TrimPrefix(key, dextypes.KeyPrefix(prefix))
			items, remaining, err := parseLengthPrefixedAddress(remaining)
			if err != nil {
				return true, keyItems, remaining, err
			}
			keyItems = append(keyItems, items...)
			if len(remaining) > 0 {
				keyItems = append(keyItems, fmt.Sprintf("RemainingString: %s", string(remaining)))
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
	keyItems = append(keyItems, fmt.Sprintf("AddrBech32: %s", bech32Addr))
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
	keyItems = append(keyItems, fmt.Sprintf("ValBech32: %s", bech32Addr))
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
	keyItems = append(keyItems, fmt.Sprintf("ConsBech32: %s", bech32Addr))
	return keyItems, remaining, nil
}

func StakingParser(key []byte) ([]string, error) {
	keyItems := []string{}
	switch {
	case bytes.HasPrefix(key, stakingtypes.LastValidatorPowerKey):
		keyItems = append(keyItems, "LastValidatorPower")
		remaining := bytes.TrimPrefix(key, stakingtypes.LastValidatorPowerKey)
		items, _, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.LastTotalPowerKey):
		keyItems = append(keyItems, "LastTotalPower")
	case bytes.HasPrefix(key, stakingtypes.ValidatorsKey):
		keyItems = append(keyItems, "Validators")
		remaining := bytes.TrimPrefix(key, stakingtypes.ValidatorsKey)
		items, _, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.ValidatorsByConsAddrKey):
		keyItems = append(keyItems, "ValidatorsByConsAddr")
		remaining := bytes.TrimPrefix(key, stakingtypes.ValidatorsByConsAddrKey)
		items, _, err := parseLengthPrefixedConsAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.ValidatorsByPowerIndexKey):
		keyItems = append(keyItems, "ValidatorsByPowerIndex")
		operAddr := stakingtypes.ParseValidatorPowerRankKey(key)
		valAddr, err := sdk.Bech32ifyAddressBytes(params.Bech32PrefixValAddr, operAddr)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, fmt.Sprintf("ValBech32: %s", valAddr))
	case bytes.HasPrefix(key, stakingtypes.DelegationKey):
		keyItems = append(keyItems, "Delegation")
		remaining := bytes.TrimPrefix(key, stakingtypes.DelegationKey)
		// delegator addr
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		items, _, err = parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.UnbondingDelegationKey):
		keyItems = append(keyItems, "UnbondingDelegation")
		remaining := bytes.TrimPrefix(key, stakingtypes.UnbondingDelegationKey)
		// delegator addr
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		items, _, err = parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.UnbondingDelegationByValIndexKey):
		keyItems = append(keyItems, "UnbondingDelegationByValIndex")
		remaining := bytes.TrimPrefix(key, stakingtypes.UnbondingDelegationByValIndexKey)
		items, remaining, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		items, _, err = parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.RedelegationKey):
		keyItems = append(keyItems, "Redelegation")
		remaining := bytes.TrimPrefix(key, stakingtypes.RedelegationKey)
		items, _, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.RedelegationByValSrcIndexKey):
		keyItems = append(keyItems, "RedelegationByValSrcIndex")
		remaining := bytes.TrimPrefix(key, stakingtypes.RedelegationByValSrcIndexKey)
		items, remaining, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		items, _, err = parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.RedelegationByValDstIndexKey):
		keyItems = append(keyItems, "RedelegationByValDstIndex")
		remaining := bytes.TrimPrefix(key, stakingtypes.RedelegationByValDstIndexKey)
		items, _, err := parseLengthPrefixedOperAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		items, _, err = parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
	case bytes.HasPrefix(key, stakingtypes.UnbondingQueueKey):
		keyItems = append(keyItems, "UnbondingQueue")
		remaining := bytes.TrimPrefix(key, stakingtypes.UnbondingQueueKey)
		time, err := sdk.ParseTimeBytes(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, fmt.Sprintf("Timestamp: %s", time.String()))
	case bytes.HasPrefix(key, stakingtypes.RedelegationQueueKey):
		keyItems = append(keyItems, "RedelegationQueue")
		remaining := bytes.TrimPrefix(key, stakingtypes.RedelegationQueueKey)
		time, err := sdk.ParseTimeBytes(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, fmt.Sprintf("Timestamp: %s", time.String()))
	case bytes.HasPrefix(key, stakingtypes.ValidatorQueueKey):
		keyItems = append(keyItems, "ValidatorQueue")
		time, height, err := stakingtypes.ParseValidatorQueueKey(key)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, fmt.Sprintf("Time: %s", time.String()))
		keyItems = append(keyItems, fmt.Sprintf("Height: %d", height))
	case bytes.HasPrefix(key, stakingtypes.HistoricalInfoKey):
		keyItems = append(keyItems, "HistoricalInfo")
		remaining := bytes.TrimPrefix(key, stakingtypes.HistoricalInfoKey)
		keyItems = append(keyItems, fmt.Sprintf("Height: %s", remaining))
	default:
		keyItems = append(keyItems, UNRECOGNIZED)
	}
	return keyItems, nil
}
