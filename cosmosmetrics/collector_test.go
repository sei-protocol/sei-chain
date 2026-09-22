package cosmosmetrics

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

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

type fakeStaking struct {
	validators []stakingtypes.Validator
	calls      int
}

func (f *fakeStaking) GetParams(sdk.Context) stakingtypes.Params {
	f.calls++
	p := stakingtypes.DefaultParams()
	p.MaxValidators = 50
	p.BondDenom = testDenom
	return p
}
func (f *fakeStaking) BondDenom(sdk.Context) string { return testDenom }
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
func (f *fakeStaking) GetRedelegations(sdk.Context, sdk.AccAddress, uint16) []stakingtypes.Redelegation {
	return nil
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
	if err != nil {
		t.Fatal(err)
	}
	v.Tokens = sdk.NewInt(tokens)
	v.DelegatorShares = sdk.NewDec(tokens)
	v.Status = status
	v.Commission.Rate = sdk.NewDecWithPrec(5, 2)
	return v
}

// newTestReader installs a manual OTel reader as the global provider for the test.
func newTestReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	return reader
}

func newTestCollector(t *testing.T, staking *fakeStaking, distribution fakeDistribution) (*Collector, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	wallet := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	cfg := DefaultConfig
	cfg.Enabled = true
	cfg.RefreshInterval = time.Hour
	cfg.WalletAddresses = []string{wallet}
	c, err := NewCollector(cfg, Keepers{
		Staking:      staking,
		Slashing:     fakeSlashing{},
		Distribution: distribution,
		Bank:         fakeBank{},
	}, func() (sdk.Context, error) { return sdk.Context{}.WithContext(context.Background()), nil }, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return c, logs
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var out []metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		out = append(out, sm.Metrics...)
	}
	return out
}

// gaugeValue finds the gauge of name whose attribute set equals attrs.
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
	c, logs := newTestCollector(t, staking, fakeDistribution{})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Stop)
	waitForSnapshot(t, c)

	metrics := collect(t, reader)
	got := map[string]bool{}
	for _, m := range metrics {
		got[m.Name] = true
	}
	for _, name := range []string{
		"cosmos_params_max_validators", "cosmos_params_signed_blocks_window", "cosmos_params_community_tax",
		"cosmos_general_bonded_tokens", "cosmos_general_community_pool", "cosmos_general_supply_total",
		"cosmos_validators_active", "cosmos_validators_rank", "cosmos_validators_missed_blocks",
		"cosmos_wallet_balance", "cosmos_wallet_delegations",
	} {
		if !got[name] {
			t.Errorf("%s was not collected", name)
		}
	}
	if logs.Len() != 0 {
		t.Errorf("unexpected log output: %s", logs.String())
	}

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
		if !ok {
			t.Errorf("%s%v has no data point", tc.name, tc.attrs)
			continue
		}
		if got != tc.want {
			t.Errorf("%s%v = %v, want %v", tc.name, tc.attrs, got, tc.want)
		}
	}
	if _, ok := gaugeValue(metrics, "cosmos_validators_missed_blocks", map[string]string{"address": unbonded.OperatorAddress, "moniker": "valc"}); ok {
		t.Error("missed blocks reported for a validator outside the active set")
	}

	c.Stop()
	if len(collect(t, reader)) != 0 {
		t.Error("gauges still observed after Stop")
	}
}

func waitForSnapshot(t *testing.T, c *Collector) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.snapshot.Load() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the first refresh did not complete")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func unbondedCons(t *testing.T, v stakingtypes.Validator) sdk.ConsAddress {
	t.Helper()
	addr, err := v.GetConsAddr()
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestObserveServesTheSnapshotBetweenRefreshes(t *testing.T) {
	reader := newTestReader(t)
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, _ := newTestCollector(t, staking, fakeDistribution{})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Stop)
	waitForSnapshot(t, c)

	collect(t, reader)
	collect(t, reader)
	if staking.calls != 1 {
		t.Fatalf("state was read %d times within one refresh interval, want 1", staking.calls)
	}
	c.refresh()
	if staking.calls != 2 {
		t.Fatalf("state was read %d times after a refresh, want 2", staking.calls)
	}
}

func TestRefreshSurvivesAFailedRead(t *testing.T) {
	reader := newTestReader(t)
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, logs := newTestCollector(t, staking, fakeDistribution{rewardsErr: errors.New("boom")})
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Stop)
	waitForSnapshot(t, c)
	if !strings.Contains(logs.String(), "boom") {
		t.Fatalf("the rewards failure was not logged: %s", logs.String())
	}
	before := len(collect(t, reader))
	if before == 0 {
		t.Fatal("a failed rewards read must not drop the other gauges")
	}

	c.queryCtx = func() (sdk.Context, error) { return sdk.Context{}, errors.New("no state") }
	c.refresh()
	if !strings.Contains(logs.String(), "no state") {
		t.Fatalf("the missing query context was not logged: %s", logs.String())
	}
	if after := len(collect(t, reader)); after != before {
		t.Fatalf("a failed read replaced the snapshot: %d metrics, want %d", after, before)
	}

	c.queryCtx = func() (sdk.Context, error) { return sdk.Context{}.WithContext(context.Background()), nil }
	c.keepers.Staking = nil
	c.refresh()
	if !strings.Contains(logs.String(), "panicked") {
		t.Fatalf("the panic was not logged: %s", logs.String())
	}
	if after := len(collect(t, reader)); after != before {
		t.Fatalf("a panicking read replaced the snapshot: %d metrics, want %d", after, before)
	}
}

func TestObserveTxResultsReportsLargeTransfersOnly(t *testing.T) {
	reader := newTestReader(t)
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, _ := newTestCollector(t, staking, fakeDistribution{})
	c.transfers = newTransferRecorder(c.inst.bankTransferAmount, 1_000)

	transfer := func(amount string) abci.Event {
		return abci.Event{Type: "transfer", Attributes: []abci.EventAttribute{
			{Key: []byte("recipient"), Value: []byte("sei1to")},
			{Key: []byte("sender"), Value: []byte("sei1from")},
			{Key: []byte("amount"), Value: []byte(amount)},
		}}
	}
	c.ObserveTxResults(context.Background(), []*abci.ExecTxResult{
		{Code: 0, Events: []abci.Event{transfer("999usei"), transfer("5000usei,20factory/x/y")}},
		{Code: 1, Events: []abci.Event{transfer("7000usei")}},
		nil,
	})

	metrics := collect(t, reader)
	var points int
	for _, m := range metrics {
		if m.Name == "cosmos_bank_transfer_amount" {
			points += len(m.Data.(metricdata.Gauge[float64]).DataPoints)
		}
	}
	if points != 1 {
		t.Fatalf("got %d transfer data points, want 1", points)
	}
	got, ok := gaugeValue(metrics, "cosmos_bank_transfer_amount", map[string]string{"denom": "usei", "sender": "sei1from", "recipient": "sei1to"})
	if !ok || got != 5000 {
		t.Fatalf("transfer amount = %v (%v), want 5000", got, ok)
	}
}
