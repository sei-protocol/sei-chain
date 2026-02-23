package app_test

import (
	"testing"

	cosmostypes "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	xparamtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

type PrioritizerTestSuite struct {
	apptesting.KeeperTestHelper
	prioritizer *app.SeiTxPrioritizer
}

func TestPrioritizerTestSuite(t *testing.T) {
	suite.Run(t, new(PrioritizerTestSuite))
}

func (s *PrioritizerTestSuite) SetupTest() {
	s.KeeperTestHelper.Setup()
	logger, err := log.NewDefaultLogger(log.LogFormatPlain, "info")
	require.NoError(s.T(), err)
	s.prioritizer = app.NewSeiTxPrioritizer(logger, &s.App.EvmKeeper, &s.App.UpgradeKeeper, &s.App.ParamsKeeper)
}

var (
	_ sdk.FeeTx = (*mockFeeTx)(nil)
	_ sdk.Tx    = (*mockTx)(nil)
)

type mockFeeTx struct {
	sdk.Tx
	fees sdk.Coins
	gas  uint64
	msgs []sdk.Msg
}

func (tx *mockFeeTx) FeePayer() sdk.AccAddress   { return nil }
func (tx *mockFeeTx) FeeGranter() sdk.AccAddress { return nil }
func (tx *mockFeeTx) GetFee() sdk.Coins          { return tx.fees }
func (tx *mockFeeTx) GetGas() uint64             { return tx.gas }
func (tx *mockFeeTx) GetMsgs() []sdk.Msg         { return tx.msgs }

type mockTx struct {
	msgs        []sdk.Msg
	gasEstimate uint64
}

func (tx *mockTx) GetGasEstimate() uint64    { return tx.gasEstimate }
func (tx *mockTx) GetMsgs() []sdk.Msg        { return tx.msgs }
func (*mockTx) ValidateBasic() error         { return nil }
func (*mockTx) GetSigners() []sdk.AccAddress { return nil }

func (s *PrioritizerTestSuite) TestGetTxPriority() {
	var (
		zeroValueTx    = func(*PrioritizerTestSuite) sdk.Tx { return &mockTx{} }
		zeroValueFeeTx = func(*PrioritizerTestSuite) sdk.Tx { return &mockFeeTx{} }
		zeroGasFeeTx   = func(*PrioritizerTestSuite) sdk.Tx {
			return &mockFeeTx{
				gas: 0,
			}
		}
		oracleVoteTx = func(s *PrioritizerTestSuite) sdk.Tx {
			return &mockFeeTx{
				msgs: []sdk.Msg{&oracletypes.MsgAggregateExchangeRateVote{}},
			}
		}
	)

	for _, tc := range []struct {
		name          string
		givenTx       func(s *PrioritizerTestSuite) sdk.Tx
		givenContext  func(sdk.Context) sdk.Context
		wantPriority  int64
		wantErr       string
		expectedErrAs interface{}
	}{
		{
			name:    "unexpected Tx type is error",
			givenTx: zeroValueTx,
			wantErr: "must either be EVM or Fee",
		},
		{
			name:    "context with priority present is context priority",
			givenTx: zeroValueFeeTx,
			givenContext: func(ctx sdk.Context) sdk.Context {
				return ctx.WithPriority(123)
			},
			wantPriority: 123,
		},
		{
			name:         "oracle Tx type is oracle priority",
			givenTx:      oracleVoteTx,
			wantPriority: antedecorators.OraclePriority,
		},
		{
			name:         "zero gas FeeTx is zero priority",
			givenTx:      zeroGasFeeTx,
			wantPriority: 0,
		},
		{
			name: "cosmos tx with denominators is has priority of smallest demon multiplier",
			givenTx: func(s *PrioritizerTestSuite) sdk.Tx {
				s.App.ParamsKeeper.SetFeesParams(s.Ctx, xparamtypes.FeesParams{
					AllowedFeeDenoms: []string{"fish", "lobster"},
				})
				return &mockFeeTx{
					gas: 4_200,
					fees: []sdk.Coin{
						{Denom: "fish", Amount: sdk.NewInt(230_000_000)},
						{Denom: "lobster", Amount: sdk.NewInt(290_000_000_000)},
					},
				}
			},
			wantPriority: cosmostypes.NewInt(230_000_000).QuoRaw(4_200).Int64(),
		},
	} {
		s.T().Run(tc.name, func(t *testing.T) {
			s.SetupTest()
			tx := tc.givenTx(s)
			ctx := s.Ctx
			if tc.givenContext != nil {
				ctx = tc.givenContext(ctx)
			}
			gotPriority, gotErr := s.prioritizer.GetTxPriorityHint(ctx, tx)
			if tc.wantErr != "" {
				require.Error(t, gotErr)
				require.ErrorContains(t, gotErr, tc.wantErr)
			} else {
				require.NoError(t, gotErr)
				require.Equal(t, tc.wantPriority, gotPriority)
			}
		})
	}
}
