package cosmosmetrics

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

const testDenom = "usei"

var testReader = sync.OnceValue(func() *sdkmetric.ManualReader {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	return reader
})

type fakeStaking struct {
	bondDenom     string
	validators    []stakingtypes.Validator
	redelegations int
	calls         int
}

func (f *fakeStaking) GetParams(sdk.Context) stakingtypes.Params {
	f.calls++
	p := stakingtypes.DefaultParams()
	p.MaxValidators = 50
	p.BondDenom = testDenom
	return p
}
func (f *fakeStaking) BondDenom(sdk.Context) string {
	if f.bondDenom != "" {
		return f.bondDenom
	}
	return testDenom
}
func (f *fakeStaking) GetAllValidators(sdk.Context) []stakingtypes.Validator {
	return append([]stakingtypes.Validator(nil), f.validators...)
}
func (f *fakeStaking) GetValidator(_ sdk.Context, addr sdk.ValAddress) (stakingtypes.Validator, bool) {
	for _, v := range f.validators {
		if v.OperatorAddress == addr.String() {
			return v, true
		}
	}
	return stakingtypes.Validator{}, false
}
func (f *fakeStaking) GetBondedPool(sdk.Context) authtypes.ModuleAccountI {
	return authtypes.NewEmptyModuleAccount(stakingtypes.BondedPoolName)
}
func (f *fakeStaking) GetNotBondedPool(sdk.Context) authtypes.ModuleAccountI {
	return authtypes.NewEmptyModuleAccount(stakingtypes.NotBondedPoolName)
}
func (f *fakeStaking) GetAllDelegatorDelegations(_ sdk.Context, d sdk.AccAddress) []stakingtypes.Delegation {
	return []stakingtypes.Delegation{{DelegatorAddress: d.String(), ValidatorAddress: f.validators[0].OperatorAddress, Shares: sdk.NewDec(3_000_000)}}
}
func (f *fakeStaking) GetUnbondingDelegations(sdk.Context, sdk.AccAddress, uint16) []stakingtypes.UnbondingDelegation {
	return nil
}
func (f *fakeStaking) GetRedelegations(_ sdk.Context, d sdk.AccAddress, limit uint16) []stakingtypes.Redelegation {
	n := min(f.redelegations, int(limit))
	reds := make([]stakingtypes.Redelegation, n)
	for i := range reds {
		reds[i] = stakingtypes.Redelegation{
			DelegatorAddress:    d.String(),
			ValidatorSrcAddress: sdk.ValAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String(),
			ValidatorDstAddress: f.validators[0].OperatorAddress,
			Entries:             []stakingtypes.RedelegationEntry{{InitialBalance: sdk.NewInt(1)}},
		}
	}
	return reds
}

type fakeSlashing struct{}

func (fakeSlashing) GetParams(sdk.Context) slashingtypes.Params {
	p := slashingtypes.DefaultParams()
	p.SignedBlocksWindow = 10_000
	return p
}
func (fakeSlashing) GetValidatorSigningInfo(sdk.Context, sdk.ConsAddress) (slashingtypes.ValidatorSigningInfo, bool) {
	return slashingtypes.ValidatorSigningInfo{MissedBlocksCounter: 7}, true
}

type fakeDistribution struct{ rewardsErr error }

func (fakeDistribution) GetParams(sdk.Context) distrtypes.Params { return distrtypes.DefaultParams() }
func (fakeDistribution) GetFeePoolCommunityCoins(sdk.Context) sdk.DecCoins {
	return sdk.NewDecCoins(sdk.NewDecCoin(testDenom, sdk.NewInt(5_000_000)), sdk.NewDecCoin("factory/x/y", sdk.NewInt(9)))
}
func (f fakeDistribution) DelegationTotalRewards(context.Context, *distrtypes.QueryDelegationTotalRewardsRequest) (*distrtypes.QueryDelegationTotalRewardsResponse, error) {
	if f.rewardsErr != nil {
		return nil, f.rewardsErr
	}
	return &distrtypes.QueryDelegationTotalRewardsResponse{}, nil
}

