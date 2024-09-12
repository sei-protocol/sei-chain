package cli

import (
	"bytes"
	"context"
	"fmt"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
	"os"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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

type MockErrorQueryClient struct {
	evmtypes.QueryClient
}

func (m *MockErrorQueryClient) SeiAddressByEVMAddress(ctx context.Context, in *evmtypes.QuerySeiAddressByEVMAddressRequest, opts ...grpc.CallOption) (*evmtypes.QuerySeiAddressByEVMAddressResponse, error) {
	return nil, fmt.Errorf("address is not associated")
}

func Test_ParseAllowListJSON(t *testing.T) {
	mockQueryClient := &MockQueryClient{}

	seiAddr1 := sdk.AccAddress("sei1_______________").String()
	seiAddr2 := sdk.AccAddress("sei2_______________").String()
	evmAddr := "0x5c71b5577B9223d39ae0B7Dcb3f1BC8e1aC81f3e"
	convertedSeiAddr := "sei1u8j4gaxyzhg39dk848q5w9h53tgggpcx74m762"

	tests := []struct {
		name    string
		json    string
		want    banktypes.AllowList
		wantErr string
		errMock bool
	}{
		{
			name: "valid allow list with Sei addresses",
			json: fmt.Sprintf(`{"addresses": ["%s", "%s"]}`, seiAddr1, seiAddr2),
			want: banktypes.AllowList{Addresses: []string{seiAddr1, seiAddr2}},
		},
		{
			name: "valid allow list with Sei and EVM addresses",
			json: fmt.Sprintf(`{"addresses": ["%s", "%s", "%s"]}`, seiAddr1, seiAddr2, evmAddr),
			want: banktypes.AllowList{Addresses: []string{seiAddr1, seiAddr2, convertedSeiAddr}},
		},
		{
			name:    "invalid JSON",
			json:    `{[}`,
			wantErr: "invalid character '[' looking for beginning of object key string",
		},
		{
			name:    "invalid Sei address",
			json:    `{"addresses": ["invalid_sei_address"]}`,
			wantErr: "invalid address invalid_sei_address: decoding bech32 failed:",
		},
		{
			name:    "invalid EVM address",
			json:    `{"addresses": ["0xinvalid_evm_address"]}`,
			wantErr: "invalid address 0xinvalid_evm_address:",
		},
		{
			name:    "EVM address not associated",
			json:    fmt.Sprintf(`{"addresses": ["%s"]}`, evmAddr),
			wantErr: "address is not associated",
			errMock: true,
		},
		{
			name: "empty allow list",
			json: `{"addresses": []}`,
			want: banktypes.AllowList{Addresses: []string{}},
		},
		{
			name: "duplicate addresses",
			json: fmt.Sprintf(`{"addresses": ["%s", "%s", "%s"]}`, seiAddr1, seiAddr1, seiAddr2),
			want: banktypes.AllowList{Addresses: []string{seiAddr1, seiAddr2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			tempFile := testutil.WriteToNewTempFile(t, tt.json)
			defer os.Remove(tempFile.Name())

			var m evmtypes.QueryClient
			m = mockQueryClient
			if tt.errMock {
				m = &MockErrorQueryClient{}
			}

			got, err := ParseAllowListJSON(tempFile.Name(), m)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}

	t.Run("non-existent file", func(t *testing.T) {
		_, err := ParseAllowListJSON("non_existent_file.json", mockQueryClient)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no such file or directory")
	})
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
