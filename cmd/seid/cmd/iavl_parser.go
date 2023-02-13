package cmd

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
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
	switch {
	case bytes.HasPrefix(key, dextypes.KeyPrefix(dextypes.LongBookKey)):
		keyItems = append(keyItems, "LongBook")
		remaining := bytes.TrimPrefix(key, dextypes.KeyPrefix(dextypes.LongBookKey))
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		// TODO: make this better
		keyItems = append(keyItems, fmt.Sprintf("RemainingString: %s", string(remaining)))
	case bytes.HasPrefix(key, dextypes.KeyPrefix(dextypes.ShortBookKey)):
		keyItems = append(keyItems, "ShortBook")
		remaining := bytes.TrimPrefix(key, dextypes.KeyPrefix(dextypes.ShortBookKey))
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		// TODO: make this better
		keyItems = append(keyItems, fmt.Sprintf("RemainingString: %s", string(remaining)))
	case bytes.HasPrefix(key, dextypes.KeyPrefix(dextypes.TriggerBookKey)):
		keyItems = append(keyItems, "TriggerBook")
		remaining := bytes.TrimPrefix(key, dextypes.KeyPrefix(dextypes.TriggerBookKey))
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		// TODO: make this better
		keyItems = append(keyItems, fmt.Sprintf("RemainingString: %s", string(remaining)))
	case bytes.HasPrefix(key, dextypes.KeyPrefix(dextypes.TwapKey)):
		keyItems = append(keyItems, "TWAP")
		remaining := bytes.TrimPrefix(key, dextypes.KeyPrefix(dextypes.TwapKey))
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		if len(remaining) > 0 {
			keyItems = append(keyItems, fmt.Sprintf("RemainingString: %s", string(remaining)))
		}
	case bytes.HasPrefix(key, dextypes.KeyPrefix(dextypes.PriceKey)):
		keyItems = append(keyItems, "Price")
		remaining := bytes.TrimPrefix(key, dextypes.KeyPrefix(dextypes.PriceKey))
		items, remaining, err := parseLengthPrefixedAddress(remaining)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, items...)
		if len(remaining) > 0 {
			keyItems = append(keyItems, fmt.Sprintf("RemainingString: %s", string(remaining)))
		}
	default:
		keyItems = append(keyItems, "Unrecognized prefix")
	}
	return keyItems, nil
}

func parseLengthPrefixedAddress(remainingKey []byte) ([]string, []byte, error) {
	keyItems := []string{}
	lengthPrefix, remaining := int(remainingKey[0]), remainingKey[1:]
	keyItems = append(keyItems, fmt.Sprintf("AddrLength: %d", lengthPrefix))
	accountAddr := remaining[0:lengthPrefix]
	remaining = remaining[lengthPrefix:]
	bech32Addr, err := sdk.Bech32ifyAddressBytes("sei", accountAddr)
	if err != nil {
		return keyItems, remaining, err
	}
	keyItems = append(keyItems, fmt.Sprintf("AddrBech32: %s", bech32Addr))
	return keyItems, remaining, nil
}