type fakeBank struct{}

func (fakeBank) GetBalance(_ sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	if addr.Equals(authtypes.NewEmptyModuleAccount(stakingtypes.BondedPoolName).GetAddress()) {
		return sdk.NewInt64Coin(denom, 700)
	}
	return sdk.NewInt64Coin(denom, 2_500_000)
}
func (fakeBank) GetSupply(_ sdk.Context, denom string) sdk.Coin {
	return sdk.NewInt64Coin(denom, 10_000_000_000)
}

func newValidator(t *testing.T, seed byte, tokens int64, status stakingtypes.BondStatus) stakingtypes.Validator {
	t.Helper()
	pk := ed25519.GenPrivKeyFromSecret([]byte{seed}).PubKey()
	v, err := stakingtypes.NewValidator(sdk.ValAddress(bytes.Repeat([]byte{seed}, 20)), pk, stakingtypes.Description{Moniker: "val" + string(rune('a'+seed))})
	require.NoError(t, err)
	v.Tokens = sdk.NewInt(tokens)
	v.DelegatorShares = sdk.NewDec(tokens)
	v.Status = status
	v.Commission.Rate = sdk.NewDecWithPrec(5, 2)
	return v
}

// newTestReader returns the manual OTel reader that the package-level instruments report to.
func newTestReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	return testReader()
}

func testQueryCtx() sdk.Context {
	return sdk.Context{}.WithContext(context.Background()).WithBlockHeight(1)
}

func newTestReporter(t *testing.T, staking *fakeStaking, distribution fakeDistribution) (*Reporter, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	prev := logger
	logger = slog.New(slog.NewTextHandler(logs, nil))
	t.Cleanup(func() { logger = prev })
	wallet := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	cfg := DefaultConfig
	cfg.Enabled = true
	cfg.RefreshInterval = time.Hour
	cfg.WalletAddresses = []string{wallet}
	c, err := NewReporter(cfg, Keepers{
		Staking:      staking,
		Slashing:     fakeSlashing{},
		Distribution: distribution,
		Bank:         fakeBank{},
	}, func() (sdk.Context, error) { return testQueryCtx(), nil })
	require.NoError(t, err)
	return c, logs
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	var out []metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		out = append(out, sm.Metrics...)
	}
	return out
}

// gaugeValue finds the gauge of name whose attribute set equals attrs.
func counterValue[N int64 | float64](metrics []metricdata.Metrics, name string, attrs map[string]string) (N, bool) {
	want := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		want = append(want, attribute.String(k, v))
	}
	wantSet := attribute.NewSet(want...)
	for _, m := range metrics {
		if m.Name != name {
			continue
		}
		sum, ok := m.Data.(metricdata.Sum[N])
		if !ok {
			continue
		}
		for _, dp := range sum.DataPoints {
			if dp.Attributes.Equals(&wantSet) {
				return dp.Value, true
			}
		}
	}
	return 0, false
}

func gaugeValue(metrics []metricdata.Metrics, name string, attrs map[string]string) (float64, bool) {
	want := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		want = append(want, attribute.String(k, v))
	}
	wantSet := attribute.NewSet(want...)
	for _, m := range metrics {
		if m.Name != name {
			continue
		}
		g, ok := m.Data.(metricdata.Gauge[float64])
		if !ok {
			continue
		}
		for _, dp := range g.DataPoints {
			if dp.Attributes.Equals(&wantSet) {
				return dp.Value, true
			}
		}
	}
	return 0, false
}

