package app_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	cosmostypes "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	xparamtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
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
	s.prioritizer = app.NewSeiTxPrioritizer(&s.App.EvmKeeper, &s.App.UpgradeKeeper, &s.App.ParamsKeeper)
}

var (
	_ cosmostypes.FeeTx = (*mockFeeTx)(nil)
	_ cosmostypes.Tx    = (*mockTx)(nil)
)

type mockFeeTx struct {
	cosmostypes.Tx
	fees cosmostypes.Coins
	gas  uint64
	msgs []cosmostypes.Msg
}

func (tx *mockFeeTx) FeePayer() cosmostypes.AccAddress   { return nil }
func (tx *mockFeeTx) FeeGranter() cosmostypes.AccAddress { return nil }
func (tx *mockFeeTx) GetFee() cosmostypes.Coins          { return tx.fees }
func (tx *mockFeeTx) GetGas() uint64                     { return tx.gas }
func (tx *mockFeeTx) GetMsgs() []cosmostypes.Msg         { return tx.msgs }

type mockTx struct {
	msgs        []cosmostypes.Msg
	gasEstimate uint64
}

func (tx *mockTx) GetGasEstimate() uint64            { return tx.gasEstimate }
func (tx *mockTx) GetMsgs() []cosmostypes.Msg        { return tx.msgs }
func (*mockTx) ValidateBasic() error                 { return nil }
func (*mockTx) GetSigners() []cosmostypes.AccAddress { return nil }

func newNativeAssociateTx(t *testing.T) cosmostypes.Tx {
	t.Helper()

	privKey := testkeeper.MockPrivateKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	require.NoError(t, err)

	emptyData := make([]byte, 32)
	customMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(emptyData)) + string(emptyData)
	hash := crypto.Keccak256Hash([]byte(customMessage))
	sig, err := crypto.Sign(hash.Bytes(), key)
	require.NoError(t, err)
	r, ss, _, err := ethtx.DecodeSignature(sig)
	require.NoError(t, err)

	msg, err := evmtypes.NewMsgEVMTransaction(&ethtx.AssociateTx{
		V:             big.NewInt(int64(sig[64])).Bytes(),
		R:             r.Bytes(),
		S:             ss.Bytes(),
		CustomMessage: customMessage,
	})
	require.NoError(t, err)
	return &mockTx{msgs: []cosmostypes.Msg{msg}}
}

func (s *PrioritizerTestSuite) TestGetTxPriority() {
	var (
		zeroValueTx    = func(*PrioritizerTestSuite) cosmostypes.Tx { return &mockTx{} }
		zeroValueFeeTx = func(*PrioritizerTestSuite) cosmostypes.Tx { return &mockFeeTx{} }
		zeroGasFeeTx   = func(*PrioritizerTestSuite) cosmostypes.Tx {
			return &mockFeeTx{
				gas: 0,
			}
		}
		associateMsgTx = func(s *PrioritizerTestSuite) cosmostypes.Tx {
			s.App.ParamsKeeper.SetFeesParams(s.Ctx, xparamtypes.FeesParams{AllowedFeeDenoms: []string{"fish"}})
			return &mockFeeTx{
				fees: cosmostypes.NewCoins(cosmostypes.NewInt64Coin("fish", 1_000)),
				gas:  100,
				msgs: []cosmostypes.Msg{
					evmtypes.NewMsgAssociate(cosmostypes.AccAddress(make([]byte, 20)), "test"),
				},
			}
		}
		nativeAssociateTx = func(s *PrioritizerTestSuite) cosmostypes.Tx {
			return newNativeAssociateTx(s.T())
		}
	)

	for _, tc := range []struct {
		name          string
		givenTx       func(s *PrioritizerTestSuite) cosmostypes.Tx
		givenContext  func(cosmostypes.Context) cosmostypes.Context
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
			givenContext: func(ctx cosmostypes.Context) cosmostypes.Context {
				return ctx.WithPriority(123)
			},
			wantPriority: 123,
		},
		{
			name:         "deprecated MsgAssociate uses fee priority",
			givenTx:      associateMsgTx,
			wantPriority: 10,
		},
		{
			name:         "native AssociateTx keeps associate priority",
			givenTx:      nativeAssociateTx,
			wantPriority: antedecorators.EVMAssociatePriority,
		},
		{
			name:         "zero gas FeeTx is zero priority",
			givenTx:      zeroGasFeeTx,
			wantPriority: 0,
		},
		{
			name: "cosmos tx with denominators is has priority of smallest demon multiplier",
			givenTx: func(s *PrioritizerTestSuite) cosmostypes.Tx {
				s.App.ParamsKeeper.SetFeesParams(s.Ctx, xparamtypes.FeesParams{
					AllowedFeeDenoms: []string{"fish", "lobster"},
				})
				return &mockFeeTx{
					gas: 4_200,
					fees: []cosmostypes.Coin{
						{Denom: "fish", Amount: cosmostypes.NewInt(230_000_000)},
						{Denom: "lobster", Amount: cosmostypes.NewInt(290_000_000_000)},
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
