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
	testLoopToken = common.HexToAddress("0x000000000000000000000000000000000000100b")
	testAssocAddr = common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
)

// fakeEVM answers ERC-20 static calls for testToken, exhausts the caller's gas meter for
// testLoopToken the way a looping contract does, and reverts for anything else. One wallet is
// associated to testAssocAddr; every other wallet resolves to its cast address. Its symbol answer
// can be changed between refreshes, or replaced wholesale with symbolRaw to mimic a contract whose
// symbol() is not an ABI string.
type fakeEVM struct {
	associated sdk.AccAddress
	balances   map[common.Address]*big.Int
	symbol     *string
	symbolRaw  []byte
	gasLimits  *[]uint64
}

func (f fakeEVM) GetEVMAddressOrDefault(_ sdk.Context, addr sdk.AccAddress) common.Address {
	if addr.Equals(f.associated) {
		return testAssocAddr
	}
	return common.BytesToAddress(addr)
}

func (f fakeEVM) StaticCallEVM(ctx sdk.Context, _ sdk.AccAddress, to *common.Address, data []byte) ([]byte, error) {
	if f.gasLimits != nil {
		*f.gasLimits = append(*f.gasLimits, ctx.GasMeter().Limit())
	}
	if *to == testLoopToken {
		ctx.GasMeter().ConsumeGas(ctx.GasMeter().Limit()+1, "loop")
	}
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
		if f.symbolRaw != nil {
			return f.symbolRaw, nil
		}
		if f.symbol != nil {
			return method.Outputs.Pack(*f.symbol)
		}
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
	for _, w := range wallets {
		attrs := map[string]string{"address": w.String(), "token": testToken.Hex()}
		got, ok := gaugeValue(metrics, "cosmos_wallet_erc20_read_ok", attrs)
		require.True(t, ok, "cosmos_wallet_erc20_read_ok%v has no data point", attrs)
		require.Equal(t, 1.0, got)
	}
}

// readOK returns cosmos_wallet_erc20_read_ok by token for the first wallet and the tokens that
// reported a balance, in configuration order.
func readOK(t *testing.T, c *Reporter) (ok map[common.Address]float64, reported []common.Address) {
	t.Helper()
	require.NotNil(t, c.snapshot.Load(), "a failed token read must still publish a snapshot")
	ok = map[common.Address]float64{}
	for _, s := range *c.snapshot.Load() {
		switch s.inst {
		case cosmosMetrics.walletERC20Balance:
			reported = append(reported, common.HexToAddress(observedAttr(t, s, "token")))
		case cosmosMetrics.walletERC20ReadOK:
			if observedAttr(t, s, "address") == testWallets()[0].String() {
				ok[common.HexToAddress(observedAttr(t, s, "token"))] = s.value
			}
		}
	}
	return ok, reported
}

func TestReadSkipsATokenThatReverts(t *testing.T) {
	newTestReader(t)
	evm := fakeEVM{balances: map[common.Address]*big.Int{}}
	c, _, logs := newERC20Reporter(t, []string{testBadToken.Hex(), testToken.Hex()}, evm)
	c.refresh()
	require.Contains(t, logs.String(), "erc20 "+testBadToken.Hex(), "the reverting token was not logged")
	ok, reported := readOK(t, c)
	require.Equal(t, []common.Address{testToken, testToken}, reported, "one sample per wallet for the token that answers")
	require.Equal(t, map[common.Address]float64{testBadToken: 0, testToken: 1}, ok, "read_ok must cover the failed token too")
}

func TestReadBoundsEVMGasAndSkipsATokenThatExhaustsIt(t *testing.T) {
	newTestReader(t)
	var limits []uint64
	evm := fakeEVM{balances: map[common.Address]*big.Int{}, gasLimits: &limits}
	c, _, logs := newERC20Reporter(t, []string{testLoopToken.Hex(), testToken.Hex()}, evm)
	c.refresh()
	require.Contains(t, logs.String(), "erc20 "+testLoopToken.Hex(), "the looping token was not logged")
	ok, reported := readOK(t, c)
	require.Equal(t, []common.Address{testToken, testToken}, reported, "the refresh must survive the looping token")
	require.Equal(t, map[common.Address]float64{testLoopToken: 0, testToken: 1}, ok)
	require.NotEmpty(t, limits)
	for _, l := range limits {
		require.Equal(t, erc20CallGasLimit, l, "every static call must run under the finite limit")
	}
}

func TestReadKeepsTheSymbolItFirstRead(t *testing.T) {
	newTestReader(t)
	symbol := "USDC"
	evm := fakeEVM{balances: map[common.Address]*big.Int{}, symbol: &symbol}
	c, _, _ := newERC20Reporter(t, []string{testToken.Hex()}, evm)
	c.refresh()
	symbol = "USDC.n"
	c.refresh()
	for _, s := range *c.snapshot.Load() {
		if s.inst == cosmosMetrics.walletERC20Balance {
			require.Equal(t, "USDC", observedAttr(t, s, "symbol"), "the symbol label must not follow the contract")
		}
	}
}

func TestReadReportsATokenWhoseSymbolIsNotAString(t *testing.T) {
	newTestReader(t)
	bytes32 := make([]byte, 32)
	copy(bytes32, "MKR")
	evm := fakeEVM{balances: map[common.Address]*big.Int{}, symbolRaw: bytes32}
	c, _, logs := newERC20Reporter(t, []string{testToken.Hex()}, evm)
	c.refresh()
	ok, reported := readOK(t, c)
	require.Equal(t, []common.Address{testToken, testToken}, reported, "the balance must be reported without a symbol")
	require.Equal(t, map[common.Address]float64{testToken: 1}, ok)
	for _, s := range *c.snapshot.Load() {
		if s.inst == cosmosMetrics.walletERC20Balance {
			require.Equal(t, "", observedAttr(t, s, "symbol"))
		}
	}
	require.Empty(t, logs.String(), "a missing symbol is not an error")
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
