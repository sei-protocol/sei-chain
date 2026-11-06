package retiredtokenfactory_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	seiapp "github.com/sei-protocol/sei-chain/app"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/app/retiredtokenfactory"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
)

var appEncodings = []struct {
	name   string
	config func() appparams.EncodingConfig
}{
	{"app", seiapp.MakeEncodingConfig},
	{"legacy app", seiapp.MakeLegacyEncodingConfig},
}

var (
	sender = sdk.AccAddress("tokenfactory-sender1")
	admin  = sdk.AccAddress("tokenfactory-admin-1")
	denom  = "factory/" + sender.String() + "/subdenom"
)

// legacyMsg is the legacy amino surface every retired tokenfactory message keeps.
type legacyMsg interface {
	sdk.Msg
	Route() string
	Type() string
	GetSignBytes() []byte
	Marshal() ([]byte, error)
}

// retiredMsg is a retired tokenfactory message with every field set, its
// encoding under tokenfactory/tx.proto written field by field rather than by
// the retired type, and the identity and sign bytes the removed module gave it.
type retiredMsg struct {
	msg       legacyMsg
	encoded   []byte
	typeURL   string
	msgType   string
	signBytes string
}

func appendString(b []byte, field protowire.Number, s string) []byte {
	return protowire.AppendString(protowire.AppendTag(b, field, protowire.BytesType), s)
}

func appendMessage(t *testing.T, b []byte, field protowire.Number, m interface{ Marshal() ([]byte, error) }) []byte {
	t.Helper()
	bz, err := m.Marshal()
	require.NoError(t, err)
	return protowire.AppendBytes(protowire.AppendTag(b, field, protowire.BytesType), bz)
}

func retiredMsgs(t *testing.T) []retiredMsg {
	t.Helper()
	createAllowList := banktypes.AllowList{Addresses: []string{sender.String(), admin.String()}}
	updateAllowList := banktypes.AllowList{Addresses: []string{admin.String()}}
	mintAmount := sdk.NewInt64Coin(denom, 1000)
	burnAmount := sdk.NewInt64Coin(denom, 400)
	metadata := banktypes.Metadata{
		Description: "description",
		DenomUnits:  []*banktypes.DenomUnit{{Denom: denom, Aliases: []string{"alias"}}},
		Base:        denom,
		Display:     denom,
		Name:        "name",
		Symbol:      "SYM",
	}
	return []retiredMsg{
		{
			msg:       &retiredtokenfactory.MsgCreateDenom{Sender: sender.String(), Subdenom: "subdenom", AllowList: &createAllowList},
			encoded:   appendMessage(t, appendString(appendString(nil, 1, sender.String()), 2, "subdenom"), 3, &createAllowList),
			typeURL:   "/seiprotocol.seichain.tokenfactory.MsgCreateDenom",
			msgType:   "create_denom",
			signBytes: `{"type":"tokenfactory/MsgCreateDenom","value":{"allow_list":{"addresses":["sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak","sei1w3hkketwveskxar0wfuj6ctyd45kutf3ja4m2v"]},"sender":"sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak","subdenom":"subdenom"}}`,
		},
		{
			msg:       &retiredtokenfactory.MsgUpdateDenom{Sender: sender.String(), Denom: denom, AllowList: &updateAllowList},
			encoded:   appendMessage(t, appendString(appendString(nil, 1, sender.String()), 2, denom), 3, &updateAllowList),
			typeURL:   "/seiprotocol.seichain.tokenfactory.MsgUpdateDenom",
			msgType:   "update_denom",
			signBytes: `{"type":"tokenfactory/MsgUpdateDenom","value":{"allow_list":{"addresses":["sei1w3hkketwveskxar0wfuj6ctyd45kutf3ja4m2v"]},"denom":"factory/sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak/subdenom","sender":"sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak"}}`,
		},
		{
			msg:       &retiredtokenfactory.MsgMint{Sender: sender.String(), Amount: mintAmount},
			encoded:   appendMessage(t, appendString(nil, 1, sender.String()), 2, &mintAmount),
			typeURL:   "/seiprotocol.seichain.tokenfactory.MsgMint",
			msgType:   "mint",
			signBytes: `{"type":"tokenfactory/MsgMint","value":{"amount":{"amount":"1000","denom":"factory/sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak/subdenom"},"sender":"sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak"}}`,
		},
		{
			msg:       &retiredtokenfactory.MsgBurn{Sender: sender.String(), Amount: burnAmount},
			encoded:   appendMessage(t, appendString(nil, 1, sender.String()), 2, &burnAmount),
			typeURL:   "/seiprotocol.seichain.tokenfactory.MsgBurn",
			msgType:   "burn",
			signBytes: `{"type":"tokenfactory/MsgBurn","value":{"amount":{"amount":"400","denom":"factory/sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak/subdenom"},"sender":"sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak"}}`,
		},
		{
			msg:       &retiredtokenfactory.MsgChangeAdmin{Sender: sender.String(), Denom: denom, NewAdmin: admin.String()},
			encoded:   appendString(appendString(appendString(nil, 1, sender.String()), 2, denom), 3, admin.String()),
			typeURL:   "/seiprotocol.seichain.tokenfactory.MsgChangeAdmin",
			msgType:   "change_admin",
			signBytes: `{"type":"tokenfactory/MsgChangeAdmin","value":{"denom":"factory/sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak/subdenom","new_admin":"sei1w3hkketwveskxar0wfuj6ctyd45kutf3ja4m2v","sender":"sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak"}}`,
		},
		{
			msg:       &retiredtokenfactory.MsgSetDenomMetadata{Sender: sender.String(), Metadata: metadata},
			encoded:   appendMessage(t, appendString(nil, 1, sender.String()), 2, &metadata),
			typeURL:   "/seiprotocol.seichain.tokenfactory.MsgSetDenomMetadata",
			msgType:   "set_denom_metadata",
			signBytes: `{"type":"tokenfactory/MsgSetDenomMetadata","value":{"metadata":{"base":"factory/sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak/subdenom","denom_units":[{"aliases":["alias"],"denom":"factory/sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak/subdenom"}],"description":"description","display":"factory/sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak/subdenom","name":"name","symbol":"SYM"},"sender":"sei1w3hkketwveskxar0wfuj6um9dejx2u33s6j3ak"}}`,
		},
	}
}