func TestStartReportsTheExporterGauges(t *testing.T) {
	reader := newTestReader(t)
	staking := &fakeStaking{validators: []stakingtypes.Validator{
		newValidator(t, 1, 4_000_000, stakingtypes.Bonded),
		newValidator(t, 2, 9_000_000, stakingtypes.Unbonded),
	}}
	c, logs := newTestReporter(t, staking, fakeDistribution{})
	require.NoError(t, c.Start())
	t.Cleanup(c.Stop)
	waitForSnapshot(t, c)

	metrics := collect(t, reader)
	names := map[string]bool{}
	for _, m := range metrics {
		names[m.Name] = true
	}
	expected := []string{
		"cosmos_params_max_validators", "cosmos_params_signed_blocks_window", "cosmos_params_community_tax",
		"cosmos_general_bonded_tokens", "cosmos_general_community_pool", "cosmos_general_supply_total",
		"cosmos_validators_active", "cosmos_validators_rank", "cosmos_validators_missed_blocks",
		"cosmos_wallet_balance", "cosmos_wallet_delegations",
	}
	for _, name := range expected {
		assert.True(t, names[name], "%s was not collected", name)
	}
	assert.Empty(t, logs.String(), "unexpected log output")

	bonded, unbonded := staking.validators[0], staking.validators[1]
	wallet := c.wallets[0].String()
	for _, tc := range []struct {
		name  string
		attrs map[string]string
		want  float64
	}{
		{"cosmos_params_max_validators", nil, 50},
		{"cosmos_general_bonded_tokens", nil, 700},
		{"cosmos_general_supply_total", map[string]string{"denom": "usei"}, 10_000},
		{"cosmos_general_community_pool", map[string]string{"denom": "usei"}, 5},
		{"cosmos_general_community_pool", map[string]string{"denom": "factory/x/y"}, 9},
		{"cosmos_validators_rank", map[string]string{"address": unbonded.OperatorAddress, "moniker": "valc"}, 1},
		{"cosmos_validators_rank", map[string]string{"address": bonded.OperatorAddress, "moniker": "valb"}, 2},
		{"cosmos_validators_tokens", map[string]string{"address": bonded.OperatorAddress, "moniker": "valb", "denom": "usei"}, 4},
		{"cosmos_validators_missed_blocks", map[string]string{"address": bonded.OperatorAddress, "moniker": "valb"}, 7},
		{"cosmos_validators_active", map[string]string{"address": unbonded.OperatorAddress, "moniker": "valc", "pubkey_hash": strings.ToUpper(hex.EncodeToString(unbondedCons(t, unbonded)))}, 0},
		{"cosmos_wallet_balance", map[string]string{"address": wallet, "denom": "usei"}, 2.5},
		{"cosmos_wallet_delegations", map[string]string{"address": wallet, "denom": "usei", "delegated_to": bonded.OperatorAddress}, 3},
	} {
		got, ok := gaugeValue(metrics, tc.name, tc.attrs)
		if assert.True(t, ok, "%s%v has no data point", tc.name, tc.attrs) {
			assert.Equal(t, tc.want, got, "%s%v", tc.name, tc.attrs)
		}
	}
	_, ok := gaugeValue(metrics, "cosmos_validators_missed_blocks", map[string]string{"address": unbonded.OperatorAddress, "moniker": "valc"})
	assert.False(t, ok, "missed blocks reported for a validator outside the active set")

	c.Stop()
	names = map[string]bool{}
	for _, m := range collect(t, reader) {
		names[m.Name] = true
	}
	for _, name := range expected {
		assert.False(t, names[name], "%s still observed after Stop", name)
	}
}

func waitForSnapshot(t *testing.T, c *Reporter) {
	t.Helper()
	require.Eventually(t, func() bool { return c.snapshot.Load() != nil }, 5*time.Second, 5*time.Millisecond,
		"the first refresh did not complete")
}

func unbondedCons(t *testing.T, v stakingtypes.Validator) sdk.ConsAddress {
	t.Helper()
	addr, err := v.GetConsAddr()
	require.NoError(t, err)
	return addr
}

func TestObserveServesTheSnapshotBetweenRefreshes(t *testing.T) {
	reader := newTestReader(t)
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, _ := newTestReporter(t, staking, fakeDistribution{})
	require.NoError(t, c.Start())
	t.Cleanup(c.Stop)
	waitForSnapshot(t, c)

	collect(t, reader)
	collect(t, reader)
	require.Equal(t, 1, staking.calls, "state reads within one refresh interval")
	c.refresh()
	require.Equal(t, 2, staking.calls, "state reads after a refresh")
}

