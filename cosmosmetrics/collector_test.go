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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
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

type fakeOracle struct{}

func (fakeOracle) GetVotePenaltyCounter(sdk.Context, sdk.ValAddress) oracletypes.VotePenaltyCounter {
	return oracletypes.VotePenaltyCounter{MissCount: 1, AbstainCount: 2, SuccessCount: 3}
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

func newTestCollector(t *testing.T, staking *fakeStaking, distribution fakeDistribution) (*Collector, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	wallet := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	cfg := DefaultConfig
	cfg.Enabled = true
	cfg.WalletAddresses = []string{wallet}
	c, err := NewCollector(cfg, Keepers{
		Staking:      staking,
		Slashing:     fakeSlashing{},
		Distribution: distribution,
		Bank:         fakeBank{},
		Oracle:       fakeOracle{},
	}, func() (sdk.Context, error) { return sdk.Context{}.WithContext(context.Background()), nil }, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return c, logs
}

func TestCollectReportsTheExporterGauges(t *testing.T) {
	staking := &fakeStaking{validators: []stakingtypes.Validator{
		newValidator(t, 1, 4_000_000, stakingtypes.Bonded),
		newValidator(t, 2, 9_000_000, stakingtypes.Unbonded),
	}}
	c, logs := newTestCollector(t, staking, fakeDistribution{})
	reg := prometheus.NewPedanticRegistry()
	if err := c.Register(reg); err != nil {
		t.Fatal(err)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range families {
		got[f.GetName()] = true
	}
	for _, name := range []string{
		"cosmos_params_max_validators", "cosmos_params_signed_blocks_window", "cosmos_params_community_tax",
		"cosmos_general_bonded_tokens", "cosmos_general_community_pool", "cosmos_general_supply_total",
		"cosmos_validators_active", "cosmos_validators_rank", "cosmos_validators_missed_blocks",
		"cosmos_wallet_balance", "cosmos_wallet_delegations", "cosmos_oracle_vote_penalty_count",
	} {
		if !got[name] {
			t.Errorf("%s was not gathered", name)
		}
	}
	if logs.Len() != 0 {
		t.Errorf("unexpected log output: %s", logs.String())
	}

	bonded, unbonded := staking.validators[0], staking.validators[1]
	wallet := c.wallets[0].String()
	for _, tc := range []struct {
		name   string
		labels map[string]string
		want   float64
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
		{"cosmos_oracle_vote_penalty_count", map[string]string{"address": bonded.OperatorAddress, "moniker": "valb", "type": "abstain"}, 2},
	} {
		got, ok := sampleValue(families, tc.name, tc.labels)
		if !ok {
			t.Errorf("%s%v has no sample", tc.name, tc.labels)
			continue
		}
		if got != tc.want {
			t.Errorf("%s%v = %v, want %v", tc.name, tc.labels, got, tc.want)
		}
	}
	if _, ok := sampleValue(families, "cosmos_validators_missed_blocks", map[string]string{"address": unbonded.OperatorAddress, "moniker": "valc"}); ok {
		t.Error("missed blocks reported for a validator outside the active set")
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

// sampleValue finds the gauge of name whose label set equals labels.
func sampleValue(families []*dto.MetricFamily, name string, labels map[string]string) (float64, bool) {
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if len(m.GetLabel()) != len(labels) {
				continue
			}
			match := true
			for _, l := range m.GetLabel() {
				if labels[l.GetName()] != l.GetValue() {
					match = false
					break
				}
			}
			if match {
				return m.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

func TestCollectServesTheCacheWithinTheRefreshInterval(t *testing.T) {
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, _ := newTestCollector(t, staking, fakeDistribution{})
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }

	gather := func() {
		if _, err := testutil.CollectAndLint(c); err != nil {
			t.Fatal(err)
		}
	}
	gather()
	gather()
	if staking.calls != 1 {
		t.Fatalf("state was read %d times within one refresh interval, want 1", staking.calls)
	}
	now = now.Add(c.cfg.RefreshInterval)
	gather()
	if staking.calls != 2 {
		t.Fatalf("state was read %d times after the refresh interval elapsed, want 2", staking.calls)
	}
}

func TestCollectSurvivesAFailedRead(t *testing.T) {
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, logs := newTestCollector(t, staking, fakeDistribution{rewardsErr: errors.New("boom")})
	if _, err := testutil.CollectAndLint(c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Fatalf("the rewards failure was not logged: %s", logs.String())
	}

	c.queryCtx = func() (sdk.Context, error) { return sdk.Context{}, errors.New("no state") }
	c.cached = nil
	c.now = func() time.Time { return time.Now().Add(time.Hour) }
	n, err := testutil.GatherAndCount(collectorGatherer{c})
	if err != nil || n != 0 {
		t.Fatalf("a failed read must yield no samples, got %d, %v", n, err)
	}

	c.queryCtx = func() (sdk.Context, error) { return sdk.Context{}.WithContext(context.Background()), nil }
	c.keepers.Staking = nil
	if n, err := testutil.GatherAndCount(collectorGatherer{c}); err != nil || n != 0 {
		t.Fatalf("a panicking read must be recovered and yield no samples, got %d, %v", n, err)
	}
	if !strings.Contains(logs.String(), "panicked") {
		t.Fatalf("the panic was not logged: %s", logs.String())
	}
}

func TestObserveTxResultsReportsLargeTransfersOnly(t *testing.T) {
	staking := &fakeStaking{validators: []stakingtypes.Validator{newValidator(t, 1, 1, stakingtypes.Bonded)}}
	c, _ := newTestCollector(t, staking, fakeDistribution{})
	c.transfers = newTransferGauge(1_000, 50*time.Millisecond)

	transfer := func(amount string) abci.Event {
		return abci.Event{Type: "transfer", Attributes: []abci.EventAttribute{
			{Key: []byte("recipient"), Value: []byte("sei1to")},
			{Key: []byte("sender"), Value: []byte("sei1from")},
			{Key: []byte("amount"), Value: []byte(amount)},
		}}
	}
	c.ObserveTxResults([]*abci.ExecTxResult{
		{Code: 0, Events: []abci.Event{transfer("999usei"), transfer("5000usei,20factory/x/y")}},
		{Code: 1, Events: []abci.Event{transfer("7000usei")}},
		nil,
	})

	if n := testutil.CollectAndCount(c.transfers.gauge); n != 1 {
		t.Fatalf("got %d transfer samples, want 1", n)
	}
	got := testutil.ToFloat64(c.transfers.gauge.WithLabelValues("usei", "sei1from", "sei1to"))
	if got != 5000 {
		t.Fatalf("transfer amount = %v, want 5000", got)
	}

	deadline := time.Now().Add(5 * time.Second)
	for testutil.CollectAndCount(c.transfers.gauge) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the transfer sample was not expired")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type collectorGatherer struct{ c *Collector }

func (g collectorGatherer) Gather() ([]*dto.MetricFamily, error) {
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(g.c); err != nil {
		return nil, err
	}
	return reg.Gather()
}
