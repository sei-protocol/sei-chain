package retiredauthz_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/app/retiredauthz"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

const (
	// pacific1ExecTxHex is the pacific-1 transaction pacific1ExecTxHash, included
	// at height 229,297,272, in which a restaking operator delegated for four
	// granters through one MsgExec.
	pacific1ExecTxHash = "08F5465F862B567C295B9B7367E14CB064200BA81652E728EEB8EB2E70EC0A87"
	pacific1ExecTxHex  = "0acf050ab2050a1d2f636f736d6f732e617574687a2e763162657461312e4d7367457865631290050a2a73656931773464753079363730357271726567666c36636b746d37737764733674346473736b72686d681296010a232f636f736d6f732e7374616b696e672e763162657461312e4d736744656c6567617465126f0a2a7365693135703264303932376c7678367434367570676b793837657676636e38646c68353672366b6d7a123173656976616c6f70657231376761396a6861343567753078303479726d66706a706b736d716534387775707a6b356a65781a0e0a047573656912063137303235301296010a232f636f736d6f732e7374616b696e672e763162657461312e4d736744656c6567617465126f0a2a7365693130796a346b636763357a67716763303834647a686b3476726a6a68793930306e347933373079123173656976616c6f70657231376761396a6861343567753078303479726d66706a706b736d716534387775707a6b356a65781a0e0a047573656912063333383331341296010a232f636f736d6f732e7374616b696e672e763162657461312e4d736744656c6567617465126f0a2a736569316836637330616a616d6d6d776767396d343364797132673279396165633973393076796c6e61123173656976616c6f70657231376761396a6861343567753078303479726d66706a706b736d716534387775707a6b356a65781a0e0a047573656912063131353432351296010a232f636f736d6f732e7374616b696e672e763162657461312e4d736744656c6567617465126f0a2a7365693130683234656d6e3832383076736e6b32686a7a6b647339366c686e343471786170377a7a3938123173656976616c6f70657231376761396a6861343567753078303479726d66706a706b736d716534387775707a6b356a65781a0e0a04757365691206313035353832121852455374616b656420627920416374697665204e6f64657312680a510a460a1f2f636f736d6f732e63727970746f2e736563703235366b312e5075624b657912230a2102bfbcb296cd31bc2d8b32edc4193c6c3be7f7742d874ccda12457af687392eb8112040a02080118b70812130a0d0a04757365691205363039303210fb95251a402b28124da0f339bdcecfe61172c81ca3efba392d195e786e508895e898380fa958692216d16574f29e7bd1534354e9e792107c3add36d532f3d7e9c253b655af"

	// pacific1GrantTxHex is the pacific-1 transaction pacific1GrantTxHash,
	// included at height 229,203,392, which granted a StakeAuthorization.
	pacific1GrantTxHash = "7CD6EB264F059C2A72D1E75126DD36306F551B19EBE96972E31BA3EB65A4FFB8"
	pacific1GrantTxHex  = "0aef010aec010a1e2f636f736d6f732e617574687a2e763162657461312e4d73674772616e7412c9010a2a736569316135726b7a686638733979347a7176766b743470323474336775727a39326875776838353532122a73656931327870713638636177326171647537306a6d346b3739326730763970726872706430686a78651a6f0a650a2a2f636f736d6f732e7374616b696e672e763162657461312e5374616b65417574686f72697a6174696f6e123712330a3173656976616c6f7065723134306c36793267703367787661793671746e37307265377a327330676e35377a6c366e7570732001120608f0afd2e30612660a500a460a1f2f636f736d6f732e63727970746f2e736563703235366b312e5075624b657912230a2102db3c0cbe9c2e14cbb07d929a47ec6965f29c822ddae47d155d1411f0954e9b4e12040a020801180212120a0c0a047573656912043230353710aca3061a401d3ed886f76b1bb084a438eb2a8f5fbbd68abcf43e9de76dc1c48511ac8fbb515d681a1fb51fff7dad683be75faa0f101c614381255ad9006cb1fd6babc6a090"
)

var appEncodings = []struct {
	name   string
	config func() appparams.EncodingConfig
}{
	{"app", seiapp.MakeEncodingConfig},
	{"legacy app", seiapp.MakeLegacyEncodingConfig},
}

var (
	granter = sdk.AccAddress("authz-granter-addr-1")
	grantee = sdk.AccAddress("authz-grantee-addr-1")
)

// legacyMsg is the legacy amino surface every retired authz message keeps.
type legacyMsg interface {
	sdk.Msg
	Route() string
	Type() string
	GetSignBytes() []byte
	Marshal() ([]byte, error)
}

