package types

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/x/auth/legacy/legacytx"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const firstCodeID = 1

func TestStoreCodeValidation(t *testing.T) {
	bad, err := sdk.AccAddressFromHex("012345")
	require.NoError(t, err)
	badAddress := bad.String()
	// proper address size
	goodAddress := sdk.AccAddress(make([]byte, ContractAddrLen)).String()
	sdk.GetConfig().SetAddressVerifier(VerifyAddressLen())
	cases := map[string]struct {
		msg   MsgStoreCode
		valid bool
	}{
		"empty": {
			msg:   MsgStoreCode{},
			valid: false,
		},
		"correct minimal": {
			msg: MsgStoreCode{
				Sender:       goodAddress,
				WASMByteCode: []byte("foo"),
			},
			valid: true,
		},
		"missing code": {
			msg: MsgStoreCode{
				Sender: goodAddress,
			},
			valid: false,
		},
		"bad sender minimal": {
			msg: MsgStoreCode{
				Sender:       badAddress,
				WASMByteCode: []byte("foo"),
			},
			valid: false,
		},
		"correct maximal": {
			msg: MsgStoreCode{
				Sender:       goodAddress,
				WASMByteCode: []byte("foo"),
			},
			valid: true,
		},
		"invalid InstantiatePermission": {
			msg: MsgStoreCode{
				Sender:                goodAddress,
				WASMByteCode:          []byte("foo"),
				InstantiatePermission: &AccessConfig{Permission: AccessTypeOnlyAddress, Address: badAddress},
			},
			valid: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestInstantiateContractValidation(t *testing.T) {
	bad, err := sdk.AccAddressFromHex("012345")
	require.NoError(t, err)
	badAddress := bad.String()
	// proper address size
	goodAddress := sdk.AccAddress(make([]byte, 20)).String()

	cases := map[string]struct {
		msg   MsgInstantiateContract
		valid bool
	}{
		"empty": {
			msg:   MsgInstantiateContract{},
			valid: false,
		},
		"correct minimal": {
			msg: MsgInstantiateContract{
				Sender: goodAddress,
				CodeID: firstCodeID,
				Label:  "foo",
				Msg:    []byte("{}"),
			},
			valid: true,
		},
		"missing code": {
			msg: MsgInstantiateContract{
				Sender: goodAddress,
				Label:  "foo",
				Msg:    []byte("{}"),
			},
			valid: false,
		},
		"missing label": {
			msg: MsgInstantiateContract{
				Sender: goodAddress,
				Msg:    []byte("{}"),
			},
			valid: false,
		},
		"label too long": {
			msg: MsgInstantiateContract{
				Sender: goodAddress,
				Label:  strings.Repeat("food", 33),
			},
			valid: false,
		},
		"bad sender minimal": {
			msg: MsgInstantiateContract{
				Sender: badAddress,
				CodeID: firstCodeID,
				Label:  "foo",
				Msg:    []byte("{}"),
			},
			valid: false,
		},
		"correct maximal": {
			msg: MsgInstantiateContract{
				Sender: goodAddress,
				CodeID: firstCodeID,
				Label:  "foo",
				Msg:    []byte(`{"some": "data"}`),
				Funds:  sdk.Coins{sdk.Coin{Denom: "foobar", Amount: sdk.NewInt(200)}},
			},
			valid: true,
		},
		"negative funds": {
			msg: MsgInstantiateContract{
				Sender: goodAddress,
				CodeID: firstCodeID,
				Label:  "foo",
				Msg:    []byte(`{"some": "data"}`),
				// we cannot use sdk.NewCoin() constructors as they panic on creating invalid data (before we can test)
				Funds: sdk.Coins{sdk.Coin{Denom: "foobar", Amount: sdk.NewInt(-200)}},
			},
			valid: false,
		},
		"non json init msg": {
			msg: MsgInstantiateContract{
				Sender: goodAddress,
				CodeID: firstCodeID,
				Label:  "foo",
				Msg:    []byte("invalid-json"),
			},
			valid: false,
		},
		"empty init msg": {
			msg: MsgInstantiateContract{
				Sender: goodAddress,
				CodeID: firstCodeID,
				Label:  "foo",
			},
			valid: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestExecuteContractValidation(t *testing.T) {
	bad, err := sdk.AccAddressFromHex("012345")
	require.NoError(t, err)
	badAddress := bad.String()
	// proper address size
	goodAddress := sdk.AccAddress(make([]byte, 20)).String()

	cases := map[string]struct {
		msg   MsgExecuteContract
		valid bool
	}{
		"empty": {
			msg:   MsgExecuteContract{},
			valid: false,
		},
		"correct minimal": {
			msg: MsgExecuteContract{
				Sender:   goodAddress,
				Contract: goodAddress,
				Msg:      []byte("{}"),
			},
			valid: true,
		},
		"correct all": {
			msg: MsgExecuteContract{
				Sender:   goodAddress,
				Contract: goodAddress,
				Msg:      []byte(`{"some": "data"}`),
				Funds:    sdk.Coins{sdk.Coin{Denom: "foobar", Amount: sdk.NewInt(200)}},
			},
			valid: true,
		},
		"bad sender": {
			msg: MsgExecuteContract{
				Sender:   badAddress,
				Contract: goodAddress,
				Msg:      []byte(`{"some": "data"}`),
			},
			valid: false,
		},
		"empty sender": {
			msg: MsgExecuteContract{
				Contract: goodAddress,
				Msg:      []byte(`{"some": "data"}`),
			},
			valid: false,
		},
		"bad contract": {
			msg: MsgExecuteContract{
				Sender:   goodAddress,
				Contract: badAddress,
				Msg:      []byte(`{"some": "data"}`),
			},
			valid: false,
		},
		"empty contract": {
			msg: MsgExecuteContract{
				Sender: goodAddress,
				Msg:    []byte(`{"some": "data"}`),
			},
			valid: false,
		},
		"negative funds": {
			msg: MsgExecuteContract{
				Sender:   goodAddress,
				Contract: goodAddress,
				Msg:      []byte(`{"some": "data"}`),
				Funds:    sdk.Coins{sdk.Coin{Denom: "foobar", Amount: sdk.NewInt(-1)}},
			},
			valid: false,
		},
		"duplicate funds": {
			msg: MsgExecuteContract{
				Sender:   goodAddress,
				Contract: goodAddress,
				Msg:      []byte(`{"some": "data"}`),
				Funds:    sdk.Coins{sdk.Coin{Denom: "foobar", Amount: sdk.NewInt(1)}, sdk.Coin{Denom: "foobar", Amount: sdk.NewInt(1)}},
			},
			valid: false,
		},
		"non json msg": {
			msg: MsgExecuteContract{
				Sender:   goodAddress,
				Contract: goodAddress,
				Msg:      []byte("invalid-json"),
			},
			valid: false,
		},
		"empty msg": {
			msg: MsgExecuteContract{
				Sender:   goodAddress,
				Contract: goodAddress,
			},
			valid: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestMsgUpdateAdministrator(t *testing.T) {
	bad, err := sdk.AccAddressFromHex("012345")
	require.NoError(t, err)
	badAddress := bad.String()
	// proper address size
	goodAddress := sdk.AccAddress(make([]byte, 20)).String()
	otherGoodAddress := sdk.AccAddress(bytes.Repeat([]byte{0x1}, 20)).String()
	anotherGoodAddress := sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20)).String()

	specs := map[string]struct {
		src    MsgUpdateAdmin
		expErr bool
	}{
		"all good": {
			src: MsgUpdateAdmin{
				Sender:   goodAddress,
				NewAdmin: otherGoodAddress,
				Contract: anotherGoodAddress,
			},
		},
		"new admin required": {
			src: MsgUpdateAdmin{
				Sender:   goodAddress,
				Contract: anotherGoodAddress,
			},
			expErr: true,
		},
		"bad sender": {
			src: MsgUpdateAdmin{
				Sender:   badAddress,
				NewAdmin: otherGoodAddress,
				Contract: anotherGoodAddress,
			},
			expErr: true,
		},
		"bad new admin": {
			src: MsgUpdateAdmin{
				Sender:   goodAddress,
				NewAdmin: badAddress,
				Contract: anotherGoodAddress,
			},
			expErr: true,
		},
		"bad contract addr": {
			src: MsgUpdateAdmin{
				Sender:   goodAddress,
				NewAdmin: otherGoodAddress,
				Contract: badAddress,
			},
			expErr: true,
		},
		"new admin same as old admin": {
			src: MsgUpdateAdmin{
				Sender:   goodAddress,
				NewAdmin: goodAddress,
				Contract: anotherGoodAddress,
			},
			expErr: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			err := spec.src.ValidateBasic()
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestMsgClearAdministrator(t *testing.T) {
	bad, err := sdk.AccAddressFromHex("012345")
	require.NoError(t, err)
	badAddress := bad.String()
	// proper address size
	goodAddress := sdk.AccAddress(make([]byte, 20)).String()
	anotherGoodAddress := sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20)).String()

	specs := map[string]struct {
		src    MsgClearAdmin
		expErr bool
	}{
		"all good": {
			src: MsgClearAdmin{
				Sender:   goodAddress,
				Contract: anotherGoodAddress,
			},
		},
		"bad sender": {
			src: MsgClearAdmin{
				Sender:   badAddress,
				Contract: anotherGoodAddress,
			},
			expErr: true,
		},
		"bad contract addr": {
			src: MsgClearAdmin{
				Sender:   goodAddress,
				Contract: badAddress,
			},
			expErr: true,
		},
		"contract missing": {
			src: MsgClearAdmin{
				Sender: goodAddress,
			},
			expErr: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			err := spec.src.ValidateBasic()
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestMsgMigrateContract(t *testing.T) {
	bad, err := sdk.AccAddressFromHex("012345")
	require.NoError(t, err)
	badAddress := bad.String()
	// proper address size
	goodAddress := sdk.AccAddress(make([]byte, 20)).String()
	anotherGoodAddress := sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20)).String()

	specs := map[string]struct {
		src    MsgMigrateContract
		expErr bool
	}{
		"all good": {
			src: MsgMigrateContract{
				Sender:   goodAddress,
				Contract: anotherGoodAddress,
				CodeID:   firstCodeID,
				Msg:      []byte("{}"),
			},
		},
		"bad sender": {
			src: MsgMigrateContract{
				Sender:   badAddress,
				Contract: anotherGoodAddress,
				CodeID:   firstCodeID,
			},
			expErr: true,
		},
		"empty sender": {
			src: MsgMigrateContract{
				Contract: anotherGoodAddress,
				CodeID:   firstCodeID,
			},
			expErr: true,
		},
		"empty code": {
			src: MsgMigrateContract{
				Sender:   goodAddress,
				Contract: anotherGoodAddress,
			},
			expErr: true,
		},
		"bad contract addr": {
			src: MsgMigrateContract{
				Sender:   goodAddress,
				Contract: badAddress,
				CodeID:   firstCodeID,
			},
			expErr: true,
		},
		"empty contract addr": {
			src: MsgMigrateContract{
				Sender: goodAddress,
				CodeID: firstCodeID,
			},
			expErr: true,
		},
		"non json migrateMsg": {
			src: MsgMigrateContract{
				Sender:   goodAddress,
				Contract: anotherGoodAddress,
				CodeID:   firstCodeID,
				Msg:      []byte("invalid json"),
			},
			expErr: true,
		},
		"empty migrateMsg": {
			src: MsgMigrateContract{
				Sender:   goodAddress,
				Contract: anotherGoodAddress,
				CodeID:   firstCodeID,
			},
			expErr: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			err := spec.src.ValidateBasic()
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestMsgJsonSignBytes(t *testing.T) {
	const myInnerMsg = `{"foo":"bar"}`
	specs := map[string]struct {
		src legacytx.LegacyMsg
		exp string
	}{
		"MsgInstantiateContract": {
			src: &MsgInstantiateContract{Msg: RawContractMessage(myInnerMsg)},
			exp: `
{
	"type":"wasm/MsgInstantiateContract",
	"value": {"msg": {"foo":"bar"}, "funds":[]}
}`,
		},
		"MsgExecuteContract": {
			src: &MsgExecuteContract{Msg: RawContractMessage(myInnerMsg)},
			exp: `
{
	"type":"wasm/MsgExecuteContract",
	"value": {"msg": {"foo":"bar"}, "funds":[]}
}`,
		},
		"MsgMigrateContract": {
			src: &MsgMigrateContract{Msg: RawContractMessage(myInnerMsg)},
			exp: `
{
	"type":"wasm/MsgMigrateContract",
	"value": {"msg": {"foo":"bar"}}
}`,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			bz := spec.src.GetSignBytes()
			assert.JSONEq(t, spec.exp, string(bz), "raw: %s", string(bz))
		})
	}
}
