package bankquery

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	pquery "github.com/sei-protocol/sei-chain/precompiles/query"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkquery "github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/stretchr/testify/require"
)

type fakeEVMCaller struct {
	t              *testing.T
	wantBlock      *big.Int
	wantSeiAddress string
}

func (f fakeEVMCaller) CallContract(_ context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	require.NotNil(f.t, msg.To)
	require.Equal(f.t, f.wantBlock, blockNumber)

	require.Equal(f.t, common.HexToAddress(bank.BankAddress), *msg.To)
	contractABI := bank.GetABI()
	method, err := contractABI.MethodById(msg.Data[:4])
	require.NoError(f.t, err)
	args, err := method.Inputs.Unpack(msg.Data[4:])
	require.NoError(f.t, err)

	switch method.Name {
	case bank.BalanceForAddressMethod:
		require.Equal(f.t, []interface{}{f.wantSeiAddress, "usei"}, args)
		return method.Outputs.Pack(big.NewInt(123))
	case bank.AllBalancesForAddressMethod:
		require.Equal(f.t, []interface{}{f.wantSeiAddress}, args)
		return method.Outputs.Pack([]bank.CoinBalance{
			{Amount: big.NewInt(7), Denom: "uatom"},
			{Amount: big.NewInt(11), Denom: "usei"},
		})
	case bank.SpendableBalancesForAddressMethod:
		require.Equal(f.t, []interface{}{f.wantSeiAddress}, args)
		return method.Outputs.Pack([]bank.CoinBalance{
			{Amount: big.NewInt(7), Denom: "uatom"},
			{Amount: big.NewInt(11), Denom: "usei"},
		})
	case bank.DenomMetadataMethod:
		require.Equal(f.t, []interface{}{"usei"}, args)
		return method.Outputs.Pack(bank.Metadata{
			Description: "Sei base denom",
			DenomUnits: []bank.DenomUnit{
				{Denom: "usei", Exponent: 0, Aliases: []string{"microsei"}},
				{Denom: "sei", Exponent: 6, Aliases: []string{}},
			},
			Base:    "usei",
			Display: "sei",
			Name:    "Sei",
			Symbol:  "SEI",
		})
	case bank.ParamsMethod:
		return method.Outputs.Pack(bank.Params{
			SendEnabled:        []bank.SendEnabled{{Denom: "usei", Enabled: true}},
			DefaultSendEnabled: true,
		})
	default:
		f.t.Fatalf("unexpected method %s", method.Name)
		return nil, nil
	}
}

func TestGeneratedQueryClientUsesBankPrecompileBindings(t *testing.T) {
	seiAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	caller := fakeEVMCaller{
		t:              t,
		wantBlock:      big.NewInt(99),
		wantSeiAddress: seiAddr.String(),
	}
	client := banktypes.NewQueryClient(pquery.NewConn(
		caller,
		Registry(),
		pquery.WithDefaultBlockNumber(99),
	))

	balance, err := client.Balance(context.Background(), &banktypes.QueryBalanceRequest{
		Address: seiAddr.String(),
		Denom:   "usei",
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoin("usei", sdk.NewInt(123)), *balance.Balance)

	allBalances, err := client.AllBalances(context.Background(), &banktypes.QueryAllBalancesRequest{
		Address:    seiAddr.String(),
		Pagination: &sdkquery.PageRequest{Limit: 1, CountTotal: true},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uatom", sdk.NewInt(7))), allBalances.Balances)
	require.Equal(t, []byte("usei"), allBalances.Pagination.NextKey)
	require.Equal(t, uint64(2), allBalances.Pagination.Total)

	spendableBalances, err := client.SpendableBalances(context.Background(), &banktypes.QuerySpendableBalancesRequest{
		Address:    seiAddr.String(),
		Pagination: &sdkquery.PageRequest{Offset: 1},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(11))), spendableBalances.Balances)

	metadata, err := client.DenomMetadata(context.Background(), &banktypes.QueryDenomMetadataRequest{Denom: "usei"})
	require.NoError(t, err)
	require.Equal(t, banktypes.Metadata{
		Description: "Sei base denom",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: "usei", Exponent: 0, Aliases: []string{"microsei"}},
			{Denom: "sei", Exponent: 6, Aliases: []string{}},
		},
		Base:    "usei",
		Display: "sei",
		Name:    "Sei",
		Symbol:  "SEI",
	}, metadata.Metadata)

	params, err := client.Params(context.Background(), &banktypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, banktypes.Params{
		SendEnabled:        []*banktypes.SendEnabled{{Denom: "usei", Enabled: true}},
		DefaultSendEnabled: true,
	}, params.Params)
}
