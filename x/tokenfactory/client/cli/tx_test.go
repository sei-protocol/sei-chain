package cli

import (
	"bytes"
	"context"
	"fmt"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

func TestParseAllowListJSON(t *testing.T) {
	cdc := codec.NewLegacyAmino()
	addr1 := sdk.AccAddress("addr1_______________")
	addr2 := sdk.AccAddress("addr2_______________")
	allowListJSON := fmt.Sprintf(`{"addresses": ["%s", "%s"]}`, addr1, addr2)
	tempFile := testutil.WriteToNewTempFile(t, allowListJSON)

	// Test parsing the allow list
	allowList, err := ParseAllowListJSON(cdc, tempFile.Name())
	require.NoError(t, err)
	expectedResult := banktypes.AllowList{
		Addresses: []string{addr1.String(), addr2.String()},
	}

	require.Equal(t, expectedResult, allowList)

	// Test with non-existent file
	_, err = ParseAllowListJSON(cdc, "non_existent_file.json")
	require.Error(t, err)
	require.Equal(t, "open non_existent_file.json: no such file or directory", err.Error())

	// Test with invalid JSON
	invalidJsonFIle := testutil.WriteToNewTempFile(t, `{[}`)
	_, err = ParseAllowListJSON(cdc, invalidJsonFIle.Name())
	require.Error(t, err)
	require.Equal(t, "invalid character '[' looking for beginning of object key string", err.Error())

	// Empty list
	emptyListFile := testutil.WriteToNewTempFile(t, `{[]}`)
	allowList, err = ParseAllowListJSON(cdc, emptyListFile.Name())
	require.Equal(t, banktypes.AllowList{}, allowList)
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
