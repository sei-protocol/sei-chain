package cosmosmetrics

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// StakingKeeper is the staking state the reporter reads.
type StakingKeeper interface {
	GetParams(ctx sdk.Context) stakingtypes.Params
	BondDenom(ctx sdk.Context) string
	GetAllValidators(ctx sdk.Context) []stakingtypes.Validator
	GetValidator(ctx sdk.Context, addr sdk.ValAddress) (stakingtypes.Validator, bool)
	GetBondedPool(ctx sdk.Context) authtypes.ModuleAccountI
	GetNotBondedPool(ctx sdk.Context) authtypes.ModuleAccountI
	GetAllDelegatorDelegations(ctx sdk.Context, delegator sdk.AccAddress) []stakingtypes.Delegation
	GetUnbondingDelegations(ctx sdk.Context, delegator sdk.AccAddress, maxRetrieve uint16) []stakingtypes.UnbondingDelegation
	GetRedelegations(ctx sdk.Context, delegator sdk.AccAddress, maxRetrieve uint16) []stakingtypes.Redelegation
}

// SlashingKeeper is the slashing state the reporter reads.
type SlashingKeeper interface {
	GetParams(ctx sdk.Context) slashingtypes.Params
	GetValidatorSigningInfo(ctx sdk.Context, address sdk.ConsAddress) (slashingtypes.ValidatorSigningInfo, bool)
}

// DistributionKeeper is the distribution state the reporter reads.
type DistributionKeeper interface {
	GetParams(ctx sdk.Context) distrtypes.Params
	GetFeePoolCommunityCoins(ctx sdk.Context) sdk.DecCoins
	DelegationTotalRewards(ctx context.Context, req *distrtypes.QueryDelegationTotalRewardsRequest) (*distrtypes.QueryDelegationTotalRewardsResponse, error)
}

// BankKeeper is the bank state the reporter reads.
type BankKeeper interface {
	GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetSupply(ctx sdk.Context, denom string) sdk.Coin
}

// Keepers groups the module state the reporter reads.
type Keepers struct {
	Staking      StakingKeeper
	Slashing     SlashingKeeper
	Distribution DistributionKeeper
	Bank         BankKeeper
}

// QueryContextFunc returns a read-only context over the latest committed state.
type QueryContextFunc func() (sdk.Context, error)

// maxWalletEntries bounds the unbonding and redelegation entries read per wallet.
const maxWalletEntries = 100

// Reporter reports the cosmos_* gauges from the node's own keepers as OTel observables
// over a periodically refreshed snapshot of committed state.
type Reporter struct {
	cfg      Config
	keepers  Keepers
	queryCtx QueryContextFunc
	logger   *slog.Logger
	wallets  []sdk.AccAddress
	scale    float64

	inst     *instruments
	snapshot atomic.Pointer[[]sample]

	transfers *transferRecorder

	stop func()
}

// sample is one observed value of an asynchronous gauge.
type sample struct {
	inst  metric.Float64Observable
	value float64
	attrs metric.ObserveOption
}

// NewReporter returns a Reporter for cfg. cfg must have passed ReadConfig.
func NewReporter(cfg Config, keepers Keepers, queryCtx QueryContextFunc, logger *slog.Logger) (*Reporter, error) {
	wallets := make([]sdk.AccAddress, 0, len(cfg.WalletAddresses))
	for _, addr := range cfg.WalletAddresses {
		acc, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return nil, fmt.Errorf("wallet address %q: %w", addr, err)
		}
		wallets = append(wallets, acc)
	}
	return &Reporter{
		cfg:       cfg,
		keepers:   keepers,
		queryCtx:  queryCtx,
		logger:    logger,
		wallets:   wallets,
		scale:     math.Pow10(int(cfg.DenomExponent)),
		inst:      cosmosMetrics,
		transfers: newTransferRecorder(cosmosMetrics.bankTransferAmount, cfg.BankTransferThreshold),
		stop:      func() {},
	}, nil
}

// Start registers the observables and begins refreshing the snapshot every RefreshInterval.
func (r *Reporter) Start() error {
	reg, err := meter.RegisterCallback(r.observe, r.inst.observables()...)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(r.cfg.RefreshInterval)
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		defer ticker.Stop()
		r.refresh()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.refresh()
			}
		}
	}()
	r.stop = sync.OnceFunc(func() {
		close(stop)
		<-stopped
		if err := reg.Unregister(); err != nil {
			r.logger.Error("cosmos metrics: unregister", "err", err)
		}
	})
	return nil
}

// Stop ends refreshing and unregisters the observables. It waits for an in-flight refresh.
func (r *Reporter) Stop() {
	r.stop()
}

