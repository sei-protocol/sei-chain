package testutil

import (
	"fmt"

	seiapp "github.com/sei-protocol/sei-chain/app"
	clitestutil "github.com/sei-protocol/sei-chain/sei-cosmos/testutil/cli"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/network"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/client/cli"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"
)

func NewIntegrationTestSuite(cfg network.Config) *IntegrationTestSuite {
	return &IntegrationTestSuite{cfg: cfg}
}

type IntegrationTestSuite struct {
	suite.Suite

	app     *seiapp.App
	cfg     network.Config
	network *network.Network
	ctx     sdk.Context
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")
	app := seiapp.Setup(s.T(), false, false, false)
	s.app = app
	s.ctx = app.NewContext(false, tmproto.Header{})

	cfg := network.DefaultConfig(s.T())
	cfg.NumValidators = 1

	s.cfg = cfg
	s.network = network.New(s.T(), cfg)
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.network.Cleanup()
}

func (s *IntegrationTestSuite) TestModuleVersionsCLI() {
	testCases := []struct {
		msg     string
		req     types.QueryModuleVersionsRequest
		single  bool
		expPass bool
	}{
		{
			msg:     "test full query",
			req:     types.QueryModuleVersionsRequest{ModuleName: ""},
			single:  false,
			expPass: true,
		},
		{
			msg:     "test single module",
			req:     types.QueryModuleVersionsRequest{ModuleName: "bank"},
			single:  true,
			expPass: true,
		},
		{
			msg:     "test non-existent module",
			req:     types.QueryModuleVersionsRequest{ModuleName: "abcdefg"},
			single:  true,
			expPass: false,
		},
	}

	val := s.network.Validators[0]
	clientCtx := val.ClientCtx
	// avoid printing as yaml from CLI command
	clientCtx.OutputFormat = "JSON"

	vm := s.app.UpgradeKeeper.GetModuleVersionMap(s.ctx)
	mv := s.app.UpgradeKeeper.GetModuleVersions(s.ctx)
	s.Require().NotEmpty(vm)

	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case %s", tc.msg), func() {

			expect := mv
			if tc.expPass {
				if tc.single {
					expect = []*types.ModuleVersion{{Name: tc.req.ModuleName, Version: vm[tc.req.ModuleName]}}
				}
				// setup expected response
				pm := types.QueryModuleVersionsResponse{
					ModuleVersions: expect,
				}
				jsonVM, _ := clientCtx.Codec.MarshalAsJSON(&pm)
				expectedRes := string(jsonVM)
				// append new line to match behaviour of PrintProto
				expectedRes += "\n"

				// get actual module versions list response from cli
				cmd := cli.GetModuleVersionsCmd()
				outVM, err := clitestutil.ExecTestCLICmd(clientCtx, cmd, []string{tc.req.ModuleName})
				s.Require().NoError(err)

				s.Require().Equal(expectedRes, outVM.String())
			} else {
				cmd := cli.GetModuleVersionsCmd()
				_, err := clitestutil.ExecTestCLICmd(clientCtx, cmd, []string{tc.req.ModuleName})
				s.Require().Error(err)
			}
		})
	}
}
