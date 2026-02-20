package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func TestMsgSendRoute(t *testing.T) {
	addr1 := sdk.AccAddress([]byte("from"))
	addr2 := sdk.AccAddress([]byte("to"))
	coins := sdk.NewCoins(sdk.NewInt64Coin("atom", 10))
	var msg = NewMsgSend(addr1, addr2, coins)

	require.Equal(t, msg.Route(), RouterKey)
	require.Equal(t, msg.Type(), "send")
}

func TestMsgSendValidation(t *testing.T) {
	addr1 := sdk.AccAddress([]byte("from________________"))
	addr2 := sdk.AccAddress([]byte("to__________________"))
	addrEmpty := sdk.AccAddress([]byte(""))
	addrLong := sdk.AccAddress([]byte("Purposefully long address"))

	atom123 := sdk.NewCoins(sdk.NewInt64Coin("atom", 123))
	atom0 := sdk.NewCoins(sdk.NewInt64Coin("atom", 0))
	atom123eth123 := sdk.NewCoins(sdk.NewInt64Coin("atom", 123), sdk.NewInt64Coin("eth", 123))
	atom123eth0 := sdk.Coins{sdk.NewInt64Coin("atom", 123), sdk.NewInt64Coin("eth", 0)}

	cases := []struct {
		expectedErr string // empty means no error expected
		msg         *MsgSend
	}{
		{"", NewMsgSend(addr1, addr2, atom123)},       // valid send
		{"", NewMsgSend(addr1, addr2, atom123eth123)}, // valid send with multiple coins
		{"Invalid sender address (address length must be 20 or 32 bytes, got 25: unknown address): invalid address", NewMsgSend(addrLong, addr2, atom123)},    // valid send with long addr sender
		{"Invalid recipient address (address length must be 20 or 32 bytes, got 25: unknown address): invalid address", NewMsgSend(addr1, addrLong, atom123)}, // valid send with long addr recipient
		{": invalid coins", NewMsgSend(addr1, addr2, atom0)},                   // non positive coin
		{"123atom,0eth: invalid coins", NewMsgSend(addr1, addr2, atom123eth0)}, // non positive coin in multicoins
		{"Invalid sender address (empty address string is not allowed): invalid address", NewMsgSend(addrEmpty, addr2, atom123)},
		{"Invalid recipient address (empty address string is not allowed): invalid address", NewMsgSend(addr1, addrEmpty, atom123)},
	}

	for _, tc := range cases {
		err := tc.msg.ValidateBasic()
		if tc.expectedErr == "" {
			require.Nil(t, err)
		} else {
			require.EqualError(t, err, tc.expectedErr)
		}
	}
}

func TestMsgSendGetSignBytes(t *testing.T) {
	addr1 := sdk.AccAddress([]byte("input"))
	addr2 := sdk.AccAddress([]byte("output"))
	coins := sdk.NewCoins(sdk.NewInt64Coin("atom", 10))
	var msg = NewMsgSend(addr1, addr2, coins)
	res := msg.GetSignBytes()

	expected := `{"type":"cosmos-sdk/MsgSend","value":{"amount":[{"amount":"10","denom":"atom"}],"from_address":"sei1d9h8qat54uvf5a","to_address":"sei1da6hgur4wseuc2ay"}}`
	require.Equal(t, expected, string(res))
}

func TestMsgSendGetSigners(t *testing.T) {
	var msg = NewMsgSend(sdk.AccAddress([]byte("input111111111111111")), sdk.AccAddress{}, sdk.NewCoins())
	res := msg.GetSigners()
	// TODO: fix this !
	require.Equal(t, fmt.Sprintf("%v", res), "[696E707574313131313131313131313131313131]")
}

func TestMsgMultiSendRoute(t *testing.T) {
	// Construct a MsgSend
	addr1 := sdk.AccAddress([]byte("input"))
	addr2 := sdk.AccAddress([]byte("output"))
	coins := sdk.NewCoins(sdk.NewInt64Coin("atom", 10))
	var msg = MsgMultiSend{
		Inputs:  []Input{NewInput(addr1, coins)},
		Outputs: []Output{NewOutput(addr2, coins)},
	}

	// TODO some failures for bad result
	require.Equal(t, msg.Route(), RouterKey)
	require.Equal(t, msg.Type(), "multisend")
}