func (r *Reporter) observe(_ context.Context, o metric.Observer) error {
	if samples := r.snapshot.Load(); samples != nil {
		for _, s := range *samples {
			o.ObserveFloat64(s.inst, s.value, s.attrs)
		}
	}
	return nil
}

// refresh replaces the snapshot with a fresh read of committed state, keeping the previous one
// if the read fails.
func (r *Reporter) refresh() {
	if samples, ok := r.read(); ok {
		r.snapshot.Store(&samples)
	}
}

// read collects every gauge from the latest committed state. A panic in any keeper read is
// recovered and reported as a failed read so a refresh never takes the node down.
func (r *Reporter) read() (samples []sample, ok bool) {
	defer func() {
		if p := recover(); p != nil {
			r.logger.Error("cosmos metrics read panicked", "panic", p, "stack", string(debug.Stack()))
			samples, ok = nil, false
		}
	}()
	ctx, err := r.queryCtx()
	if err != nil {
		r.logger.Error("cosmos metrics: no query context", "err", err)
		return nil, false
	}
	b := &builder{inst: r.inst}
	bondDenom := r.keepers.Staking.BondDenom(ctx)
	r.readParams(ctx, b)
	r.readGeneral(ctx, b, bondDenom)
	r.readValidators(ctx, b, bondDenom)
	r.readWallets(ctx, b, bondDenom)
	for _, err := range b.errs {
		r.logger.Error("cosmos metrics: metric skipped", "err", err)
	}
	return b.samples, true
}

func (r *Reporter) readParams(ctx sdk.Context, b *builder) {
	staking := r.keepers.Staking.GetParams(ctx)
	b.gauge(b.inst.paramsMaxValidators, float64(staking.MaxValidators))
	b.gauge(b.inst.paramsUnbondingTime, staking.UnbondingTime.Seconds())

	slashing := r.keepers.Slashing.GetParams(ctx)
	b.gauge(b.inst.paramsDowntimeJailDuration, slashing.DowntimeJailDuration.Seconds())
	b.gauge(b.inst.paramsSignedBlocksWindow, float64(slashing.SignedBlocksWindow))
	b.dec(b.inst.paramsMinSignedPerWindow, slashing.MinSignedPerWindow)
	b.dec(b.inst.paramsSlashFractionDoubleSign, slashing.SlashFractionDoubleSign)
	b.dec(b.inst.paramsSlashFractionDowntime, slashing.SlashFractionDowntime)

	distr := r.keepers.Distribution.GetParams(ctx)
	b.dec(b.inst.paramsBaseProposerReward, distr.BaseProposerReward)
	b.dec(b.inst.paramsBonusProposerReward, distr.BonusProposerReward)
	b.dec(b.inst.paramsCommunityTax, distr.CommunityTax)
}

func (r *Reporter) readGeneral(ctx sdk.Context, b *builder, bondDenom string) {
	bonded := r.keepers.Bank.GetBalance(ctx, r.keepers.Staking.GetBondedPool(ctx).GetAddress(), bondDenom)
	notBonded := r.keepers.Bank.GetBalance(ctx, r.keepers.Staking.GetNotBondedPool(ctx).GetAddress(), bondDenom)
	b.int(b.inst.generalBondedTokens, bonded.Amount, 1)
	b.int(b.inst.generalNotBondedTokens, notBonded.Amount, 1)
	for _, coin := range r.keepers.Distribution.GetFeePoolCommunityCoins(ctx) {
		b.decScaled(b.inst.generalCommunityPool, coin.Amount, r.scaleFor(coin.Denom, bondDenom), denomAttr(coin.Denom))
	}
	supply := r.keepers.Bank.GetSupply(ctx, bondDenom)
	b.int(b.inst.generalSupplyTotal, supply.Amount, r.scale, denomAttr(bondDenom))
}

func (r *Reporter) readValidators(ctx sdk.Context, b *builder, bondDenom string) {
	validators := r.keepers.Staking.GetAllValidators(ctx)
	sort.SliceStable(validators, func(i, j int) bool {
		return validators[i].Tokens.GT(validators[j].Tokens)
	})
	for rank, v := range validators {
		addr, moniker := addressAttr(v.OperatorAddress), attribute.String("moniker", v.Description.Moniker)
		denom := denomAttr(bondDenom)
		b.dec(b.inst.validatorsCommission, v.Commission.Rate, addr, moniker)
		b.gauge(b.inst.validatorsStatus, float64(v.Status), addr, moniker)
		b.gauge(b.inst.validatorsJailed, boolToFloat(v.Jailed), addr, moniker)
		b.int(b.inst.validatorsTokens, v.Tokens, r.scale, addr, moniker, denom)
		b.decScaled(b.inst.validatorsDelegatorShares, v.DelegatorShares, r.scale, addr, moniker, denom)
		b.int(b.inst.validatorsMinSelfDelegation, v.MinSelfDelegation, r.scale, addr, moniker, denom)
		b.gauge(b.inst.validatorsRank, float64(rank+1), addr, moniker)

		consAddr, err := v.GetConsAddr()
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("validator %s: consensus address: %w", v.OperatorAddress, err))
			continue
		}
		pubkeyHash := attribute.String("pubkey_hash", strings.ToUpper(hex.EncodeToString(consAddr)))
		b.gauge(b.inst.validatorsActive, boolToFloat(v.IsBonded()), addr, pubkeyHash, moniker)
		if !v.IsBonded() {
			continue
		}
		if info, found := r.keepers.Slashing.GetValidatorSigningInfo(ctx, consAddr); found {
			b.gauge(b.inst.validatorsMissedBlocks, float64(info.MissedBlocksCounter), addr, moniker)
		}
	}
}

