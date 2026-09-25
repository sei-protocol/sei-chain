package cosmosmetrics

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

const (
	// maxValidators bounds the unjailed validators, taken by power, and the jailed validators,
	// taken from the unbonding queue, read per refresh; anyone can create a validator.
	maxValidators = 1000
	// maxWalletEntries bounds the unbonding and redelegation entries read per wallet.
	maxWalletEntries = 100
)

var (
	logger = seilog.NewLogger("cosmosmetrics")
	// queueEnd bounds the unbonding validator queue iteration; the queue is keyed by completion
	// time then height, so this covers every entry.
	queueEnd = time.Date(9999, 12, 31, 23, 59, 59, 999_999_999, time.UTC)
)

// StakingKeeper is the staking state the reporter reads.
type StakingKeeper interface {
	GetParams(sdk.Context) stakingtypes.Params
	BondDenom(sdk.Context) string
	ValidatorsPowerStoreIterator(sdk.Context) sdk.Iterator
	ValidatorQueueIterator(sdk.Context, time.Time, int64) sdk.Iterator
	GetValidator(sdk.Context, sdk.ValAddress) (stakingtypes.Validator, bool)
	GetBondedPool(sdk.Context) authtypes.ModuleAccountI
	GetNotBondedPool(sdk.Context) authtypes.ModuleAccountI
	GetAllDelegatorDelegations(sdk.Context, sdk.AccAddress) []stakingtypes.Delegation
	GetUnbondingDelegations(sdk.Context, sdk.AccAddress, uint16) []stakingtypes.UnbondingDelegation
	GetRedelegations(sdk.Context, sdk.AccAddress, uint16) []stakingtypes.Redelegation
}

// SlashingKeeper is the slashing state the reporter reads.
type SlashingKeeper interface {
	GetParams(sdk.Context) slashingtypes.Params
	GetValidatorSigningInfo(sdk.Context, sdk.ConsAddress) (slashingtypes.ValidatorSigningInfo, bool)
}

// DistributionKeeper is the distribution state the reporter reads.
type DistributionKeeper interface {
	GetParams(sdk.Context) distrtypes.Params
	DelegationTotalRewards(context.Context, *distrtypes.QueryDelegationTotalRewardsRequest) (*distrtypes.QueryDelegationTotalRewardsResponse, error)
}

// BankKeeper is the bank state the reporter reads.
type BankKeeper interface {
	GetBalance(sdk.Context, sdk.AccAddress, string) sdk.Coin
	GetSupply(sdk.Context, string) sdk.Coin
}

// Keepers groups the module state the reporter reads.
type Keepers struct {
	Staking      StakingKeeper
	Slashing     SlashingKeeper
	Distribution DistributionKeeper
	Bank         BankKeeper
	// EVM may be nil when no ERC-20 tokens are configured.
	EVM EVMKeeper
}

// QueryContextFunc returns a read-only context over the latest committed state.
type QueryContextFunc func() (sdk.Context, error)

// Reporter reports the cosmos_* gauges from the node's own keepers as OTel observables
// over a periodically refreshed snapshot of committed state.
type Reporter struct {
	cfg      Config
	keepers  Keepers
	queryCtx QueryContextFunc
	wallets  []sdk.AccAddress
	scale    float64

	erc20Tokens []common.Address

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
func NewReporter(cfg Config, keepers Keepers, queryCtx QueryContextFunc) (*Reporter, error) {
	wallets := make([]sdk.AccAddress, 0, len(cfg.WalletAddresses))
	for _, addr := range cfg.WalletAddresses {
		acc, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return nil, fmt.Errorf("wallet address %q: %w", addr, err)
		}
		wallets = append(wallets, acc)
	}
	tokens, err := parseERC20Tokens(cfg.ERC20Tokens)
	if err != nil {
		return nil, err
	}
	if len(tokens) > 0 && keepers.EVM == nil {
		return nil, fmt.Errorf("%s: set but no EVM keeper", flagERC20Tokens)
	}
	return &Reporter{
		cfg:         cfg,
		keepers:     keepers,
		queryCtx:    queryCtx,
		wallets:     wallets,
		scale:       math.Pow10(int(cfg.DenomExponent)),
		erc20Tokens: tokens,
		transfers:   newTransferRecorder(cosmosMetrics.bankTransfersTotal, cosmosMetrics.bankTransferAmountTotal, sdk.DefaultBondDenom, cfg.BankTransferThreshold),
		stop:        func() {},
	}, nil
}

