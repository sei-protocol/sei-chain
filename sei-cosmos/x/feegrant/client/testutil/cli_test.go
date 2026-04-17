//go:build norace
// +build norace

package testutil

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/network"

	"github.com/stretchr/testify/suite"
)

func TestIntegrationTestSuite(t *testing.T) {
	cfg := network.DefaultConfig()
	cfg.NumValidators = 3
	suite.Run(t, NewIntegrationTestSuite(cfg))
}