func (r *Reporter) readWallets(ctx sdk.Context, b *builder, bondDenom string) {
	denom := denomAttr(bondDenom)
	for _, acc := range r.wallets {
		addr := addressAttr(acc.String())
		balance := r.keepers.Bank.GetBalance(ctx, acc, bondDenom)
		b.int(b.inst.walletBalance, balance.Amount, r.scale, addr, denom)

		for _, d := range r.keepers.Staking.GetAllDelegatorDelegations(ctx, acc) {
			validator, found := r.keepers.Staking.GetValidator(ctx, d.GetValidatorAddr())
			if !found {
				continue
			}
			b.decScaled(b.inst.walletDelegations, validator.TokensFromShares(d.Shares), r.scale,
				addr, denom, attribute.String("delegated_to", d.ValidatorAddress))
		}
		for _, u := range r.keepers.Staking.GetUnbondingDelegations(ctx, acc, maxWalletEntries) {
			sum := sdk.ZeroInt()
			for _, e := range u.Entries {
				sum = sum.Add(e.Balance)
			}
			b.int(b.inst.walletUnbondings, sum, r.scale, addr, denom, attribute.String("unbonded_from", u.ValidatorAddress))
		}
		for _, red := range r.keepers.Staking.GetRedelegations(ctx, acc, maxWalletEntries) {
			sum := sdk.ZeroInt()
			for _, e := range red.Entries {
				sum = sum.Add(e.InitialBalance)
			}
			b.int(b.inst.walletRedelegations, sum, r.scale, addr, denom,
				attribute.String("redelegated_from", red.ValidatorSrcAddress), attribute.String("redelegated_to", red.ValidatorDstAddress))
		}

		rewards, err := r.keepers.Distribution.DelegationTotalRewards(sdk.WrapSDKContext(ctx), &distrtypes.QueryDelegationTotalRewardsRequest{DelegatorAddress: acc.String()})
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("wallet %s: rewards: %w", acc, err))
			continue
		}
		for _, rew := range rewards.Rewards {
			for _, coin := range rew.Reward {
				b.decScaled(b.inst.walletRewards, coin.Amount, r.scaleFor(coin.Denom, bondDenom),
					addr, denomAttr(coin.Denom), attribute.String("validator_address", rew.ValidatorAddress))
			}
		}
	}
}

// scaleFor is the divisor for amounts of denom: the configured exponent for the bond denom, and
// unity for anything else.
func (r *Reporter) scaleFor(denom, bondDenom string) float64 {
	if denom == bondDenom {
		return r.scale
	}
	return 1
}

func addressAttr(addr string) attribute.KeyValue { return attribute.String("address", addr) }
func denomAttr(denom string) attribute.KeyValue  { return attribute.String("denom", denom) }

// builder accumulates samples and the errors of the ones it could not build.
type builder struct {
	inst    *instruments
	samples []sample
	errs    []error
}

func (b *builder) gauge(inst metric.Float64Observable, value float64, attrs ...attribute.KeyValue) {
	b.samples = append(b.samples, sample{inst: inst, value: value, attrs: metric.WithAttributes(attrs...)})
}

func (b *builder) int(inst metric.Float64Observable, value sdk.Int, scale float64, attrs ...attribute.KeyValue) {
	f, _ := new(big.Float).SetInt(value.BigInt()).Float64()
	b.gauge(inst, f/scale, attrs...)
}

func (b *builder) dec(inst metric.Float64Observable, value sdk.Dec, attrs ...attribute.KeyValue) {
	b.decScaled(inst, value, 1, attrs...)
}

func (b *builder) decScaled(inst metric.Float64Observable, value sdk.Dec, scale float64, attrs ...attribute.KeyValue) {
	f, err := value.Float64()
	if err != nil {
		b.errs = append(b.errs, fmt.Errorf("%v: %w", attrs, err))
		return
	}
	b.gauge(inst, f/scale, attrs...)
}

func boolToFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