func TestInputValidation(t *testing.T) {
	addr1 := sdk.AccAddress([]byte("_______alice________"))
	addr2 := sdk.AccAddress([]byte("________bob_________"))
	addrEmpty := sdk.AccAddress([]byte(""))
	addrLong := sdk.AccAddress([]byte("Purposefully long address"))

	someCoins := sdk.NewCoins(sdk.NewInt64Coin("atom", 123))
	multiCoins := sdk.NewCoins(sdk.NewInt64Coin("atom", 123), sdk.NewInt64Coin("eth", 20))

	emptyCoins := sdk.NewCoins()
	emptyCoins2 := sdk.NewCoins(sdk.NewInt64Coin("eth", 0))
	someEmptyCoins := sdk.Coins{sdk.NewInt64Coin("eth", 10), sdk.NewInt64Coin("atom", 0)}
	unsortedCoins := sdk.Coins{sdk.NewInt64Coin("eth", 1), sdk.NewInt64Coin("atom", 1)}

	cases := []struct {
		expectedErr string // empty means no error expected
		txIn        Input
	}{
		// auth works with different apps
		{"", NewInput(addr1, someCoins)},
		{"", NewInput(addr2, someCoins)},
		{"", NewInput(addr2, multiCoins)},
		{"address length must be 20 or 32 bytes, got 25: unknown address", NewInput(addrLong, someCoins)},

		{"empty address string is not allowed", NewInput(addrEmpty, someCoins)},
		{": invalid coins", NewInput(addr1, emptyCoins)},                // invalid coins
		{": invalid coins", NewInput(addr1, emptyCoins2)},               // invalid coins
		{"10eth,0atom: invalid coins", NewInput(addr1, someEmptyCoins)}, // invalid coins
		{"1eth,1atom: invalid coins", NewInput(addr1, unsortedCoins)},   // unsorted coins
	}

	for i, tc := range cases {
		err := tc.txIn.ValidateBasic()
		if tc.expectedErr == "" {
			require.Nil(t, err, "%d: %+v", i, err)
		} else {
			require.EqualError(t, err, tc.expectedErr, "%d", i)
		}
	}
}

func TestOutputValidation(t *testing.T) {
	addr1 := sdk.AccAddress([]byte("_______alice________"))
	addr2 := sdk.AccAddress([]byte("________bob_________"))
	addrEmpty := sdk.AccAddress([]byte(""))
	addrLong := sdk.AccAddress([]byte("Purposefully long address"))

	someCoins := sdk.NewCoins(sdk.NewInt64Coin("atom", 123))
	multiCoins := sdk.NewCoins(sdk.NewInt64Coin("atom", 123), sdk.NewInt64Coin("eth", 20))

	emptyCoins := sdk.NewCoins()
	emptyCoins2 := sdk.NewCoins(sdk.NewInt64Coin("eth", 0))
	someEmptyCoins := sdk.Coins{sdk.NewInt64Coin("eth", 10), sdk.NewInt64Coin("atom", 0)}
	unsortedCoins := sdk.Coins{sdk.NewInt64Coin("eth", 1), sdk.NewInt64Coin("atom", 1)}

	cases := []struct {
		expectedErr string // empty means no error expected
		txOut       Output
	}{
		// auth works with different apps
		{"", NewOutput(addr1, someCoins)},
		{"", NewOutput(addr2, someCoins)},
		{"", NewOutput(addr2, multiCoins)},
		{"Invalid output address (address length must be 20 or 32 bytes, got 25: unknown address): invalid address", NewOutput(addrLong, someCoins)},

		{"Invalid output address (empty address string is not allowed): invalid address", NewOutput(addrEmpty, someCoins)},
		{": invalid coins", NewOutput(addr1, emptyCoins)},                // invalid coins
		{": invalid coins", NewOutput(addr1, emptyCoins2)},               // invalid coins
		{"10eth,0atom: invalid coins", NewOutput(addr1, someEmptyCoins)}, // invalid coins
		{"1eth,1atom: invalid coins", NewOutput(addr1, unsortedCoins)},   // unsorted coins
	}

	for i, tc := range cases {
		err := tc.txOut.ValidateBasic()
		if tc.expectedErr == "" {
			require.Nil(t, err, "%d: %+v", i, err)
		} else {
			require.EqualError(t, err, tc.expectedErr, "%d", i)
		}
	}
}

