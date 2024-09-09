package cli

import (
	"bytes"
	"context"
	"fmt"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"google.golang.org/grpc"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestParseMetadata(t *testing.T) {
	cdc := codec.NewLegacyAmino()
	okJSON := testutil.WriteToNewTempFile(t, `
{
	"description": "Update token metadata",
	"denom_units": [
		{
			"denom": "doge1",
			"exponent": 6,
			"aliases": ["d", "o", "g"]
		},
		{
			"denom": "doge2",
			"exponent": 3,
			"aliases": ["d", "o", "g"]
		}
	],
	"base": "doge",
	"display": "DOGE",
	"name": "dogecoin",
	"symbol": "DOGE"
}
`)
	metadata, err := ParseMetadataJSON(cdc, okJSON.Name())
	require.NoError(t, err)

	require.Equal(t, banktypes.Metadata{
		Description: "Update token metadata",
		DenomUnits: []*banktypes.DenomUnit{{
			Denom:    "doge1",
			Exponent: 6,
			Aliases:  []string{"d", "o", "g"},
		}, {
			Denom:    "doge2",
			Exponent: 3,
			Aliases:  []string{"d", "o", "g"},
		}},
		Base:    "doge",
		Display: "DOGE",
		Name:    "dogecoin",
		Symbol:  "DOGE",
	}, metadata)
}

type MockQueryClient struct {
	evmtypes.QueryClient
}

func (m *MockQueryClient) SeiAddressByEVMAddress(ctx context.Context, in *evmtypes.QuerySeiAddressByEVMAddressRequest, opts ...grpc.CallOption) (*evmtypes.QuerySeiAddressByEVMAddressResponse, error) {
	return &evmtypes.QuerySeiAddressByEVMAddressResponse{SeiAddress: "sei1u8j4gaxyzhg39dk848q5w9h53tgggpcx74m762"}, nil
}

func TestParseAllowListJSON(t *testing.T) {
	addr1 := sdk.AccAddress("addr1_______________")
	addr2 := sdk.AccAddress("addr2_______________")
	allowListJSON := fmt.Sprintf(`{"addresses": ["%s", "%s"]}`, addr1, addr2)
	tempFile := testutil.WriteToNewTempFile(t, allowListJSON)
	mockQueryClient := &MockQueryClient{}
	// Test parsing the allow list
	allowList, err := ParseAllowListJSON(tempFile.Name(), mockQueryClient)
	require.NoError(t, err)
	expectedResult := banktypes.AllowList{
		Addresses: []string{addr1.String(), addr2.String()},
	}

	require.Equal(t, expectedResult, allowList)

	// Test with non-existent file
	_, err = ParseAllowListJSON("non_existent_file.json", mockQueryClient)
	require.Error(t, err)
	require.Equal(t, "open non_existent_file.json: no such file or directory", err.Error())

	// Test with invalid JSON
	invalidJsonFIle := testutil.WriteToNewTempFile(t, `{[}`)
	_, err = ParseAllowListJSON(invalidJsonFIle.Name(), mockQueryClient)
	require.Error(t, err)
	require.Equal(t, "invalid character '[' looking for beginning of object key string", err.Error())

	// Empty list
	emptyListFile := testutil.WriteToNewTempFile(t, `{[]}`)
	allowList, err = ParseAllowListJSON(emptyListFile.Name(), mockQueryClient)
	require.Equal(t, banktypes.AllowList{}, allowList)

	// Invalid address in the list
	invalidAddress := fmt.Sprintf(`{"addresses": ["%s", "nobech32"]}`, addr1)
	invalidAddressInJsonFile := testutil.WriteToNewTempFile(t, invalidAddress)
	_, err = ParseAllowListJSON(invalidAddressInJsonFile.Name(), mockQueryClient)
	require.Error(t, err)
	require.Equal(t, "invalid address nobech32: decoding bech32 failed: invalid separator index -1",
		err.Error())

	// Invalid address in the list
	evmAddress := fmt.Sprintf(`{"addresses": ["%s", "0x5c71b5577B9223d39ae0B7Dcb3f1BC8e1aC81f3e"]}`, addr1)
	evmAddressInJsonFile := testutil.WriteToNewTempFile(t, evmAddress)
	allowListWithConvertedAddress, err := ParseAllowListJSON(evmAddressInJsonFile.Name(), mockQueryClient)
	require.NoError(t, err)

	expectedResult = banktypes.AllowList{
		Addresses: []string{addr1.String(), "sei1u8j4gaxyzhg39dk848q5w9h53tgggpcx74m762"},
	}
	require.Equal(t, expectedResult, allowListWithConvertedAddress)
}

func TestNewCreateDenomCmd_AllowList(t *testing.T) {
	// Setup codec and client context
	cdc := codec.NewLegacyAmino()
	clientCtx := client.Context{
		LegacyAmino: cdc,
	}

	// Create a temporary command to test
	cmd := NewCreateDenomCmd()
	cmd.SetContext(context.WithValue(context.Background(), client.ClientContextKey, &clientCtx))

	// Create a temporary allow list JSON file with invalid content
	jsonInvalidFile := testutil.WriteToNewTempFile(t, `{[}`)

	// Define test cases
	testCases := []struct {
		name          string
		args          []string
		flags         []string
		expectErr     bool
		expectedError string
	}{
		{
			name:          "command fails with invalid allow list",
			args:          []string{"subdenom"},
			flags:         []string{"--allow-list", jsonInvalidFile.Name()},
			expectErr:     true,
			expectedError: "invalid character '[' looking for beginning of object key string",
		},
		{
			name:          "invalid: non-existent allow list file",
			args:          []string{"subdenom"},
			flags:         []string{"--allow-list", "non_existent_file.json"},
			expectErr:     true,
			expectedError: "open non_existent_file.json: no such file or directory",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set command arguments and flags
			cmd.SetArgs(append(tc.args, tc.flags...))

			// Capture output
			out := &bytes.Buffer{}
			cmd.SetOut(out)

			// Execute command
			err := cmd.Execute()

			// Check for expected errors
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
