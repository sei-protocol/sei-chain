package types_test

import (
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
)

// TestMustMarshalProtoJSON tests that the memo field is only emitted (marshalled) if it is populated
func (suite *TypesTestSuite) TestMustMarshalProtoJSON() {
	memo := "memo"
	packetData := types.NewFungibleTokenPacketData(sdk.DefaultBondDenom, "1", suite.chainA.SenderAccount.GetAddress().String(), suite.chainB.SenderAccount.GetAddress().String())
	packetData.Memo = memo

	bz := packetData.GetBytes()
	exists := strings.Contains(string(bz), memo)
	suite.Require().True(exists)

	packetData.Memo = ""

	bz = packetData.GetBytes()
	exists = strings.Contains(string(bz), memo)
	suite.Require().False(exists)
}