func TestMsgMultiSendValidation(t *testing.T) {
	addr1 := sdk.AccAddress([]byte("_______alice________"))
	addr2 := sdk.AccAddress([]byte("________bob_________"))
	atom123 := sdk.NewCoins(sdk.NewInt64Coin("atom", 123))
	atom124 := sdk.NewCoins(sdk.NewInt64Coin("atom", 124))
	eth123 := sdk.NewCoins(sdk.NewInt64Coin("eth", 123))
	atom123eth123 := sdk.NewCoins(sdk.NewInt64Coin("atom", 123), sdk.NewInt64Coin("eth", 123))

	input1 := NewInput(addr1, atom123)
	input2 := NewInput(addr1, eth123)
	output1 := NewOutput(addr2, atom123)
	output2 := NewOutput(addr2, atom124)
	outputMulti := NewOutput(addr2, atom123eth123)

	var emptyAddr sdk.AccAddress

	cases := []struct {
		valid bool
		tx    MsgMultiSend
	}{
		{false, MsgMultiSend{}},                           // no input or output
		{false, MsgMultiSend{Inputs: []Input{input1}}},    // just input
		{false, MsgMultiSend{Outputs: []Output{output1}}}, // just output
		{false, MsgMultiSend{
			Inputs:  []Input{NewInput(emptyAddr, atom123)}, // invalid input
			Outputs: []Output{output1}}},
		{false, MsgMultiSend{
			Inputs:  []Input{input1},
			Outputs: []Output{{emptyAddr.String(), atom123}}}, // invalid output
		},
		{false, MsgMultiSend{
			Inputs:  []Input{input1},
			Outputs: []Output{output2}}, // amounts dont match
		},
		{true, MsgMultiSend{
			Inputs:  []Input{input1},
			Outputs: []Output{output1}},
		},
		{true, MsgMultiSend{
			Inputs:  []Input{input1, input2},
			Outputs: []Output{outputMulti}},
		},
	}

	for i, tc := range cases {
		err := tc.tx.ValidateBasic()
		if tc.valid {
			require.Nil(t, err, "%d: %+v", i, err)
		} else {
			require.NotNil(t, err, "%d", i)
		}
	}
}

func TestMsgMultiSendGetSignBytes(t *testing.T) {
	addr1 := sdk.AccAddress([]byte("input"))
	addr2 := sdk.AccAddress([]byte("output"))
	coins := sdk.NewCoins(sdk.NewInt64Coin("atom", 10))
	var msg = MsgMultiSend{
		Inputs:  []Input{NewInput(addr1, coins)},
		Outputs: []Output{NewOutput(addr2, coins)},
	}
	res := msg.GetSignBytes()

	expected := `{"type":"cosmos-sdk/MsgMultiSend","value":{"inputs":[{"address":"sei1d9h8qat54uvf5a","coins":[{"amount":"10","denom":"atom"}]}],"outputs":[{"address":"sei1da6hgur4wseuc2ay","coins":[{"amount":"10","denom":"atom"}]}]}}`
	require.Equal(t, expected, string(res))
}

func TestMsgMultiSendGetSigners(t *testing.T) {
	var msg = MsgMultiSend{
		Inputs: []Input{
			NewInput(sdk.AccAddress([]byte("input111111111111111")), nil),
			NewInput(sdk.AccAddress([]byte("input222222222222222")), nil),
			NewInput(sdk.AccAddress([]byte("input333333333333333")), nil),
		},
	}

	res := msg.GetSigners()
	// TODO: fix this !
	require.Equal(t, "[696E707574313131313131313131313131313131 696E707574323232323232323232323232323232 696E707574333333333333333333333333333333]", fmt.Sprintf("%v", res))
}

func TestMsgSendSigners(t *testing.T) {
	signers := []sdk.AccAddress{
		sdk.MustAccAddressFromBech32("sei10xwrnrezdg227cgt82az7f7j47q3zklvu5ax6k"),
		sdk.MustAccAddressFromBech32("sei1rs8v2232uv5nw8c88ruvyjy08mmxfx25pur3pl"),
		sdk.MustAccAddressFromBech32("sei1l976cvcndrr6hnuyzn93azaxx8sc2xre5crtpz"),
	}

	someCoins := sdk.NewCoins(sdk.NewInt64Coin("atom", 123))
	inputs := make([]Input, len(signers))
	for i, signer := range signers {
		inputs[i] = NewInput(signer, someCoins)
	}
	tx := NewMsgMultiSend(inputs, nil)

	require.Equal(t, signers, tx.GetSigners())
}
