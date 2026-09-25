package retiredvesting_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	seiapp "github.com/sei-protocol/sei-chain/app"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/app/retiredvesting"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	authtestutil "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/testutil"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

const (
	msgCreateVestingAccountTypeURL = "/cosmos.vesting.v1beta1.MsgCreateVestingAccount"
	// vestingEndTime is 3000-01-01, the end time of every vesting account on pacific-1.
	vestingEndTime int64 = 32_503_680_000
)

// historicalMsgCreateVestingAccount returns a MsgCreateVestingAccount with every
// field set, and its encoding under cosmos/vesting/v1beta1/tx.proto written
// field by field rather than by the retired type.
func historicalMsgCreateVestingAccount(t *testing.T) (*retiredvesting.MsgCreateVestingAccount, []byte) {
	t.Helper()
	msg := &retiredvesting.MsgCreateVestingAccount{
		FromAddress: sdk.AccAddress("vesting-from-address").String(),
		ToAddress:   sdk.AccAddress("vesting-to---address").String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100_000)),
		EndTime:     vestingEndTime,
		Delayed:     true,
		Admin:       sdk.AccAddress("vesting-admin-addres").String(),
	}
	coin, err := msg.Amount[0].Marshal()
	require.NoError(t, err)
	encoded := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), msg.FromAddress)
	encoded = protowire.AppendString(protowire.AppendTag(encoded, 2, protowire.BytesType), msg.ToAddress)
	encoded = protowire.AppendBytes(protowire.AppendTag(encoded, 3, protowire.BytesType), coin)
	encoded = protowire.AppendVarint(protowire.AppendTag(encoded, 4, protowire.VarintType), uint64(vestingEndTime))
	encoded = protowire.AppendVarint(protowire.AppendTag(encoded, 5, protowire.VarintType), 1)
	encoded = protowire.AppendString(protowire.AppendTag(encoded, 6, protowire.BytesType), msg.Admin)
	return msg, encoded
}

// txCarrying encodes a transaction whose only message is the
// MsgCreateVestingAccount encoding msg, with a placeholder signature.
func txCarrying(t *testing.T, msg []byte) []byte {
	t.Helper()
	body, err := (&txtypes.TxBody{
		Messages: []*codectypes.Any{{TypeUrl: msgCreateVestingAccountTypeURL, Value: msg}},
	}).Marshal()
	require.NoError(t, err)
	authInfo, err := (&txtypes.AuthInfo{
		Fee: &txtypes.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("usei", 200_000)), GasLimit: 1_000_000},
	}).Marshal()
	require.NoError(t, err)
	txBytes, err := (&txtypes.TxRaw{
		BodyBytes:     body,
		AuthInfoBytes: authInfo,
		Signatures:    [][]byte{make([]byte, 64)},
	}).Marshal()
	require.NoError(t, err)
	return txBytes
}

// TestHistoricalMsgCreateVestingAccountDecodes decodes a transaction carrying a
// MsgCreateVestingAccount through both app transaction configs, and requires
// the retired type to carry every field and encode it back byte for byte.
func TestHistoricalMsgCreateVestingAccountDecodes(t *testing.T) {
	want, encoded := historicalMsgCreateVestingAccount(t)
	txBytes := txCarrying(t, encoded)
	for _, encoding := range []struct {
		name   string
		config func() appparams.EncodingConfig
	}{
		{"app", seiapp.MakeEncodingConfig},
		{"legacy app", seiapp.MakeLegacyEncodingConfig},
	} {
		t.Run(encoding.name, func(t *testing.T) {
			tx, err := encoding.config().TxConfig.TxDecoder()(txBytes)
			require.NoError(t, err)
			require.Len(t, tx.GetMsgs(), 1)
			got, ok := tx.GetMsgs()[0].(*retiredvesting.MsgCreateVestingAccount)
			require.True(t, ok, "decoded %T", tx.GetMsgs()[0])
			require.True(t, want.Equal(got), "decoded %v", got)
			reencoded, err := got.Marshal()
			require.NoError(t, err)
			require.Equal(t, encoded, reencoded)
		})
	}
}

// TestRetiredMsgCreateVestingAccountIsRefused requires the retired message to
// keep its type URL and signer, and to fail validation in codespace vesting
// with code 2.
func TestRetiredMsgCreateVestingAccountIsRefused(t *testing.T) {
	msg, _ := historicalMsgCreateVestingAccount(t)
	require.Equal(t, msgCreateVestingAccountTypeURL, sdk.MsgTypeURL(msg))
	require.Equal(t, []sdk.AccAddress{sdk.AccAddress("vesting-from-address")}, msg.GetSigners())

	err := msg.ValidateBasic()
	require.ErrorIs(t, err, retiredvesting.ErrDeprecated)
	codespace, code, _ := sdkerrors.ABCIInfo(err, false)
	require.Equal(t, "vesting", codespace)
	require.EqualValues(t, 2, code)
}

// TestRetiredVestingDoesNotRestoreAccountsOrModule requires the app codec to
// keep refusing every vesting account type, and the vesting module to stay
// unregistered.
func TestRetiredVestingDoesNotRestoreAccountsOrModule(t *testing.T) {
	registry := seiapp.MakeEncodingConfig().InterfaceRegistry
	for _, typeURL := range authtestutil.LegacyVestingAccountTypeURLs {
		var account authtypes.AccountI
		err := registry.UnpackAny(&codectypes.Any{TypeUrl: typeURL}, &account)
		require.ErrorContains(t, err, "no concrete type registered", typeURL)
	}
	_, registered := seiapp.ModuleBasics[retiredvesting.ModuleName]
	require.False(t, registered)
}
