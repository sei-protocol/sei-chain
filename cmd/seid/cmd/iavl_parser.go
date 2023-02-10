package cmd

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

type ModuleParser func([]byte) ([]string, error)

var ModuleParserMap map[string]ModuleParser = map[string]ModuleParser{
	"bank": BankParser,
}

func BankParser(key []byte) ([]string, error) {
	keyItems := []string{}
	// split the stuff before any colons for JUST prefix
	// key, err := hex.DecodeString(hexKey)
	// if err != nil {
	// 	return keyItems, err
	// }
	// iterate through, attempt to match against the Bank key prefixes
	if bytes.HasPrefix(key, banktypes.SupplyKey) {
		keyItems = append(keyItems, "Supply")
		// rest of the key is the denom
		remaining := bytes.TrimPrefix(key, banktypes.SupplyKey)
		keyItems = append(keyItems, fmt.Sprintf("Denom: %s", string(remaining)))
	} else if bytes.HasPrefix(key, banktypes.DenomMetadataPrefix) {
		keyItems = append(keyItems, "DenomMetadata")
		// rest of the key is the denom
		remaining := bytes.TrimPrefix(key, banktypes.DenomMetadataPrefix)
		keyItems = append(keyItems, fmt.Sprintf("Denom: %s", string(remaining)))
	} else if bytes.HasPrefix(key, banktypes.BalancesPrefix) {
		keyItems = append(keyItems, "Balances")
		// remaining is length prefixed addr + denom
		remaining := bytes.TrimPrefix(key, banktypes.BalancesPrefix)
		lengthPrefix, remaining := int(remaining[0]), remaining[1:]
		keyItems = append(keyItems, fmt.Sprintf("AddrLength: %d", lengthPrefix))
		accountAddr := remaining[0:lengthPrefix]
		denom := remaining[lengthPrefix:]
		bech32Addr, err := sdk.Bech32ifyAddressBytes("sei", accountAddr)
		if err != nil {
			return keyItems, err
		}
		keyItems = append(keyItems, fmt.Sprintf("AddrBech32: %s", bech32Addr))
		keyItems = append(keyItems, fmt.Sprintf("Denom: %s", string(denom)))
	} else {
		keyItems = append(keyItems, "Unrecognized prefix")
	}
	return keyItems, nil
}
