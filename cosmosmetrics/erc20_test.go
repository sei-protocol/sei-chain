package cosmosmetrics

import (
	"bytes"
	"errors"
	"log/slog"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

var (
	testToken     = common.HexToAddress("0x3894085Ef7Ff0f0aeDf52E2A2704928d1Ec074F1")
	testBadToken  = common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	testAssocAddr = common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
)

// fakeEVM answers ERC-20 static calls for testToken and reverts for anything else. One wallet is
// associated to testAssocAddr; every other wallet resolves to its cast address.
type fakeEVM struct {
	associated sdk.AccAddress
	balances   map[common.Address]*big.Int
}

func (f fakeEVM) GetEVMAddressOrDefault(_ sdk.Context, addr sdk.AccAddress) common.Address {
	if addr.Equals(f.associated) {
		return testAssocAddr
	}
	return common.BytesToAddress(addr)
}

func (f fakeEVM) StaticCallEVM(_ sdk.Context, _ sdk.AccAddress, to *common.Address, data []byte) ([]byte, error) {
	if *to != testToken {
		return nil, errors.New("execution reverted")
	}
	method, err := erc20ABI.MethodById(data[:4])
	if err != nil {
		return nil, err
	}
	switch method.Name {
	case "decimals":
		return method.Outputs.Pack(uint8(6))
	case "symbol":
		return method.Outputs.Pack("USDC")
	case "balanceOf":
		args, err := method.Inputs.Unpack(data[4:])
		if err != nil {
			return nil, err
		}
		balance := f.balances[args[0].(common.Address)]
		if balance == nil {
			balance = new(big.Int)
		}
		return method.Outputs.Pack(balance)
	}
	return nil, errors.New("unknown method")
}

func testWallets() []sdk.AccAddress {
	return []sdk.AccAddress{bytes.Repeat([]byte{9}, 20), bytes.Repeat([]byte{7}, 20)}
}

func observedAttr(t *testing.T, s sample, key string) string {
	t.Helper()
	attrs := metric.NewObserveConfig([]metric.ObserveOption{s.attrs}).Attributes()
	v, ok := attrs.Value(attribute.Key(key))
	require.True(t, ok, "sample without a %s attribute", key)
	return v.AsString()
}

func newERC20Reporter(t *testing.T, tokens []string, evm EVMKeeper) (*Reporter, []sdk.AccAddress, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	prev := logger
	logger = slog.New(slog.NewTextHandler(logs, nil))
	t.Cleanup(func() { logger = prev })
	wallets := testWallets()
	cfg := DefaultConfig
	cfg.Enabled = true
	cfg.RefreshInterval = time.Hour
	cfg.WalletAddresses = []string{wallets[0].String(), wallets[1].String()}
	cfg.ERC20Tokens = tokens
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, err := NewReporter(cfg, Keepers{
		Staking:      staking,
		Slashing:     fakeSlashing{},
		Distribution: fakeDistribution{},
		Bank:         fakeBank{},
		EVM:          evm,
	}, func() (sdk.Context, error) { return testQueryCtx(), nil })
	require.NoError(t, err)
	return c, wallets, logs
}

func TestReadReportsERC20BalancesAtTheWalletEVMAddress(t *testing.T) {
	reader := newTestReader(t)
	wallets := testWallets()
	evm := fakeEVM{associated: wallets[1], balances: map[common.Address]*big.Int{
		common.BytesToAddress(wallets[0]): big.NewInt(123_456_789),
		testAssocAddr:                     big.NewInt(5_000_000),
	}}
	c, _, logs := newERC20Reporter(t, []string{testToken.Hex()}, evm)
	require.NoError(t, c.Start())
	t.Cleanup(c.Stop)
	waitForSnapshot(t, c)

	metrics := collect(t, reader)
	for _, tc := range []struct {
		wallet  sdk.AccAddress
		evmAddr common.Address
		want    float64
	}{
		{wallets[0], common.BytesToAddress(wallets[0]), 123.456789},
		{wallets[1], testAssocAddr, 5},
	} {
		attrs := map[string]string{"address": tc.wallet.String(), "evm_address": tc.evmAddr.Hex(), "token": testToken.Hex(), "symbol": "USDC"}
		got, ok := gaugeValue(metrics, "cosmos_wallet_erc20_balance", attrs)
		require.True(t, ok, "cosmos_wallet_erc20_balance%v has no data point", attrs)
		require.Equal(t, tc.want, got)
	}
	require.Empty(t, logs.String(), "unexpected log output")
}

func TestReadSkipsATokenThatReverts(t *testing.T) {
	newTestReader(t)
	evm := fakeEVM{balances: map[common.Address]*big.Int{}}
	c, _, logs := newERC20Reporter(t, []string{testBadToken.Hex(), testToken.Hex()}, evm)
	c.refresh()
	require.Contains(t, logs.String(), "erc20 "+testBadToken.Hex(), "the reverting token was not logged")
	require.NotNil(t, c.snapshot.Load(), "a failed token read must still publish a snapshot")
	var reported []common.Address
	for _, s := range *c.snapshot.Load() {
		if s.inst == cosmosMetrics.walletERC20Balance {
			reported = append(reported, common.HexToAddress(observedAttr(t, s, "token")))
		}
	}
	require.Equal(t, []common.Address{testToken, testToken}, reported, "one sample per wallet for the token that answers")
}

func TestNewReporterRequiresAnEVMKeeperForTokens(t *testing.T) {
	cfg := DefaultConfig
	cfg.ERC20Tokens = []string{testToken.Hex()}
	_, err := NewReporter(cfg, Keepers{}, func() (sdk.Context, error) { return testQueryCtx(), nil })
	require.ErrorContains(t, err, "no EVM keeper")

	cfg.ERC20Tokens = []string{"sei1notanevmaddress"}
	_, err = NewReporter(cfg, Keepers{EVM: fakeEVM{}}, func() (sdk.Context, error) { return testQueryCtx(), nil })
	require.ErrorContains(t, err, "not a 0x address")
}