// retiredMsg is a retired authz message as the removed module encoded it, with
// the signer, amino sign bytes and protobuf JSON the removed module gave it. A
// grant carrying a StakeAuthorization has no sign bytes: amino cannot encode its
// validators oneof, so the removed module panicked producing them.
type retiredMsg struct {
	name      string
	typeURL   string
	encoded   string
	signer    sdk.AccAddress
	signBytes string
	json      string
}

// retiredMsgs covers every message type, every authorization type a grant can
// carry, and an exec nesting a grant. Every expected value was captured from
// the removed module.
var retiredMsgs = []retiredMsg{
	{
		name:      "grant generic",
		typeURL:   "/cosmos.authz.v1beta1.MsgGrant",
		encoded:   "0a2a73656931763936686736723639346e687963747777336a687974747076336a387974663363326b687537122a73656931763936686736723639346e687963747777336a6b3274747076336a38797466336864367471741a560a4c0a2a2f636f736d6f732e617574687a2e763162657461312e47656e65726963417574686f72697a6174696f6e121e0a1c2f636f736d6f732e62616e6b2e763162657461312e4d736753656e64120608a5aaf58607",
		signer:    granter,
		signBytes: `{"grant":{"authorization":{"msg":"/cosmos.bank.v1beta1.MsgSend"},"expiration":"2030-01-02T03:04:05Z"},"grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7"}`,
		json:      `{"granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","grant":{"authorization":{"@type":"/cosmos.authz.v1beta1.GenericAuthorization","msg":"/cosmos.bank.v1beta1.MsgSend"},"expiration":"2030-01-02T03:04:05Z"}}`,
	},
	{
		name:      "grant send",
		typeURL:   "/cosmos.authz.v1beta1.MsgGrant",
		encoded:   "0a2a73656931763936686736723639346e687963747777336a687974747076336a387974663363326b687537122a73656931763936686736723639346e687963747777336a6b3274747076336a38797466336864367471741a400a360a262f636f736d6f732e62616e6b2e763162657461312e53656e64417574686f72697a6174696f6e120c0a0a0a047573656912023130120608a5aaf58607",
		signer:    granter,
		signBytes: `{"grant":{"authorization":{"spend_limit":[{"amount":"10","denom":"usei"}]},"expiration":"2030-01-02T03:04:05Z"},"grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7"}`,
		json:      `{"granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","grant":{"authorization":{"@type":"/cosmos.bank.v1beta1.SendAuthorization","spend_limit":[{"denom":"usei","amount":"10"}]},"expiration":"2030-01-02T03:04:05Z"}}`,
	},
	{
		name:    "grant stake allow list",
		typeURL: "/cosmos.authz.v1beta1.MsgGrant",
		encoded: "0a2a73656931763936686736723639346e687963747777336a687974747076336a387974663363326b687537122a73656931763936686736723639346e687963747777336a6b3274747076336a38797466336864367471741a7c0a720a2a2f636f736d6f732e7374616b696e672e763162657461312e5374616b65417574686f72697a6174696f6e12440a0b0a0475736569120331303012330a3173656976616c6f70657231763936686736723639346d787a6d726676337368676d6d6a3934736b6765726a326d307371342001120608a5aaf58607",
		signer:  granter,
		json:    `{"granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","grant":{"authorization":{"@type":"/cosmos.staking.v1beta1.StakeAuthorization","max_tokens":{"denom":"usei","amount":"100"},"allow_list":{"address":["seivaloper1v96hg6r694mxzmrfv3shgmmj94skgerj2m0sq4"]},"authorization_type":"AUTHORIZATION_TYPE_DELEGATE"},"expiration":"2030-01-02T03:04:05Z"}}`,
	},
	{
		name:    "grant stake deny list",
		typeURL: "/cosmos.authz.v1beta1.MsgGrant",
		encoded: "0a2a73656931763936686736723639346e687963747777336a687974747076336a387974663363326b687537122a73656931763936686736723639346e687963747777336a6b3274747076336a38797466336864367471741a6f0a650a2a2f636f736d6f732e7374616b696e672e763162657461312e5374616b65417574686f72697a6174696f6e12371a330a3173656976616c6f70657231763936686736723639346d787a6d726676337368676d6d6a3934736b6765726a326d307371342002120608a5aaf58607",
		signer:  granter,
		json:    `{"granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","grant":{"authorization":{"@type":"/cosmos.staking.v1beta1.StakeAuthorization","max_tokens":null,"deny_list":{"address":["seivaloper1v96hg6r694mxzmrfv3shgmmj94skgerj2m0sq4"]},"authorization_type":"AUTHORIZATION_TYPE_UNDELEGATE"},"expiration":"2030-01-02T03:04:05Z"}}`,
	},
	{
		name:      "exec",
		typeURL:   "/cosmos.authz.v1beta1.MsgExec",
		encoded:   "0a2a73656931763936686736723639346e687963747777336a6b3274747076336a38797466336864367471741283010a1c2f636f736d6f732e62616e6b2e763162657461312e4d736753656e6412630a2a73656931763936686736723639346e687963747777336a687974747076336a387974663363326b687537122a73656931763936686736723639346e687963747777336a6b3274747076336a38797466336864367471741a090a0475736569120137",
		signer:    grantee,
		signBytes: `{"grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","msgs":[{"amount":[{"amount":"7","denom":"usei"}],"from_address":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","to_address":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt"}]}`,
		json:      `{"grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","msgs":[{"@type":"/cosmos.bank.v1beta1.MsgSend","from_address":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","to_address":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","amount":[{"denom":"usei","amount":"7"}]}]}`,
	},
	{
		name:      "exec nesting a grant",
		typeURL:   "/cosmos.authz.v1beta1.MsgExec",
		encoded:   "0a2a73656931763936686736723639346e687963747777336a6b3274747076336a387974663368643674717412d3010a1e2f636f736d6f732e617574687a2e763162657461312e4d73674772616e7412b0010a2a73656931763936686736723639346e687963747777336a687974747076336a387974663363326b687537122a73656931763936686736723639346e687963747777336a6b3274747076336a38797466336864367471741a560a4c0a2a2f636f736d6f732e617574687a2e763162657461312e47656e65726963417574686f72697a6174696f6e121e0a1c2f636f736d6f732e62616e6b2e763162657461312e4d736753656e64120608a5aaf58607",
		signer:    grantee,
		signBytes: `{"grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","msgs":[{"grant":{"authorization":{"msg":"/cosmos.bank.v1beta1.MsgSend"},"expiration":"2030-01-02T03:04:05Z"},"grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7"}]}`,
		json:      `{"grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","msgs":[{"@type":"/cosmos.authz.v1beta1.MsgGrant","granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","grant":{"authorization":{"@type":"/cosmos.authz.v1beta1.GenericAuthorization","msg":"/cosmos.bank.v1beta1.MsgSend"},"expiration":"2030-01-02T03:04:05Z"}}]}`,
	},
	{
		name:      "revoke",
		typeURL:   "/cosmos.authz.v1beta1.MsgRevoke",
		encoded:   "0a2a73656931763936686736723639346e687963747777336a687974747076336a387974663363326b687537122a73656931763936686736723639346e687963747777336a6b3274747076336a38797466336864367471741a1c2f636f736d6f732e62616e6b2e763162657461312e4d736753656e64",
		signer:    granter,
		signBytes: `{"grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","msg_type_url":"/cosmos.bank.v1beta1.MsgSend"}`,
		json:      `{"granter":"sei1v96hg6r694nhyctww3jhyttpv3j8ytf3c2khu7","grantee":"sei1v96hg6r694nhyctww3jk2ttpv3j8ytf3hd6tqt","msg_type_url":"/cosmos.bank.v1beta1.MsgSend"}`,
	},
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

// decodeRetired decodes a transaction carrying retired through the transaction
// config of config and returns its only message.
func decodeRetired(t *testing.T, config appparams.EncodingConfig, retired retiredMsg) legacyMsg {
	t.Helper()
	encoded, err := hex.DecodeString(retired.encoded)
	require.NoError(t, err)
	tx, err := config.TxConfig.TxDecoder()(txCarrying(t, retired.typeURL, encoded))
	require.NoError(t, err)
	require.Len(t, tx.GetMsgs(), 1)
	msg, ok := tx.GetMsgs()[0].(legacyMsg)
	require.True(t, ok, "decoded %T", tx.GetMsgs()[0])
	return msg
}

// TestPacific1AuthzTransactionsDecode decodes a MsgExec and a MsgGrant
// transaction pacific-1 included through both app transaction configs, and
// requires the retired type to unpack what the message carries and encode it
// back to the bytes the chain included.
func TestPacific1AuthzTransactionsDecode(t *testing.T) {
	fixtures := []struct {
		name   string
		hash   string
		txHex  string
		verify func(t *testing.T, msg sdk.Msg)
	}{
		{"exec", pacific1ExecTxHash, pacific1ExecTxHex, func(t *testing.T, msg sdk.Msg) {
			exec, ok := msg.(*retiredauthz.MsgExec)
			require.True(t, ok, "decoded %T", msg)
			require.Len(t, exec.Msgs, 4)
			for _, authorized := range exec.Msgs {
				require.IsType(t, &stakingtypes.MsgDelegate{}, authorized.GetCachedValue())
			}
		}},
		{"grant", pacific1GrantTxHash, pacific1GrantTxHex, func(t *testing.T, msg sdk.Msg) {
			grant, ok := msg.(*retiredauthz.MsgGrant)
			require.True(t, ok, "decoded %T", msg)
			require.IsType(t, &stakingtypes.StakeAuthorization{}, grant.Grant.Authorization.GetCachedValue())
		}},
	}
	for _, fixture := range fixtures {
		txBytes, err := hex.DecodeString(fixture.txHex)
		require.NoError(t, err)
		digest := sha256.Sum256(txBytes)
		require.Equal(t, fixture.hash, strings.ToUpper(hex.EncodeToString(digest[:])))
		var raw txtypes.TxRaw
		require.NoError(t, raw.Unmarshal(txBytes))
		var body txtypes.TxBody
		require.NoError(t, body.Unmarshal(raw.BodyBytes))
		require.Len(t, body.Messages, 1)

		for _, encoding := range appEncodings {
			t.Run(fixture.name+"/"+encoding.name, func(t *testing.T) {
				tx, err := encoding.config().TxConfig.TxDecoder()(txBytes)
				require.NoError(t, err)
				require.Len(t, tx.GetMsgs(), 1)
				fixture.verify(t, tx.GetMsgs()[0])
				reencoded, err := tx.GetMsgs()[0].(legacyMsg).Marshal()
				require.NoError(t, err)
				require.Equal(t, body.Messages[0].Value, reencoded)
			})
		}
	}
}

// TestRetiredMsgsDecode decodes a transaction carrying each retired message
// through both app transaction configs, and requires the retired type to encode
// it back byte for byte and to render the protobuf JSON the removed module did.
func TestRetiredMsgsDecode(t *testing.T) {
	for _, retired := range retiredMsgs {
		for _, encoding := range appEncodings {
			t.Run(retired.name+"/"+encoding.name, func(t *testing.T) {
				config := encoding.config()
				msg := decodeRetired(t, config, retired)
				require.Equal(t, retired.typeURL, sdk.MsgTypeURL(msg))
				reencoded, err := msg.Marshal()
				require.NoError(t, err)
				require.Equal(t, retired.encoded, hex.EncodeToString(reencoded))
				json, err := config.Marshaler.MarshalAsJSON(msg)
				require.NoError(t, err)
				require.Equal(t, retired.json, string(json))
			})
		}
	}
}

// TestRetiredMsgsKeepTheirLegacyIdentity requires each decoded retired message
// to keep the route, type, signer and amino sign bytes the removed module gave
// it, so signatures over historical transactions still verify.
func TestRetiredMsgsKeepTheirLegacyIdentity(t *testing.T) {
	for _, retired := range retiredMsgs {
		t.Run(retired.name, func(t *testing.T) {
			msg := decodeRetired(t, seiapp.MakeEncodingConfig(), retired)
			require.Equal(t, retired.typeURL, msg.Route())
			require.Equal(t, retired.typeURL, msg.Type())
			require.Equal(t, []sdk.AccAddress{retired.signer}, msg.GetSigners())
			if retired.signBytes == "" {
				require.Panics(t, func() { msg.GetSignBytes() })
				return
			}
			require.Equal(t, retired.signBytes, string(msg.GetSignBytes()))
		})
	}
}

// TestRetiredMsgsAreRefused requires each retired message to fail validation in
// codespace authz with code 2.
func TestRetiredMsgsAreRefused(t *testing.T) {
	for _, retired := range retiredMsgs {
		t.Run(retired.name, func(t *testing.T) {
			err := decodeRetired(t, seiapp.MakeEncodingConfig(), retired).ValidateBasic()
			require.ErrorIs(t, err, retiredauthz.ErrDeprecated)
			codespace, code, _ := sdkerrors.ABCIInfo(err, false)
			require.Equal(t, "authz", codespace)
			require.EqualValues(t, 2, code)
		})
	}
}

// TestRetiredAuthzDoesNotRestoreModule requires the authz module to stay
// unregistered.
func TestRetiredAuthzDoesNotRestoreModule(t *testing.T) {
	_, registered := seiapp.ModuleBasics[retiredauthz.ModuleName]
	require.False(t, registered)
}
