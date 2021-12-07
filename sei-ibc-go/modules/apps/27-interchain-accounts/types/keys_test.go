package types_test

import (
	"github.com/cosmos/ibc-go/v2/modules/apps/27-interchain-accounts/types"
)

func (suite *TypesTestSuite) TestKeyActiveChannel() {
	key := types.KeyActiveChannel("port-id")
	suite.Require().Equal("activeChannel/port-id", string(key))
}

func (suite *TypesTestSuite) TestKeyOwnerAccount() {
	key := types.KeyOwnerAccount("port-id")
	suite.Require().Equal("owner/port-id", string(key))
}
