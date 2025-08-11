package hd_test

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/crypto/hd"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/types"

	bip39 "github.com/cosmos/go-bip39"
	"github.com/stretchr/testify/require"
)

var defaultBIP39Passphrase = ""

// return bip39 seed with empty passphrase
func mnemonicToSeed(mnemonic string) []byte {
	return bip39.NewSeed(mnemonic, defaultBIP39Passphrase)
}

func TestStringifyFundraiserPathParams(t *testing.T) {
	path := hd.NewFundraiserParams(4, types.CoinType, 22)
	require.Equal(t, "m/44'/118'/4'/0/22", path.String())

	path = hd.NewFundraiserParams(4, types.CoinType, 57)
	require.Equal(t, "m/44'/118'/4'/0/57", path.String())

	path = hd.NewFundraiserParams(4, 12345, 57)
	require.Equal(t, "m/44'/12345'/4'/0/57", path.String())
}

func TestPathToArray(t *testing.T) {
	path := hd.NewParams(44, 118, 1, false, 4)
	require.Equal(t, "[44 118 1 0 4]", fmt.Sprintf("%v", path.DerivationPath()))

	path = hd.NewParams(44, 118, 2, true, 15)
	require.Equal(t, "[44 118 2 1 15]", fmt.Sprintf("%v", path.DerivationPath()))
}

func TestParamsFromPath(t *testing.T) {
	goodCases := []struct {
		params *hd.BIP44Params
		path   string
	}{
		{&hd.BIP44Params{44, 0, 0, false, 0}, "m/44'/0'/0'/0/0"},
		{&hd.BIP44Params{44, 1, 0, false, 0}, "m/44'/1'/0'/0/0"},
		{&hd.BIP44Params{44, 0, 1, false, 0}, "m/44'/0'/1'/0/0"},
		{&hd.BIP44Params{44, 0, 0, true, 0}, "m/44'/0'/0'/1/0"},
		{&hd.BIP44Params{44, 0, 0, false, 1}, "m/44'/0'/0'/0/1"},
		{&hd.BIP44Params{44, 1, 1, true, 1}, "m/44'/1'/1'/1/1"},
		{&hd.BIP44Params{44, 118, 52, true, 41}, "m/44'/118'/52'/1/41"},
	}

	for i, c := range goodCases {
		params, err := hd.NewParamsFromPath(c.path)
		errStr := fmt.Sprintf("%d %v", i, c)
		require.NoError(t, err, errStr)
		require.EqualValues(t, c.params, params, errStr)
		require.Equal(t, c.path, c.params.String())
	}

	badCases := []struct {
		path string
	}{
		{"m/43'/0'/0'/0/0"},   // doesn't start with 44
		{"m/44'/1'/0'/0/0/5"}, // too many fields
		{"m/44'/0'/1'/0"},     // too few fields
		{"m/44'/0'/0'/2/0"},   // change field can only be 0/1
		{"m/44/0'/0'/0/0"},    // first field needs '
		{"m/44'/0/0'/0/0"},    // second field needs '
		{"m/44'/0'/0/0/0"},    // third field needs '
		{"m/44'/0'/0'/0'/0"},  // fourth field must not have '
		{"m/44'/0'/0'/0/0'"},  // fifth field must not have '
		{"m/44'/-1'/0'/0/0"},  // no negatives
		{"m/44'/0'/0'/-1/0"},  // no negatives
		{"m/a'/0'/0'/-1/0"},   // invalid values
		{"m/0/X/0'/-1/0"},     // invalid values
		{"m/44'/0'/X/-1/0"},   // invalid values
		{"m/44'/0'/0'/%/0"},   // invalid values
		{"m/44'/0'/0'/0/%"},   // invalid values
		{"m44'0'0'00"},        // no separators
		{" /44'/0'/0'/0/0"},   // blank first component
	}

	for i, c := range badCases {
		params, err := hd.NewParamsFromPath(c.path)
		errStr := fmt.Sprintf("%d %v", i, c)
		require.Nil(t, params, errStr)
		require.Error(t, err, errStr)
	}

}

func TestCreateHDPath(t *testing.T) {
	type args struct {
		coinType uint32
		account  uint32
		index    uint32
	}
	tests := []struct {
		name string
		args args
		want hd.BIP44Params
	}{
		{"m/44'/0'/0'/0/0", args{0, 0, 0}, hd.BIP44Params{Purpose: 44}},
		{"m/44'/114'/0'/0/0", args{114, 0, 0}, hd.BIP44Params{Purpose: 44, CoinType: 114, Account: 0, AddressIndex: 0}},
		{"m/44'/114'/1'/1/0", args{114, 1, 1}, hd.BIP44Params{Purpose: 44, CoinType: 114, Account: 1, AddressIndex: 1}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tt := tt
			require.Equal(t, tt.want, *hd.CreateHDPath(tt.args.coinType, tt.args.account, tt.args.index))
		})
	}
}

// Tests to ensure that any index value is in the range [0, max(int32)] as per
// the extended keys specification. If the index belongs to that of a hardened key,
// its 0x80000000 bit will be set, so we can still accept values in [0, max(int32)] and then
// increase its value as deriveKeyPath already augments.
// See issue https://github.com/cosmos/cosmos-sdk/issues/7627.
func TestDeriveHDPathRange(t *testing.T) {
	seed := mnemonicToSeed("I am become Death, the destroyer of worlds!")

	tests := []struct {
		path    string
		wantErr string
	}{
		{
			path:    "m/1'/2147483648/0'/0/0",
			wantErr: "out of range",
		},
		{
			path:    "m/2147483648'/1/0/0",
			wantErr: "out of range",
		},
		{
			path:    "m/2147483648'/2147483648/0'/0/0",
			wantErr: "out of range",
		},
		{
			path:    "m/1'/-5/0'/0/0",
			wantErr: "invalid syntax",
		},
		{
			path:    "m/-2147483646'/1/0/0",
			wantErr: "invalid syntax",
		},
		{
			path:    "m/-2147483648'/-2147483648/0'/0/0",
			wantErr: "invalid syntax",
		},
		{
			path:    "m44'118'0'00",
			wantErr: "path 'm44'118'0'00' doesn't contain '/' separators",
		},
		{
			path:    "",
			wantErr: "path '' doesn't contain '/' separators",
		},
		{
			// Should pass.
			path: "m/1'/2147483647'/1/0'/0/0",
		},
		{
			// Should pass.
			path: "1'/2147483647'/1/0'/0/0",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			master, ch := hd.ComputeMastersFromSeed(seed)
			_, err := hd.DerivePrivateKeyForPath(master, ch, tt.path)

			if tt.wantErr == "" {
				require.NoError(t, err, "unexpected error")
			} else {
				require.Error(t, err, "expected a report of an int overflow")
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// Ensuring that we don't crash if values have trailing slashes
// See issue https://github.com/cosmos/cosmos-sdk/issues/8557.
func TestDerivePrivateKeyForPathDoNotCrash(t *testing.T) {
	paths := []string{
		"m/5/",
		"m/5",
		"/44",
		"m//5",
		"m/0/7",
		"/",
		" m       /0/7",          // Test case from fuzzer
		"              /       ", // Test case from fuzzer
		"m///7//////",
	}

	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			hd.DerivePrivateKeyForPath([32]byte{}, [32]byte{}, path)
		})
	}
}
