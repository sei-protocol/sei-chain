package types_test

import (
	"github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
)

func (suite *TypesTestSuite) TestKeyActiveChannel() {
	key := types.KeyActiveChannel("port-id", "connection-id")
	suite.Require().Equal("activeChannel/port-id/connection-id", string(key))
}

func (suite *TypesTestSuite) TestKeyOwnerAccount() {
	key := types.KeyOwnerAccount("connection-id", "port-id")
	suite.Require().Equal("owner/connection-id/port-id", string(key))
}