// Start registers the observables and begins refreshing the snapshot every RefreshInterval.
func (r *Reporter) Start() error {
	reg, err := meter.RegisterCallback(r.observe, observables()...)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(r.cfg.RefreshInterval)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		defer ticker.Stop()
		r.refresh()
		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
			case <-ticker.C:
				r.refresh()
			}
		}
	}()
	r.stop = sync.OnceFunc(func() {
		cancel()
		<-stopped
		if err := reg.Unregister(); err != nil {
			logger.Error("cosmos metrics: unregister", "err", err)
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
			logger.Error("cosmos metrics read panicked", "panic", p, "stack", string(debug.Stack()))
			samples, ok = nil, false
		}
	}()
	ctx, err := r.queryCtx()
	if err != nil {
		logger.Error("cosmos metrics: no query context", "err", err)
		return nil, false
	}
	if ctx.BlockHeight() == 0 {
		logger.Debug("cosmos metrics: no committed state yet")
		return nil, false
	}
	b := &builder{}
	bondDenom := r.keepers.Staking.BondDenom(ctx)
	r.transfers.setDenom(bondDenom)
	r.readParams(ctx, b)
	r.readGeneral(ctx, b, bondDenom)
	r.readValidators(ctx, b, bondDenom)
	r.readWallets(ctx, b, bondDenom)
	r.readERC20Balances(ctx, b)
	for _, err := range b.errs {
		logger.Error("cosmos metrics: metric skipped", "err", err)
	}
	return b.samples, true
}

func (r *Reporter) readParams(ctx sdk.Context, b *builder) {
	staking := r.keepers.Staking.GetParams(ctx)
	b.gauge(cosmosMetrics.paramsMaxValidators, float64(staking.MaxValidators))
	b.gauge(cosmosMetrics.paramsUnbondingTime, staking.UnbondingTime.Seconds())

	slashing := r.keepers.Slashing.GetParams(ctx)
	b.gauge(cosmosMetrics.paramsDowntimeJailDuration, slashing.DowntimeJailDuration.Seconds())
	b.gauge(cosmosMetrics.paramsSignedBlocksWindow, float64(slashing.SignedBlocksWindow))
	b.dec(cosmosMetrics.paramsMinSignedPerWindow, slashing.MinSignedPerWindow)
	b.dec(cosmosMetrics.paramsSlashFractionDoubleSign, slashing.SlashFractionDoubleSign)
	b.dec(cosmosMetrics.paramsSlashFractionDowntime, slashing.SlashFractionDowntime)

	distr := r.keepers.Distribution.GetParams(ctx)
	b.dec(cosmosMetrics.paramsBaseProposerReward, distr.BaseProposerReward)
	b.dec(cosmosMetrics.paramsBonusProposerReward, distr.BonusProposerReward)
	b.dec(cosmosMetrics.paramsCommunityTax, distr.CommunityTax)
}

func (r *Reporter) readGeneral(ctx sdk.Context, b *builder, bondDenom string) {
	bonded := r.keepers.Bank.GetBalance(ctx, r.keepers.Staking.GetBondedPool(ctx).GetAddress(), bondDenom)
	notBonded := r.keepers.Bank.GetBalance(ctx, r.keepers.Staking.GetNotBondedPool(ctx).GetAddress(), bondDenom)
	b.int(cosmosMetrics.generalBondedTokens, bonded.Amount, 1)
	b.int(cosmosMetrics.generalNotBondedTokens, notBonded.Amount, 1)
	supply := r.keepers.Bank.GetSupply(ctx, bondDenom)
	b.int(cosmosMetrics.generalSupplyTotal, supply.Amount, r.scale, denomAttr(bondDenom))
}

