package retiredvesting_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
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

	// pacific1VestingTxHex is the pacific-1 transaction pacific1VestingTxHash,
	// included at height 228,422,853, which created a delayed vesting account.
	pacific1VestingTxHash = "2838170E42486FE03E4765DDD0C8BC3559AADB1F09B2882C66C45B5906940709"
	pacific1VestingTxHex  = "0aa6010aa3010a2f2f636f736d6f732e76657374696e672e763162657461312e4d736743726561746556657374696e674163636f756e7412700a2a736569316d366d30323634303930653761666e7030716b32616d766565306339363434637437756c6836122a7365693167613833336d6a366a633664326c7965366a3834737475673968326d7078637875797735616b1a0e0a04757365691206313030303030208098fb8a79280112650a4e0a460a1f2f636f736d6f732e63727970746f2e736563703235366b312e5075624b657912230a2102b4f18f4ea5bc446a46d82d42d0abfe9b1fa4397b037cf7da5ca07dd1c967ace612040a02080112130a0d0a04757365691205333030303010e0a7121a409963a065d4c35e70639703a1d086dfe399ebe8df345fd583ad388055c4d5dba22af9a57c29e026e014d436a67e097ec7aff741cfe9c5ccf7d7fc91a0774187ec"
)

var appEncodings = []struct {
	name   string
	config func() appparams.EncodingConfig
}{
	{"app", seiapp.MakeEncodingConfig},
	{"legacy app", seiapp.MakeLegacyEncodingConfig},
}

// msgCreateVestingAccountWithEveryField returns a MsgCreateVestingAccount with
// every field set, including the admin the pacific-1 fixture leaves unset, and
// its encoding under cosmos/vesting/v1beta1/tx.proto written field by field
// rather than by the retired type.
func msgCreateVestingAccountWithEveryField(t *testing.T) (*retiredvesting.MsgCreateVestingAccount, []byte) {
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

// decodeMsgCreateVestingAccount decodes txBytes with the transaction config of
// config and returns its only message, which must be a MsgCreateVestingAccount.
func decodeMsgCreateVestingAccount(t *testing.T, config func() appparams.EncodingConfig, txBytes []byte) *retiredvesting.MsgCreateVestingAccount {
	t.Helper()
	tx, err := config().TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	require.Len(t, tx.GetMsgs(), 1)
	msg, ok := tx.GetMsgs()[0].(*retiredvesting.MsgCreateVestingAccount)
	require.True(t, ok, "decoded %T", tx.GetMsgs()[0])
	return msg
}

// TestPacific1MsgCreateVestingAccountDecodes decodes a MsgCreateVestingAccount
// transaction pacific-1 included through both app transaction configs, and
// requires the retired type to carry the fields it set and encode the message
// back to the bytes the chain included.
func TestPacific1MsgCreateVestingAccountDecodes(t *testing.T) {
	txBytes, err := hex.DecodeString(pacific1VestingTxHex)
	require.NoError(t, err)
	digest := sha256.Sum256(txBytes)
	require.Equal(t, pacific1VestingTxHash, strings.ToUpper(hex.EncodeToString(digest[:])))
	var raw txtypes.TxRaw
	require.NoError(t, raw.Unmarshal(txBytes))
	var body txtypes.TxBody
	require.NoError(t, body.Unmarshal(raw.BodyBytes))
	require.Len(t, body.Messages, 1)

	want := &retiredvesting.MsgCreateVestingAccount{
		FromAddress: "sei1m6m0264090e7afnp0qk2amvee0c9644ct7ulh6",
		ToAddress:   "sei1ga833mj6jc6d2lye6j84stug9h2mpxcxuyw5ak",
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 100_000)),
		EndTime:     vestingEndTime,
		Delayed:     true,
	}
	for _, encoding := range appEncodings {
		t.Run(encoding.name, func(t *testing.T) {
			got := decodeMsgCreateVestingAccount(t, encoding.config, txBytes)
			require.True(t, want.Equal(got), "decoded %v", got)
			reencoded, err := got.Marshal()
			require.NoError(t, err)
			require.Equal(t, body.Messages[0].Value, reencoded)
		})
	}
}

// TestMsgCreateVestingAccountWithEveryFieldDecodes decodes a transaction
// carrying a MsgCreateVestingAccount with every field set through both app
// transaction configs, and requires the retired type to carry every field and
// encode it back byte for byte.
func TestMsgCreateVestingAccountWithEveryFieldDecodes(t *testing.T) {
	want, encoded := msgCreateVestingAccountWithEveryField(t)
	txBytes := txCarrying(t, encoded)
	for _, encoding := range appEncodings {
		t.Run(encoding.name, func(t *testing.T) {
			got := decodeMsgCreateVestingAccount(t, encoding.config, txBytes)
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
	msg, _ := msgCreateVestingAccountWithEveryField(t)
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
