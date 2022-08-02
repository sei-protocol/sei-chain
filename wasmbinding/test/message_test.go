package wasmbinding

import (
	"encoding/json"
	"testing"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

const (
	TEST_TARGET_CONTRACT = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	TEST_CREATOR         = "sei1nc5tatafv6eyq7llkr2gv50ff9e22mnf70qgjlv737ktmt4eswrqms7u8a"
)

var ZeroTick = sdk.ZeroDec()
var TestPair = types.Pair{
	PriceDenom: "USDC",
	AssetDenom: "SEI",
	Ticksize:   &ZeroTick,
}

func TestPlaceOrder(t *testing.T) {
	order := types.Order{
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIMIT,
		PriceDenom:        TestPair.PriceDenom,
		AssetDenom:        TestPair.AssetDenom,
		Price:             sdk.MustNewDecFromStr("10"),
		Quantity:          sdk.OneDec(),
		Data:              "{\"position_effect\":\"OPEN\", \"leverage\":\"1\"}",
	}
	fund := sdk.NewCoin("usei", sdk.NewInt(1000000000))
	msg := types.MsgPlaceOrders{
		Creator:      TEST_CREATOR,
		Orders:       []*types.Order{&order},
		ContractAddr: TEST_TARGET_CONTRACT,
		Funds:        []sdk.Coin{fund},
	}
	serialized, _ := json.Marshal(msg)
	msgData := wasmbinding.SeiWasmMessage{
		PlaceOrders: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	k, ctx := keepertest.DexKeeper(t)
	k.SetContract(ctx, &types.ContractInfo{
		CodeId:       1,
		ContractAddr: TEST_CREATOR,
		Dependencies: []*types.ContractDependencyInfo{
			{Dependency: TEST_TARGET_CONTRACT},
		},
	})
	k.AddRegisteredPair(ctx, TEST_TARGET_CONTRACT, TestPair)
	k.SetTickSizeForPair(ctx, TEST_TARGET_CONTRACT, TestPair, *TestPair.Ticksize)

	decorator := wasmbinding.CustomMessageDecorator(k)
	messenger := decorator(nil)
	_, _, err := messenger.DispatchMsg(ctx, sdk.AccAddress(TEST_CREATOR), "", wasmvmtypes.CosmosMsg{
		Custom: serializedMsg,
	})
	require.Nil(t, err)
}

func TestDecodeOrderCancellation(t *testing.T) {
	msg := types.MsgCancelOrders{
		Creator:      TEST_CREATOR,
		OrderIds:     []uint64{1},
		ContractAddr: TEST_TARGET_CONTRACT,
	}
	serialized, _ := json.Marshal(msg)
	msgData := wasmbinding.SeiWasmMessage{
		CancelOrders: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	k, ctx := keepertest.DexKeeper(t)
	k.SetContract(ctx, &types.ContractInfo{
		CodeId:       1,
		ContractAddr: TEST_CREATOR,
		Dependencies: []*types.ContractDependencyInfo{
			{Dependency: TEST_TARGET_CONTRACT},
		},
	})
	k.AddRegisteredPair(ctx, TEST_TARGET_CONTRACT, TestPair)
	k.SetTickSizeForPair(ctx, TEST_TARGET_CONTRACT, TestPair, *TestPair.Ticksize)

	decorator := wasmbinding.CustomMessageDecorator(k)
	messenger := decorator(nil)
	_, _, err := messenger.DispatchMsg(ctx, sdk.AccAddress(TEST_CREATOR), "", wasmvmtypes.CosmosMsg{
		Custom: serializedMsg,
	})
	require.Nil(t, err)
}
