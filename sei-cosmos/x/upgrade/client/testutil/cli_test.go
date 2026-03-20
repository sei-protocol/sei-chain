package testutil

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/network"
	"github.com/stretchr/testify/suite"
)

func TestIntegrationTestSuite(t *testing.T) {
	cfg := network.DefaultConfig(t)
	cfg.NumValidators = 1
	suite.Run(t, NewIntegrationTestSuite(cfg))
}
