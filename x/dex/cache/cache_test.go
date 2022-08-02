package dex_test

import (
	"testing"

	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/stretchr/testify/require"
)

const (
	TEST_CONTRACT = "test"
	TEST_PAIR     = "pair"
)

func TestDeepCopy(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateTwo := stateOne.DeepCopy()
	stateTwo.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           2,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	// old state must not be changed
	require.Equal(t, 1, len(stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()))
	// new state must be changed
	require.Equal(t, 2, len(stateTwo.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()))
}

func TestDeepFilterAccounts(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           2,
		Account:      "test2",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockCancels(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Cancellation{
		Id:      1,
		Creator: "test",
	})
	stateOne.GetBlockCancels(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Cancellation{
		Id:      2,
		Creator: "test2",
	})
	stateOne.GetDepositInfo(utils.ContractAddress(TEST_CONTRACT)).Add(&dex.DepositInfoEntry{
		Creator: "test",
	})
	stateOne.GetDepositInfo(utils.ContractAddress(TEST_CONTRACT)).Add(&dex.DepositInfoEntry{
		Creator: "test2",
	})
	stateOne.GetLiquidationRequests(utils.ContractAddress(TEST_CONTRACT)).Add(&dex.LiquidationRequest{Requestor: "test", AccountToLiquidate: ""})
	stateOne.GetLiquidationRequests(utils.ContractAddress(TEST_CONTRACT)).Add(&dex.LiquidationRequest{Requestor: "test2", AccountToLiquidate: ""})

	stateOne.DeepFilterAccount("test")
	require.Equal(t, 1, stateOne.BlockOrders.Len())
	require.Equal(t, 1, stateOne.BlockCancels.Len())
	require.Equal(t, 1, stateOne.DepositInfo.Len())
	require.Equal(t, 1, stateOne.LiquidationRequests.Len())
}

func TestClear(t *testing.T) {
	stateOne := dex.NewMemState()
	stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.Clear()
	require.Equal(t, 0, len(stateOne.GetBlockOrders(utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()))
}