func TestReadLogsTruncatedWalletEntries(t *testing.T) {
	newTestReader(t)
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}, redelegations: maxWalletEntries + 5}
	c, logs := newTestReporter(t, staking, fakeDistribution{})
	c.refresh()
	require.Contains(t, logs.String(), "redelegations truncated", "the truncated read was not logged")
	require.NotNil(t, c.snapshot.Load(), "a truncated read must still publish a snapshot")
}

func TestRefreshSurvivesAFailedRead(t *testing.T) {
	reader := newTestReader(t)
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, logs := newTestReporter(t, staking, fakeDistribution{rewardsErr: errors.New("boom")})
	require.NoError(t, c.Start())
	t.Cleanup(c.Stop)
	waitForSnapshot(t, c)
	require.Contains(t, logs.String(), "boom", "the rewards failure was not logged")
	before := len(collect(t, reader))
	require.NotZero(t, before, "a failed rewards read must not drop the other gauges")

	c.queryCtx = func() (sdk.Context, error) { return sdk.Context{}, errors.New("no state") }
	c.refresh()
	require.Contains(t, logs.String(), "no state", "the missing query context was not logged")
	require.Len(t, collect(t, reader), before, "a failed read replaced the snapshot")

	c.queryCtx = func() (sdk.Context, error) { return testQueryCtx(), nil }
	c.keepers.Staking = nil
	c.refresh()
	require.Contains(t, logs.String(), "panicked", "the panic was not logged")
	require.Len(t, collect(t, reader), before, "a panicking read replaced the snapshot")
}

func TestObserveTxResultsCountsLargeTransfersOnly(t *testing.T) {
	reader := newTestReader(t)
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, _ := newTestReporter(t, staking, fakeDistribution{})
	c.transfers = newTransferRecorder(cosmosMetrics.bankTransfersTotal, cosmosMetrics.bankTransferAmountTotal, "usei", 1_000)

	transfer := func(amount string) abci.Event {
		return abci.Event{Type: "transfer", Attributes: []abci.EventAttribute{
			{Key: []byte("recipient"), Value: []byte("sei1to")},
			{Key: []byte("sender"), Value: []byte("sei1from")},
			{Key: []byte("amount"), Value: []byte(amount)},
		}}
	}
	c.ObserveTxResults(context.Background(), []*abci.ExecTxResult{
		{Code: 0, Events: []abci.Event{transfer("999usei"), transfer("5000usei,20factory/x/y"), transfer("9000factory/x/y"), transfer("30factory/x/usei,2000usei")}},
		{Code: 1, Events: []abci.Event{transfer("7000usei")}},
		nil,
	})

	metrics := collect(t, reader)
	usei := map[string]string{"denom": "usei"}
	count, ok := counterValue[int64](metrics, "cosmos_bank_transfers_total", usei)
	require.True(t, ok, "the large transfers have no count")
	assert.Equal(t, int64(2), count)
	amount, ok := counterValue[float64](metrics, "cosmos_bank_transfer_amount_total", usei)
	require.True(t, ok, "the large transfers have no amount")
	assert.Equal(t, 7000.0, amount)
}

func TestObserveTxResultsFollowsBondDenomFromRefresh(t *testing.T) {
	reader := newTestReader(t)
	staking := &fakeStaking{bondDenom: "usei2", validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, _ := newTestReporter(t, staking, fakeDistribution{})
	c.transfers = newTransferRecorder(cosmosMetrics.bankTransfersTotal, cosmosMetrics.bankTransferAmountTotal, "usei", 1)
	c.refresh()

	c.ObserveTxResults(context.Background(), []*abci.ExecTxResult{{Code: 0, Events: []abci.Event{
		{Type: "transfer", Attributes: []abci.EventAttribute{{Key: []byte("amount"), Value: []byte("5usei2")}}},
	}}})

	count, ok := counterValue[int64](collect(t, reader), "cosmos_bank_transfers_total", map[string]string{"denom": "usei2"})
	require.True(t, ok, "the transfer in the refreshed bond denom was not counted")
	assert.Equal(t, int64(1), count)
}
