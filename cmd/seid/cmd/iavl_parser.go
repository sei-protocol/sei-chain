package cmd

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

type ModuleParser func([]byte) ([]string, error)

var ModuleParserMap = map[string]ModuleParser{
	"bank": BankParser,
	"mint": MintParser,
	"dex":  DexParser,
}

func MintParser(key []byte) ([]string, error) {
	keyItems := []string{}
	switch {
	case bytes.HasPrefix(key, minttypes.MinterKey):
		keyItems = append(keyItems, "MinterKey")
	case bytes.HasPrefix(key, minttypes.LastTokenReleaseDate):
		keyItems = append(keyItems, "LastTokenReleaseDate")
	default:
		keyItems = append(keyItems, "Unrecognized prefix")
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
		keyItems = append(keyItems, "Unrecognized prefix")
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
		keyItems = append(keyItems, "Unrecognized prefix")
	}
	return keyItems, nil
}

func MatchAndExtractDexAddressPrefixKeys(key []byte) (bool, []string, []byte, error) {
	keyItems := []string{}
	keysToMatch := []string{
		// Source of truth: github.com/sei-protocol/sei-chain/x/dex/types/keys.go - contains key constants represented here
		dextypes.LongBookKey,
		dextypes.ShortBookKey,
		dextypes.TriggerBookKey,
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
	bech32Addr, err := sdk.Bech32ifyAddressBytes("sei", accountAddr)
	if err != nil {
		return keyItems, remaining, err
	}
	keyItems = append(keyItems, fmt.Sprintf("AddrBech32: %s", bech32Addr))
	return keyItems, remaining, nil
}