// txCarrying encodes a transaction whose only message is encoded under
// typeURL, with a placeholder signature.
func txCarrying(t *testing.T, typeURL string, encoded []byte) []byte {
	t.Helper()
	body, err := (&txtypes.TxBody{
		Messages: []*codectypes.Any{{TypeUrl: typeURL, Value: encoded}},
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

// TestRetiredMsgsDecode decodes a transaction carrying each retired message
// with every field set through both app transaction configs, and requires the
// retired type to carry every field and encode it back byte for byte.
func TestRetiredMsgsDecode(t *testing.T) {
	for _, retired := range retiredMsgs(t) {
		txBytes := txCarrying(t, retired.typeURL, retired.encoded)
		for _, encoding := range appEncodings {
			t.Run(retired.msgType+"/"+encoding.name, func(t *testing.T) {
				tx, err := encoding.config().TxConfig.TxDecoder()(txBytes)
				require.NoError(t, err)
				require.Len(t, tx.GetMsgs(), 1)
				got, ok := tx.GetMsgs()[0].(legacyMsg)
				require.True(t, ok, "decoded %T", tx.GetMsgs()[0])
				require.Equal(t, retired.msg, got)
				reencoded, err := got.Marshal()
				require.NoError(t, err)
				require.Equal(t, retired.encoded, reencoded)
			})
		}
	}
}

// TestRetiredMsgsKeepTheirLegacyIdentity requires each retired message to keep
// the route, type and amino sign bytes the removed module gave it, so signatures
// over historical transactions still verify.
func TestRetiredMsgsKeepTheirLegacyIdentity(t *testing.T) {
	for _, retired := range retiredMsgs(t) {
		t.Run(retired.msgType, func(t *testing.T) {
			require.Equal(t, retiredtokenfactory.ModuleName, retired.msg.Route())
			require.Equal(t, retired.msgType, retired.msg.Type())
			require.Equal(t, retired.signBytes, string(retired.msg.GetSignBytes()))
		})
	}
}

// TestRetiredMsgsAreRefused requires each retired message to keep its type URL
// and signer, and to fail validation in codespace tokenfactory with code 26.
func TestRetiredMsgsAreRefused(t *testing.T) {
	for _, retired := range retiredMsgs(t) {
		t.Run(retired.msgType, func(t *testing.T) {
			require.Equal(t, retired.typeURL, sdk.MsgTypeURL(retired.msg))
			require.Equal(t, []sdk.AccAddress{sender}, retired.msg.GetSigners())

			err := retired.msg.ValidateBasic()
			require.ErrorIs(t, err, retiredtokenfactory.ErrDeprecated)
			codespace, code, _ := sdkerrors.ABCIInfo(err, false)
			require.Equal(t, "tokenfactory", codespace)
			require.EqualValues(t, 26, code)
		})
	}
}

// TestRetiredTokenfactoryDoesNotRestoreModule requires the tokenfactory module
// to stay unregistered.
func TestRetiredTokenfactoryDoesNotRestoreModule(t *testing.T) {
	_, registered := seiapp.ModuleBasics[retiredtokenfactory.ModuleName]
	require.False(t, registered)
}