func (r *Reporter) readValidators(ctx sdk.Context, b *builder, bondDenom string) {
	validators := r.topValidators(ctx, b)
	validators = append(validators, r.jailedValidators(ctx, b)...)
	// Ranks are by tokens over what was read; once either read is truncated they only order that
	// subset, and the truncation is in the log rather than the samples.
	sort.SliceStable(validators, func(i, j int) bool {
		return validators[i].Tokens.GT(validators[j].Tokens)
	})
	for rank, v := range validators {
		addr, moniker := addressAttr(v.OperatorAddress), attribute.String("moniker", v.Description.Moniker)
		denom := denomAttr(bondDenom)
		b.dec(cosmosMetrics.validatorsCommission, v.Commission.Rate, addr, moniker)
		b.gauge(cosmosMetrics.validatorsStatus, float64(v.Status), addr, moniker)
		b.gauge(cosmosMetrics.validatorsJailed, boolToFloat(v.Jailed), addr, moniker)
		b.int(cosmosMetrics.validatorsTokens, v.Tokens, r.scale, addr, moniker, denom)
		b.decScaled(cosmosMetrics.validatorsDelegatorShares, v.DelegatorShares, r.scale, addr, moniker, denom)
		b.int(cosmosMetrics.validatorsMinSelfDelegation, v.MinSelfDelegation, r.scale, addr, moniker, denom)
		b.gauge(cosmosMetrics.validatorsRank, float64(rank+1), addr, moniker)

		consAddr, err := v.GetConsAddr()
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("validator %s: consensus address: %w", v.OperatorAddress, err))
			continue
		}
		pubkeyHash := attribute.String("pubkey_hash", strings.ToUpper(hex.EncodeToString(consAddr)))
		b.gauge(cosmosMetrics.validatorsActive, boolToFloat(v.IsBonded()), addr, pubkeyHash, moniker)
		if !v.IsBonded() {
			continue
		}
		if info, found := r.keepers.Slashing.GetValidatorSigningInfo(ctx, consAddr); found {
			b.gauge(cosmosMetrics.validatorsMissedBlocks, float64(info.MissedBlocksCounter), addr, moniker)
		}
	}
}

// topValidators returns up to maxValidators unjailed validators, highest power first. Jailing
// removes a validator from the power index, so jailed validators are read separately.
func (r *Reporter) topValidators(ctx sdk.Context, b *builder) []stakingtypes.Validator {
	validators := make([]stakingtypes.Validator, 0, maxValidators)
	iter := r.keepers.Staking.ValidatorsPowerStoreIterator(ctx)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		if len(validators) == maxValidators {
			b.errs = append(b.errs, fmt.Errorf("validators truncated at %d entries by power", maxValidators))
			break
		}
		v, found := r.keepers.Staking.GetValidator(ctx, iter.Value())
		if !found {
			b.errs = append(b.errs, fmt.Errorf("validator %s: in power index but not found", sdk.ValAddress(iter.Value())))
			continue
		}
		validators = append(validators, v)
	}
	return validators
}

