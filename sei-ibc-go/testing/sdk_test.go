package ibctesting_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	kmultisig "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/multisig"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	clitestutil "github.com/sei-protocol/sei-chain/sei-cosmos/testutil/cli"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/network"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/rest"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	authcli "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/client/cli"
	authrest "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/client/rest"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/suite"
	dbm "github.com/tendermint/tm-db"

	ibcclientcli "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/client/cli"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/testing/simapp"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/testing/simapp/params"
)

/*
	This file contains tests from the SDK which had to deleted during the migration of
	the IBC module from the SDK into this repository. https://github.com/cosmos/cosmos-sdk/pull/8735

	They can be removed once the SDK deprecates amino.
*/

type IntegrationTestSuite struct {
	suite.Suite

	cfg     network.Config
	network *network.Network
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")

	cfg := DefaultConfig()

	s.cfg = cfg
	s.network = network.New(s.T(), cfg)

	kb := s.network.Validators[0].ClientCtx.Keyring
	_, _, err := kb.NewMnemonic("newAccount", keyring.English, sdk.FullFundraiserPath, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	s.Require().NoError(err)

	account1, _, err := kb.NewMnemonic("newAccount1", keyring.English, sdk.FullFundraiserPath, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	s.Require().NoError(err)

	account2, _, err := kb.NewMnemonic("newAccount2", keyring.English, sdk.FullFundraiserPath, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	s.Require().NoError(err)

	multi := kmultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{account1.GetPubKey(), account2.GetPubKey()})
	_, err = kb.SaveMultisig("multi", multi)
	s.Require().NoError(err)

	_, err = s.network.WaitForHeight(1)
	s.Require().NoError(err)

	s.Require().NoError(s.network.WaitForNextBlock())
}

func TestIntegrationTestSuite(t *testing.T) {
	//TODO: these integration tests suite are broken on sei-cosmos old version as well, but working in monorepo, so we'll need to fix them once merging into monorepo
	t.Skip("Skipping integration test suite")
	suite.Run(t, new(IntegrationTestSuite))
}

// NewAppConstructor returns a new simapp AppConstructor
func NewAppConstructor(encodingCfg params.EncodingConfig) network.AppConstructor {
	return func(val network.Validator) servertypes.Application {
		return simapp.NewSimApp(
			val.Ctx.Logger, dbm.NewMemDB(), nil, true, make(map[int64]bool), val.Ctx.Config.RootDir, 0,
			nil, encodingCfg,
			simapp.EmptyAppOptions{},
			baseapp.SetPruning(storetypes.NewPruningOptionsFromString(val.AppConfig.Pruning)),
			baseapp.SetMinGasPrices(val.AppConfig.MinGasPrices),
		)
	}
}

// DefaultConfig returns a sane default configuration suitable for nearly all
// testing requirements.
func DefaultConfig() network.Config {
	encCfg := simapp.MakeTestEncodingConfig()

	return network.Config{
		Codec:             encCfg.Marshaler,
		TxConfig:          encCfg.TxConfig,
		LegacyAmino:       encCfg.Amino,
		InterfaceRegistry: encCfg.InterfaceRegistry,
		AccountRetriever:  authtypes.AccountRetriever{},
		AppConstructor:    NewAppConstructor(encCfg),
		GenesisState:      simapp.ModuleBasics.DefaultGenesis(encCfg.Marshaler),
		TimeoutCommit:     2 * time.Second,
		ChainID:           "chain-" + tmrand.NewRand().Str(6),
		NumValidators:     4,
		BondDenom:         sdk.DefaultBondDenom,
		MinGasPrices:      fmt.Sprintf("0.000006%s", sdk.DefaultBondDenom),
		AccountTokens:     sdk.TokensFromConsensusPower(1000, sdk.DefaultPowerReduction),
		StakingTokens:     sdk.TokensFromConsensusPower(500, sdk.DefaultPowerReduction),
		BondedTokens:      sdk.TokensFromConsensusPower(100, sdk.DefaultPowerReduction),
		PruningStrategy:   storetypes.PruningOptionNothing,
		CleanupDir:        true,
		SigningAlgo:       string(hd.Secp256k1Type),
		KeyringOptions:    []keyring.Option{},
	}
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.network.Cleanup()
}

// TestLegacyRestErrMessages creates two IBC txs, one that fails, one that
// succeeds, and make sure we cannot query any of them (with pretty error msg).
// Our intension is to test the error message of querying a message which is
// signed with proto, since IBC won't support legacy amino at all we are
// considering a message from IBC module.
func (s *IntegrationTestSuite) TestLegacyRestErrMessages() {
	val := s.network.Validators[0]

	// Write client state json to temp file, used for an IBC message.
	// Generated by printing the result of cdc.MarshalIntefaceJSON on
	// a solo machine client state
	clientStateJSON := testutil.WriteToNewTempFile(
		s.T(),
		`{"@type":"/ibc.lightclients.solomachine.v2.ClientState","sequence":"1","is_frozen":false,"consensus_state":{"public_key":{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AtK50+5pJOoaa04qqAqrnyAqsYrwrR/INnA6UPIaYZlp"},"diversifier":"testing","timestamp":"10"},"allow_update_after_proposal":false}`,
	)

	badClientStateJSON := testutil.WriteToNewTempFile(
		s.T(),
		`{"@type":"/ibc.lightclients.solomachine.v2.ClientState","sequence":"1","is_frozen":false,"consensus_state":{"public_key":{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AtK50+5pJOoaa04qqAqrnyAqsYrwrR/INnA6UPIaYZlp"},"diversifier":"DIFFERENT","timestamp":"10"},"allow_update_after_proposal":false}`,
	)

	// Write consensus json to temp file, used for an IBC message.
	// Generated by printing the result of cdc.MarshalIntefaceJSON on
	// a solo machine consensus state
	consensusJSON := testutil.WriteToNewTempFile(
		s.T(),
		`{"@type":"/ibc.lightclients.solomachine.v2.ConsensusState","public_key":{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AtK50+5pJOoaa04qqAqrnyAqsYrwrR/INnA6UPIaYZlp"},"diversifier":"testing","timestamp":"10"}`,
	)

	testCases := []struct {
		desc string
		cmd  *cobra.Command
		args []string
		code uint32
	}{
		{
			"Failing IBC message",
			ibcclientcli.NewCreateClientCmd(),
			[]string{
				badClientStateJSON.Name(), // path to client state json
				consensusJSON.Name(),      // path to consensus json,
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(s.cfg.BondDenom, sdk.NewInt(10))).String()),
				fmt.Sprintf("--gas=%d", flags.DefaultGasLimit),
				fmt.Sprintf("--%s=%s", flags.FlagFrom, val.Address.String()),
				fmt.Sprintf("--%s=foobar", flags.FlagNote),
			},
			uint32(8),
		},
		{
			"Successful IBC message",
			ibcclientcli.NewCreateClientCmd(),
			[]string{
				clientStateJSON.Name(), // path to client state json
				consensusJSON.Name(),   // path to consensus json,
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(s.cfg.BondDenom, sdk.NewInt(10))).String()),
				fmt.Sprintf("--gas=%d", flags.DefaultGasLimit),
				fmt.Sprintf("--%s=%s", flags.FlagFrom, val.Address.String()),
				fmt.Sprintf("--%s=foobar", flags.FlagNote),
			},
			uint32(0),
		},
	}

	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case %s", tc.desc), func() {
			out, err := clitestutil.ExecTestCLICmd(val.ClientCtx, tc.cmd, tc.args)
			s.Require().NoError(err)
			var txRes sdk.TxResponse
			s.Require().NoError(val.ClientCtx.Codec.UnmarshalAsJSON(out.Bytes(), &txRes))
			s.Require().Equal(tc.code, txRes.Code)

			s.Require().NoError(s.network.WaitForNextBlock())

			s.testQueryIBCTx(txRes, tc.cmd, tc.args)
		})
	}
}

