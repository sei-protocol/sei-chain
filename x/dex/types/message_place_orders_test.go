package types_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestValidateMsgPlaceOrder(t *testing.T) {
	TEST_CONTRACT := "sei1ghd753shjuwexxywmgs4xz7x2q732vcnkm6h2pyv9s6ah3hylvrqladqwc"
	msg := &types.MsgPlaceOrders{
		Creator:      "sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx",
		ContractAddr: TEST_CONTRACT,
		Orders: []*types.Order{
			{
				Id:           1,
				Account:      "test",
				ContractAddr: TEST_CONTRACT,
				Quantity:     sdk.OneDec(),
				Price:        sdk.OneDec(),
				AssetDenom:   "denom1",
				PriceDenom:   "denom2",
			},
		},
	}
	require.NoError(t, msg.ValidateBasic())

	// Empty orders
	msg = &types.MsgPlaceOrders{
		Creator:      "sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx",
		ContractAddr: TEST_CONTRACT,
		Orders:       []*types.Order{},
	}
	require.Error(t, msg.ValidateBasic())

	// Test various invalid orders field
	msg = &types.MsgPlaceOrders{
		Creator:      "sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx",
		ContractAddr: TEST_CONTRACT,
		Orders: []*types.Order{
			{
				Id:           1,
				Account:      "test",
				ContractAddr: TEST_CONTRACT,
				Quantity:     sdk.OneDec().Neg(),
				Price:        sdk.OneDec(),
				AssetDenom:   "denom1",
				PriceDenom:   "denom2",
			},
		},
	}
	require.Error(t, msg.ValidateBasic())
	msg = &types.MsgPlaceOrders{
		Creator:      "sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx",
		ContractAddr: TEST_CONTRACT,
		Orders: []*types.Order{
			{
				Id:           1,
				Account:      "test",
				ContractAddr: TEST_CONTRACT,
				Quantity:     sdk.OneDec(),
				AssetDenom:   "denom1",
				PriceDenom:   "denom2",
			},
		},
	}
	require.Error(t, msg.ValidateBasic())
	msg = &types.MsgPlaceOrders{
		Creator:      "sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx",
		ContractAddr: TEST_CONTRACT,
		Orders: []*types.Order{
			{
				Id:           1,
				Account:      "test",
				ContractAddr: TEST_CONTRACT,
				Quantity:     sdk.OneDec(),
				Price:        sdk.OneDec(),
				AssetDenom:   "invalid denom",
				PriceDenom:   "denom2",
			},
		},
	}
	require.Error(t, msg.ValidateBasic())
	msg = &types.MsgPlaceOrders{
		Creator:      "sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx",
		ContractAddr: TEST_CONTRACT,
		Orders: []*types.Order{
			{
				Id:           1,
				Account:      "test",
				ContractAddr: TEST_CONTRACT,
				Quantity:     sdk.OneDec().Neg(),
				Price:        sdk.OneDec(),
				AssetDenom:   "denom1",
				PriceDenom:   "invalid denom",
			},
		},
	}
	require.Error(t, msg.ValidateBasic())
}