// jailedValidators returns up to maxValidators jailed validators that are still unbonding. They
// are found through the unbonding validator queue rather than a walk of the validator store: only
// validators that were bonded enter the queue, so its size is set by the bonded set and the
// unbonding time, not by how many validators anyone cares to create and jail. A validator jailed
// without ever having been bonded, or whose unbonding has completed, is not reported.
func (r *Reporter) jailedValidators(ctx sdk.Context, b *builder) []stakingtypes.Validator {
	var validators []stakingtypes.Validator
	iter := r.keepers.Staking.ValidatorQueueIterator(ctx, queueEnd, math.MaxInt64)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		var slot stakingtypes.ValAddresses
		if err := slot.Unmarshal(iter.Value()); err != nil {
			b.errs = append(b.errs, fmt.Errorf("unbonding validator queue: %w", err))
			continue
		}
		for _, bech := range slot.Addresses {
			if len(validators) == maxValidators {
				b.errs = append(b.errs, fmt.Errorf("jailed validators truncated at %d entries", maxValidators))
				return validators
			}
			addr, err := sdk.ValAddressFromBech32(bech)
			if err != nil {
				b.errs = append(b.errs, fmt.Errorf("unbonding validator %s: %w", bech, err))
				continue
			}
			v, found := r.keepers.Staking.GetValidator(ctx, addr)
			if !found {
				b.errs = append(b.errs, fmt.Errorf("validator %s: in unbonding queue but not found", bech))
				continue
			}
			if v.Jailed {
				validators = append(validators, v)
			}
		}
	}
	return validators
}

func (r *Reporter) readWallets(ctx sdk.Context, b *builder, bondDenom string) {
	denom := denomAttr(bondDenom)
	for _, acc := range r.wallets {
		addr := addressAttr(acc.String())
		balance := r.keepers.Bank.GetBalance(ctx, acc, bondDenom)
		b.int(cosmosMetrics.walletBalance, balance.Amount, r.scale, addr, denom)

		for _, d := range r.keepers.Staking.GetAllDelegatorDelegations(ctx, acc) {
			validator, found := r.keepers.Staking.GetValidator(ctx, d.GetValidatorAddr())
			if !found {
				continue
			}
			b.decScaled(cosmosMetrics.walletDelegations, validator.TokensFromShares(d.Shares), r.scale,
				addr, denom, attribute.String("delegated_to", d.ValidatorAddress))
		}
		unbondings := r.keepers.Staking.GetUnbondingDelegations(ctx, acc, maxWalletEntries)
		b.checkTruncated("unbondings", acc, len(unbondings))
		for _, u := range unbondings {
			sum := sdk.ZeroInt()
			for _, e := range u.Entries {
				sum = sum.Add(e.Balance)
			}
			b.int(cosmosMetrics.walletUnbondings, sum, r.scale, addr, denom, attribute.String("unbonded_from", u.ValidatorAddress))
		}
		redelegations := r.keepers.Staking.GetRedelegations(ctx, acc, maxWalletEntries)
		b.checkTruncated("redelegations", acc, len(redelegations))
		for _, red := range redelegations {
			sum := sdk.ZeroInt()
			for _, e := range red.Entries {
				sum = sum.Add(e.InitialBalance)
			}
			b.int(cosmosMetrics.walletRedelegations, sum, r.scale, addr, denom,
				attribute.String("redelegated_from", red.ValidatorSrcAddress), attribute.String("redelegated_to", red.ValidatorDstAddress))
		}

		rewards, err := r.keepers.Distribution.DelegationTotalRewards(sdk.WrapSDKContext(ctx), &distrtypes.QueryDelegationTotalRewardsRequest{DelegatorAddress: acc.String()})
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("wallet %s: rewards: %w", acc, err))
			continue
		}
		for _, rew := range rewards.Rewards {
			for _, coin := range rew.Reward {
				b.decScaled(cosmosMetrics.walletRewards, coin.Amount, r.scaleFor(coin.Denom, bondDenom),
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
	samples []sample
	errs    []error
}

// checkTruncated records an error when a capped wallet read returned the full cap, since the
// reported sum may then be missing entries.
func (b *builder) checkTruncated(kind string, acc sdk.AccAddress, n int) {
	if n >= maxWalletEntries {
		b.errs = append(b.errs, fmt.Errorf("wallet %s: %s truncated at %d entries", acc, kind, maxWalletEntries))
	}
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
