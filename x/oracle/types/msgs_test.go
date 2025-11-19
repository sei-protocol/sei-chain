package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestMsgFeederDelegation(t *testing.T) {
	addrs := []seitypes.AccAddress{
		seitypes.AccAddress([]byte("addr1_______________")),
		seitypes.AccAddress([]byte("addr2_______________")),
	}

	tests := []struct {
		delegator  seitypes.ValAddress
		delegate   seitypes.AccAddress
		expectPass bool
	}{
		{seitypes.ValAddress(addrs[0]), addrs[1], true},
		{seitypes.ValAddress{}, addrs[1], false},
		{seitypes.ValAddress(addrs[0]), seitypes.AccAddress{}, false},
		{nil, nil, false},
	}

	for i, tc := range tests {
		msg := NewMsgDelegateFeedConsent(tc.delegator, tc.delegate)
		if tc.expectPass {
			require.Nil(t, msg.ValidateBasic(), "test: %v", i)
		} else {
			require.NotNil(t, msg.ValidateBasic(), "test: %v", i)
		}
	}
}

func TestMsgAggregateExchangeRateVote(t *testing.T) {
	addrs := []seitypes.AccAddress{
		seitypes.AccAddress([]byte("addr1_______________")),
	}

	invalidExchangeRates := "a,b"
	exchangeRates := "1.0foo,1232.132bar"
	abstainExchangeRates := "0.0foo,1232.132bar"
	overFlowExchangeRates := "1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000.0foo,1232.132bar"

	tests := []struct {
		voter         seitypes.AccAddress
		exchangeRates string
		expectPass    bool
	}{
		{addrs[0], exchangeRates, true},
		{addrs[0], invalidExchangeRates, false},
		{addrs[0], abstainExchangeRates, true},
		{addrs[0], overFlowExchangeRates, false},
		{seitypes.AccAddress{}, exchangeRates, false},
	}

	for i, tc := range tests {
		msg := NewMsgAggregateExchangeRateVote(tc.exchangeRates, tc.voter, seitypes.ValAddress(tc.voter))
		if tc.expectPass {
			require.Nil(t, msg.ValidateBasic(), "test: %v", i)
		} else {
			require.NotNil(t, msg.ValidateBasic(), "test: %v", i)
		}
	}
}