// testQueryIBCTx is a helper function to test querying txs which:
// - show an error message on legacy REST endpoints
// - succeed using gRPC
// In practice, we call this function on IBC txs.
func (s *IntegrationTestSuite) testQueryIBCTx(txRes sdk.TxResponse, cmd *cobra.Command, args []string) {
	val := s.network.Validators[0]

	errMsg := "this transaction cannot be displayed via legacy REST endpoints, because it does not support" +
		" Amino serialization. Please either use CLI, gRPC, gRPC-gateway, or directly query the Tendermint RPC" +
		" endpoint to query this transaction. The new REST endpoint (via gRPC-gateway) is "

	// Test that legacy endpoint return the above error message on IBC txs.
	testCases := []struct {
		desc string
		url  string
	}{
		{
			"Query by hash",
			fmt.Sprintf("%s/txs/%s", val.APIAddress, txRes.TxHash),
		},
		{
			"Query by height",
			fmt.Sprintf("%s/txs?tx.height=%d", val.APIAddress, txRes.Height),
		},
	}

	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case %s", tc.desc), func() {
			txJSON, err := rest.GetRequest(tc.url)
			s.Require().NoError(err)

			var errResp rest.ErrorResponse
			s.Require().NoError(val.ClientCtx.LegacyAmino.UnmarshalAsJSON(txJSON, &errResp))

			s.Require().Contains(errResp.Error, errMsg)
		})
	}

	// try fetching the txn using gRPC req, it will fetch info since it has proto codec.
	grpcJSON, err := rest.GetRequest(fmt.Sprintf("%s/cosmos/tx/v1beta1/txs/%s", val.APIAddress, txRes.TxHash))
	s.Require().NoError(err)

	var getTxRes txtypes.GetTxResponse
	s.Require().NoError(val.ClientCtx.Codec.UnmarshalAsJSON(grpcJSON, &getTxRes))
	s.Require().Equal(getTxRes.Tx.Body.Memo, "foobar")

	// generate broadcast only txn.
	args = append(args, fmt.Sprintf("--%s=true", flags.FlagGenerateOnly))
	out, err := clitestutil.ExecTestCLICmd(val.ClientCtx, cmd, args)
	s.Require().NoError(err)

	txFile := testutil.WriteToNewTempFile(s.T(), string(out.Bytes()))
	txFileName := txFile.Name()

	// encode the generated txn.
	out, err = clitestutil.ExecTestCLICmd(val.ClientCtx, authcli.GetEncodeCommand(), []string{txFileName})
	s.Require().NoError(err)

	bz, err := val.ClientCtx.LegacyAmino.MarshalAsJSON(authrest.DecodeReq{Tx: string(out.Bytes())})
	s.Require().NoError(err)

	// try to decode the txn using legacy rest, it fails.
	res, err := rest.PostRequest(fmt.Sprintf("%s/txs/decode", val.APIAddress), "application/json", bz)
	s.Require().NoError(err)

	var errResp rest.ErrorResponse
	s.Require().NoError(val.ClientCtx.LegacyAmino.UnmarshalAsJSON(res, &errResp))
	s.Require().Contains(errResp.Error, errMsg)
}
